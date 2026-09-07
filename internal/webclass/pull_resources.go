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

type PullResourceStats struct {
	Documents          int
	Rows               int
	Frames             int
	Categories         map[string]int
	Materials          int
	Started            int
	SkippedLimited     []string
	SkippedInteractive []string
	NoFiles            []string
	ScannedURLs        []string
}

// PullResources is the conservative material discovery path used by `pull`.
// It only follows material links after a row has been positively identified as
// category "資料". Course-page frames themselves may be scanned because loading
// them is part of rendering the course page; links for other content categories
// are never followed.
func (c *Client) PullResources(course Course) ([]Resource, PullResourceStats, error) {
	stats := PullResourceStats{Categories: map[string]int{}}
	visited := map[string]bool{}
	var pages []Resource

	// Course links on the WebClass dashboard point to
	// /course.php/<id>/login?acs_=... . Visiting that URL establishes the current
	// course in the WebClass session, but the actual contents list lives at
	// /course.php/<id>/. Do not try to parse the login endpoint as the course page.
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

	// A frame tree can expose the same row more than once. Deduplicate by the
	// resolved material page URL before doing any side-effecting start request.
	seenPages := map[string]bool{}
	uniquePages := make([]Resource, 0, len(pages))
	for _, page := range pages {
		if seenPages[page.PageURL] {
			continue
		}
		seenPages[page.PageURL] = true
		uniquePages = append(uniquePages, page)
	}

	var out []Resource
	for _, page := range uniquePages {
		links, state, err := c.materialDownloadLinks(page.PageURL)
		if err != nil {
			return nil, stats, fmt.Errorf("scan resource %q: %w", page.Title, err)
		}
		switch state {
		case "started":
			stats.Started++
		case "limited":
			stats.SkippedLimited = append(stats.SkippedLimited, page.Title)
			continue
		case "interactive":
			stats.SkippedInteractive = append(stats.SkippedInteractive, page.Title)
			continue
		case "no-files":
			stats.NoFiles = append(stats.NoFiles, page.Title)
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

	rows := doc.Find(".cl-contentsList_listGroupItem")
	stats.Rows += rows.Length()
	rows.Each(func(_ int, row *goquery.Selection) {
		category := normalizedText(row.Find(".cl-contentsList_categoryLabel").First())
		if category == "" {
			category = "(missing)"
		}
		stats.Categories[category]++
		if category != "資料" {
			return
		}
		stats.Materials++

		nameNode := row.Find(".cm-contentsList_contentName").First()
		title := normalizedText(nameNode)
		if title == "" {
			title = "material"
		}

		href := materialHref(row, nameNode)
		if href == "" {
			stats.NoFiles = append(stats.NoFiles, title+" (no navigable material link)")
			return
		}
		u := base.ResolveReference(mustParse(href))
		if !strings.EqualFold(u.Hostname(), c.Base.Hostname()) {
			stats.NoFiles = append(stats.NoFiles, title+" (cross-origin material link refused)")
			return
		}
		pageURL := u.String()
		*pages = append(*pages, Resource{
			CourseID: course.ID, CourseName: course.Name,
			ID: stableResourceID(pageURL), Title: title, PageURL: pageURL,
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
			// A decorative/navigation frame failing should not hide rows available in
			// the rest of the course page. Keep scanning other same-origin frames.
			continue
		}
	}
	return nil
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
