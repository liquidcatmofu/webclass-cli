package webclass

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	filedownloadPattern = regexp.MustCompile(`(?i)filedownload\(\s*['\"]([^'\"]+)['\"]`)
	windowOpenPattern    = regexp.MustCompile(`(?i)window\.open\(\s*['\"]([^'\"]+)['\"]`)
	executionLimitPattern = regexp.MustCompile(`実行回数(?:の制限)?[^。]{0,80}(?:[:：]\s*[0-9０-９]+|[0-9０-９]+\s*回|残り|上限|制限(?:あり|有り|されています))`)
)

type Resource struct {
	CourseID    string
	CourseName  string
	ID          string
	Title       string
	PageURL     string
	DownloadURL string
}

type ResourceStats struct {
	Materials          int
	Started            int
	SkippedLimited     []string
	SkippedInteractive []string
	NoFiles            []string
}

type Download struct {
	Body     io.ReadCloser
	Filename string
	Size     int64
	URL      string
}

func (c *Client) Resources(course Course) ([]Resource, ResourceStats, error) {
	doc, resp, err := c.document(course.URL)
	if err != nil {
		return nil, ResourceStats{}, err
	}

	var stats ResourceStats
	var pages []Resource
	doc.Find(".cl-contentsList_listGroupItem").Each(func(_ int, row *goquery.Selection) {
		category := strings.TrimSpace(row.Find(".cl-contentsList_categoryLabel").First().Text())
		// Safety boundary: pull must never enter report/questionnaire/self-study/etc.
		if category != "資料" {
			return
		}
		stats.Materials++

		nameNode := row.Find(".cm-contentsList_contentName").First()
		title := strings.Join(strings.Fields(nameNode.Text()), " ")
		if title == "" {
			title = "material"
		}

		// WebClass layouts differ: the named element can itself be the anchor,
		// or it can contain one.
		link := nameNode.Filter("a[href]").First()
		if link.Length() == 0 {
			link = nameNode.Find("a[href]").First()
		}
		if link.Length() == 0 {
			link = row.Find("a[href]").First()
		}
		href, ok := link.Attr("href")
		if !ok || strings.TrimSpace(href) == "" || strings.HasPrefix(strings.ToLower(strings.TrimSpace(href)), "javascript:") {
			stats.NoFiles = append(stats.NoFiles, title+" (no navigable material link)")
			return
		}
		pageURL := resp.Request.URL.ResolveReference(mustParse(href)).String()
		pages = append(pages, Resource{
			CourseID: course.ID, CourseName: course.Name,
			ID: stableResourceID(pageURL), Title: title, PageURL: pageURL,
		})
	})

	var out []Resource
	for _, page := range pages {
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

// materialDownloadLinks first looks for files without starting the material.
// If the page is a start screen, it starts only unrestricted, non-interactive
// entries that were already proven to be category "資料" on the course page.
func (c *Client) materialDownloadLinks(pageURL string) ([]string, string, error) {
	doc, resp, err := c.document(pageURL)
	if err != nil {
		return nil, "", err
	}

	visited := map[string]bool{resp.Request.URL.String(): true}
	links, err := c.discoverDownloadLinksInDocument(doc, resp.Request.URL, 0, visited)
	if err != nil {
		return nil, "", err
	}
	if len(links) > 0 {
		return links, "direct", nil
	}

	if hasExecutionLimit(doc) {
		return nil, "limited", nil
	}

	form, submitName, submitValue, interactive := findStartForm(doc)
	if form == nil {
		return nil, "no-files", nil
	}
	if interactive {
		return nil, "interactive", nil
	}

	startedDoc, startedResp, err := c.submitStartForm(resp.Request.URL, form, submitName, submitValue)
	if err != nil {
		return nil, "", err
	}
	visited = map[string]bool{startedResp.Request.URL.String(): true}
	links, err = c.discoverDownloadLinksInDocument(startedDoc, startedResp.Request.URL, 0, visited)
	if err != nil {
		return nil, "", err
	}
	if len(links) == 0 {
		return nil, "no-files", nil
	}
	return links, "started", nil
}

func hasExecutionLimit(doc *goquery.Document) bool {
	text := strings.Join(strings.Fields(doc.Text()), " ")
	if !strings.Contains(text, "実行回数") {
		return false
	}
	// Explicit unlimited/no-limit wording wins over the generic label.
	if strings.Contains(text, "実行回数 無制限") ||
		strings.Contains(text, "実行回数：無制限") ||
		strings.Contains(text, "実行回数: 無制限") ||
		strings.Contains(text, "実行回数の制限 なし") ||
		strings.Contains(text, "実行回数の制限：なし") {
		return false
	}
	return executionLimitPattern.MatchString(text)
}

// findStartForm finds only a literal material start submitter. It also marks
// forms requiring user-provided values (password/text/file/select/textarea) as
// interactive so pull will not submit them automatically.
func findStartForm(doc *goquery.Document) (*goquery.Selection, string, string, bool) {
	var found *goquery.Selection
	var submitName, submitValue string
	interactive := false

	doc.Find("form").EachWithBreak(func(_ int, form *goquery.Selection) bool {
		var name, value string
		hasStart := false

		form.Find("input[type='submit'], input[type='image']").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			v, _ := s.Attr("value")
			v = strings.TrimSpace(v)
			if v == "開始" || v == "教材を開始" || v == "開始する" {
				name, _ = s.Attr("name")
				value = v
				hasStart = true
				return false
			}
			return true
		})
		if !hasStart {
			form.Find("button").EachWithBreak(func(_ int, s *goquery.Selection) bool {
				typeAttr, _ := s.Attr("type")
				if typeAttr != "" && !strings.EqualFold(typeAttr, "submit") {
					return true
				}
				v := strings.TrimSpace(s.Text())
				if v == "開始" || v == "教材を開始" || v == "開始する" {
					name, _ = s.Attr("name")
					value, _ = s.Attr("value")
					if value == "" {
						value = v
					}
					hasStart = true
					return false
				}
				return true
			})
		}
		if !hasStart {
			return true
		}

		needsInput := false
		form.Find("textarea[name], select[name], input[name]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			if goquery.NodeName(s) == "textarea" || goquery.NodeName(s) == "select" {
				needsInput = true
				return false
			}
			t, _ := s.Attr("type")
			t = strings.ToLower(strings.TrimSpace(t))
			if t == "" {
				t = "text"
			}
			switch t {
			case "hidden", "submit", "image", "checkbox", "radio":
				return true
			default:
				needsInput = true
				return false
			}
		})

		found = form
		submitName, submitValue = name, value
		interactive = needsInput
		return false
	})
	return found, submitName, submitValue, interactive
}

