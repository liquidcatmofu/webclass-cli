package webclass

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var locationURLPattern = regexp.MustCompile(`(?i)(?:window\.)?location(?:\.href)?\s*=\s*['\"]([^'\"]+)['\"]`)

type PullItem struct {
	Group string
	Title string
}

type PullResourceStats struct {
	Documents          int
	Rows               int
	Frames             int
	Categories         map[string]int
	GroupOrder         []string
	GroupCategories    map[string]map[string]int
	ResourceGroups     map[string]string
	Materials          int
	Started            int
	SkippedLimited     []PullItem
	SkippedInteractive []PullItem
	NoFiles            []PullItem
	ScannedURLs        []string
}

// PullResources is the conservative material discovery path used by `pull`.
// It only follows material links after a row has been positively identified as
// category "資料". Course-page frames themselves may be scanned because loading
// them is part of rendering the course page; links for other content categories
// are never followed.
func (c *Client) PullResources(course Course) ([]Resource, PullResourceStats, error) {
	stats := PullResourceStats{
		Categories:      map[string]int{},
		GroupCategories: map[string]map[string]int{},
		ResourceGroups:  map[string]string{},
	}
	visited := map[string]bool{}
	var pages []Resource

	// Dashboard links enter a course through /login?acs_=..., while the actual
	// contents list is /course.php/<id>/. Enter the course first, then parse the
	// canonical course index URL.
	indexURL := c.courseIndexURL(course.ID)
	if course.URL != "" && course.URL != indexURL {
		resp, err := c.get(course.URL)
		if err != nil {
			return nil, stats, fmt.Errorf("enter course %s: %w", course.ID, err)
		}
		resp.Body.Close()
	}

	if err := c.scanCoursePage(course, indexURL, 0, visited, &stats, &pages); err != nil {
		return nil, stats, err
	}

	// A frame tree can expose the same row more than once. Deduplicate by content
	// ID before any material-start side effect.
	seenIDs := map[string]bool{}
	uniquePages := make([]Resource, 0, len(pages))
	for _, page := range pages {
		if seenIDs[page.ID] {
			continue
		}
		seenIDs[page.ID] = true
		uniquePages = append(uniquePages, page)
	}

	var out []Resource
	for _, page := range uniquePages {
		group := stats.ResourceGroups[page.ID]
		links, state, err := c.materialDownloadLinksV2(page.PageURL)
		if err != nil {
			return nil, stats, fmt.Errorf("scan resource %q: %w", page.Title, err)
		}
		switch state {
		case "started":
			stats.Started++
		case "limited":
			stats.SkippedLimited = append(stats.SkippedLimited, PullItem{Group: group, Title: page.Title})
			continue
		case "interactive":
			stats.SkippedInteractive = append(stats.SkippedInteractive, PullItem{Group: group, Title: page.Title})
			continue
		case "no-files":
			stats.NoFiles = append(stats.NoFiles, PullItem{Group: group, Title: page.Title})
			continue
		}
		for _, link := range links {
			r := page
			r.DownloadURL = link
			out = append(out, r)
		}
	}
	return out, stats, nil
}

func (c *Client) courseIndexURL(courseID string) string {
	return c.Base.ResolveReference(mustParse("course.php/" + url.PathEscape(courseID) + "/")).String()
}