func (c *Client) submitStartForm(current *url.URL, form *goquery.Selection, submitName, submitValue string) (*goquery.Document, *http.Response, error) {
	action, _ := form.Attr("action")
	target := current
	if strings.TrimSpace(action) != "" {
		target = current.ResolveReference(mustParse(action))
	}
	if !strings.EqualFold(target.Hostname(), c.Base.Hostname()) {
		return nil, nil, fmt.Errorf("refusing cross-origin start form: %s", target)
	}

	values := url.Values{}
	form.Find("input[name]").Each(func(_ int, s *goquery.Selection) {
		name, _ := s.Attr("name")
		if name == "" {
			return
		}
		t, _ := s.Attr("type")
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			t = "text"
		}
		if t == "submit" || t == "image" || t == "file" || t == "button" || t == "reset" {
			return
		}
		if (t == "checkbox" || t == "radio") && s.AttrOr("checked", "") == "" {
			return
		}
		value, _ := s.Attr("value")
		values.Add(name, value)
	})
	if submitName != "" {
		values.Set(submitName, submitValue)
	}

	method, _ := form.Attr("method")
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}

	var req *http.Request
	var err error
	switch method {
	case http.MethodGet:
		u := *target
		q := u.Query()
		for k, vv := range values {
			for _, v := range vv {
				q.Add(k, v)
			}
		}
		u.RawQuery = q.Encode()
		req, err = http.NewRequest(http.MethodGet, u.String(), nil)
	case http.MethodPost:
		req, err = http.NewRequest(http.MethodPost, target.String(), strings.NewReader(values.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	default:
		return nil, nil, fmt.Errorf("unsupported start form method %q", method)
	}
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Referer", current.String())

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if !strings.EqualFold(resp.Request.URL.Hostname(), c.Base.Hostname()) || strings.Contains(resp.Request.URL.Path, "login.php") {
		resp.Body.Close()
		return nil, nil, ErrNotAuthenticated
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, nil, fmt.Errorf("start material: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("parse started material %s: %w", resp.Request.URL, err)
	}
	return doc, resp, nil
}

func (c *Client) discoverDownloadLinks(pageURL string, depth int, visited map[string]bool) ([]string, error) {
	if depth > 4 || visited[pageURL] {
		return nil, nil
	}
	visited[pageURL] = true
	doc, resp, err := c.document(pageURL)
	if err != nil {
		return nil, err
	}
	return c.discoverDownloadLinksInDocument(doc, resp.Request.URL, depth, visited)
}

func (c *Client) discoverDownloadLinksInDocument(doc *goquery.Document, base *url.URL, depth int, visited map[string]bool) ([]string, error) {
	found := map[string]bool{}
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(strings.ToLower(raw), "javascript:") {
			return
		}
		u := base.ResolveReference(mustParse(raw))
		if !strings.EqualFold(u.Hostname(), c.Base.Hostname()) {
			return
		}
		if looksDownloadable(u.String()) {
			found[u.String()] = true
		}
	}

	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		add(href)
	})
	doc.Find("[onclick]").Each(func(_ int, s *goquery.Selection) {
		onclick, _ := s.Attr("onclick")
		for _, pattern := range []*regexp.Regexp{filedownloadPattern, windowOpenPattern} {
			for _, m := range pattern.FindAllStringSubmatch(onclick, -1) {
				if len(m) == 2 {
					add(m[1])
				}
			}
		}
	})
	doc.Find("script").Each(func(_ int, s *goquery.Selection) {
		for _, pattern := range []*regexp.Regexp{filedownloadPattern, windowOpenPattern} {
			for _, m := range pattern.FindAllStringSubmatch(s.Text(), -1) {
				if len(m) == 2 {
					add(m[1])
				}
			}
		}
	})

	if depth <= 4 {
		var frames []string
		doc.Find("iframe[src], frame[src]").Each(func(_ int, s *goquery.Selection) {
			src, _ := s.Attr("src")
			if src == "" {
				return
			}
			u := base.ResolveReference(mustParse(src))
			if strings.EqualFold(u.Hostname(), c.Base.Hostname()) {
				frames = append(frames, u.String())
			}
		})
		for _, frame := range frames {
			links, err := c.discoverDownloadLinks(frame, depth+1, visited)
			if err != nil {
				continue
			}
			for _, link := range links {
				found[link] = true
			}
		}
	}

	links := make([]string, 0, len(found))
	for link := range found {
		links = append(links, link)
	}
	sort.Strings(links)
	return links, nil
}

func looksDownloadable(raw string) bool {
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "/file_down.php") || strings.Contains(lower, "/download.php") || strings.Contains(lower, "/my-reports/download") {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	ext := strings.ToLower(path.Ext(u.Path))
	switch ext {
	case ".pdf", ".zip", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".txt", ".csv", ".png", ".jpg", ".jpeg":
		return true
	default:
		return false
	}
}

func (c *Client) OpenDownload(rawURL string) (*Download, error) {
	resp, err := c.get(rawURL)
	if err != nil {
		return nil, err
	}
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(strings.ToLower(contentType), "text/html") {
		doc, parseErr := goquery.NewDocumentFromReader(resp.Body)
		resp.Body.Close()
		if parseErr != nil {
			return nil, parseErr
		}
		var next string
		doc.Find("a[href]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			href, ok := s.Attr("href")
			if !ok || strings.HasPrefix(strings.ToLower(href), "javascript:") {
				return true
			}
			next = resp.Request.URL.ResolveReference(mustParse(href)).String()
			return false
		})
		if next == "" {
			return nil, fmt.Errorf("download landing page contained no file link: %s", rawURL)
		}
		resp, err = c.get(next)
		if err != nil {
			return nil, err
		}
	}
	return &Download{
		Body:     resp.Body,
		Filename: responseFilename(resp),
		Size:     resp.ContentLength,
		URL:      resp.Request.URL.String(),
	}, nil
}

func responseFilename(resp *http.Response) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if name := params["filename"]; name != "" {
				return name
			}
		}
	}
	if name := resp.Request.URL.Query().Get("file_name"); name != "" {
		return name
	}
	if name := path.Base(resp.Request.URL.Path); name != "." && name != "/" && name != "" {
		return name
	}
	return "download"
}

func stableResourceID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	// Query strings on WebClass downloads can contain short-lived tokens. Keep page identity stable.
	return u.Path
}