func (c *Client) scanCoursePage(course Course, rawURL string, depth int, visited map[string]bool, stats *PullResourceStats, pages *[]Resource) error {
	if depth > 4 || visited[rawURL] {
		return nil
	}
	visited[rawURL] = true

	doc, resp, err := c.document(rawURL)
	if err != nil {
		return err
	}
	base := resp.Request.URL
	canonical := base.String()
	if canonical != rawURL {
		visited[canonical] = true
	}
	stats.Documents++
	stats.ScannedURLs = append(stats.ScannedURLs, canonical)

	// Record folder headings even when a folder contains only non-material types.
	// This lets pull show "課題提出: レポート=..." without opening any report.
	doc.Find(".cl-contentsList_folder").Each(func(_ int, folder *goquery.Selection) {
		group := normalizedText(folder.Find(".panel-heading .panel-title").First())
		if group == "" {
			return
		}
		ensureGroup(stats, group)
	})

	rows := doc.Find(".cl-contentsList_listGroupItem")
	stats.Rows += rows.Length()
	rows.Each(func(_ int, row *goquery.Selection) {
		group := rowGroup(row)
		ensureGroup(stats, group)

		category := normalizedText(row.Find(".cl-contentsList_categoryLabel").First())
		if category == "" {
			category = "(missing)"
		}
		stats.Categories[category]++
		stats.GroupCategories[group][category]++
		if category != "資料" {
			return
		}
		stats.Materials++

		nameNode := row.Find(".cm-contentsList_contentName").First()
		title := normalizedText(nameNode)
		if title == "" {
			title = strings.TrimSpace(row.AttrOr("data-contents-name", ""))
		}
		if title == "" {
			title = "material"
		}

		href := materialHref(row, nameNode)
		if href == "" {
			stats.NoFiles = append(stats.NoFiles, PullItem{Group: group, Title: title + " (no navigable material link)"})
			return
		}
		u := base.ResolveReference(mustParse(href))
		if !strings.EqualFold(u.Hostname(), c.Base.Hostname()) {
			stats.NoFiles = append(stats.NoFiles, PullItem{Group: group, Title: title + " (cross-origin material link refused)"})
			return
		}
		pageURL := u.String()

		// WebClass exposes a stable data-contents-id on each course-list row. Use it
		// instead of the do_contents.php path (which is identical for all materials).
		contentID := strings.TrimSpace(row.AttrOr("data-contents-id", ""))
		if contentID == "" {
			contentID = u.Query().Get("set_contents_id")
		}
		if contentID == "" {
			contentID = stableResourceID(pageURL)
		}
		stats.ResourceGroups[contentID] = group

		*pages = append(*pages, Resource{
			CourseID: course.ID, CourseName: course.Name,
			ID: contentID, Title: title, PageURL: pageURL,
		})
	})

	// Scan only the frame tree belonging to the course page. This does not follow
	// content-list links and therefore cannot enter questionnaires/reports/etc.
	var frames []string
	doc.Find("iframe[src], frame[src]").Each(func(_ int, s *goquery.Selection) {
		src := strings.TrimSpace(s.AttrOr("src", ""))
		if src == "" || strings.HasPrefix(strings.ToLower(src), "javascript:") {
			return
		}
		u := base.ResolveReference(mustParse(src))
		if strings.EqualFold(u.Hostname(), c.Base.Hostname()) {
			frames = append(frames, u.String())
		}
	})
	stats.Frames += len(frames)
	for _, frame := range frames {
		if err := c.scanCoursePage(course, frame, depth+1, visited, stats, pages); err != nil {
			continue
		}
	}
	return nil
}

func ensureGroup(stats *PullResourceStats, group string) {
	if group == "" {
		group = "(ungrouped)"
	}
	if _, ok := stats.GroupCategories[group]; ok {
		return
	}
	stats.GroupCategories[group] = map[string]int{}
	stats.GroupOrder = append(stats.GroupOrder, group)
}

func rowGroup(row *goquery.Selection) string {
	folder := row.ParentsFiltered(".cl-contentsList_folder").First()
	if folder.Length() == 0 {
		return "(ungrouped)"
	}
	group := normalizedText(folder.Find(".panel-heading .panel-title").First())
	if group == "" {
		return "(ungrouped)"
	}
	return group
}

func normalizedText(s *goquery.Selection) string {
	if s == nil || s.Length() == 0 {
		return ""
	}
	return strings.Join(strings.Fields(s.Text()), " ")
}

func materialHref(row, nameNode *goquery.Selection) string {
	for _, sel := range []*goquery.Selection{
		nameNode.Filter("a[href]").First(),
		nameNode.Find("a[href]").First(),
		row.Find("a[href]").First(),
	} {
		if sel == nil || sel.Length() == 0 {
			continue
		}
		href := strings.TrimSpace(sel.AttrOr("href", ""))
		if href == "" {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(href), "javascript:") {
			return href
		}
		if m := locationURLPattern.FindStringSubmatch(href); len(m) == 2 {
			return m[1]
		}
	}

	var result string
	row.Find("[onclick]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		onclick := s.AttrOr("onclick", "")
		if m := locationURLPattern.FindStringSubmatch(onclick); len(m) == 2 {
			result = m[1]
			return false
		}
		return true
	})
	return result
}

func sortedCategorySummary(categories map[string]int) []string {
	out := make([]string, 0, len(categories))
	for category, count := range categories {
		out = append(out, fmt.Sprintf("%s=%d", category, count))
	}
	sort.Strings(out)
	return out
}

func resolveSameOrigin(base *url.URL, raw string, host string) string {
	u := base.ResolveReference(mustParse(raw))
	if !strings.EqualFold(u.Hostname(), host) {
		return ""
	}
	return u.String()
}
