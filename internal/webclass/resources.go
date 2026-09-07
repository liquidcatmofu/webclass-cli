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

var filedownloadPattern = regexp.MustCompile(`(?i)filedownload\(\s*['\"]([^'\"]+)['\"]`)

type Resource struct {
	CourseID    string
	CourseName  string
	ID          string
	Title       string
	PageURL     string
	DownloadURL string
}

type Download struct {
	Body     io.ReadCloser
	Filename string
	Size     int64
	URL      string
}

func (c *Client) Resources(course Course) ([]Resource, error) {
	doc, resp, err := c.document(course.URL)
	if err != nil {
		return nil, err
	}

	var pages []Resource
	doc.Find(".cl-contentsList_listGroupItem").Each(func(_ int, row *goquery.Selection) {
		category := strings.TrimSpace(row.Find(".cl-contentsList_categoryLabel").First().Text())
		if category != "資料" {
			return
		}
		nameNode := row.Find(".cm-contentsList_contentName").First()
		title := strings.Join(strings.Fields(nameNode.Text()), " ")
		link := nameNode.Find("a[href]").First()
		if link.Length() == 0 {
			link = row.Find("a[href]").First()
		}
		href, ok := link.Attr("href")
		if !ok {
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
		links, err := c.discoverDownloadLinks(page.PageURL, 0, map[string]bool{})
		if err != nil {
			return nil, fmt.Errorf("scan resource %q: %w", page.Title, err)
		}
		for _, link := range links {
			r := page
			r.DownloadURL = link
			r.ID = page.ID
			out = append(out, r)
		}
	}
	return out, nil
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
	base := resp.Request.URL
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
		if onclick, ok := s.Attr("onclick"); ok {
			for _, m := range filedownloadPattern.FindAllStringSubmatch(onclick, -1) {
				if len(m) == 2 {
					add(m[1])
				}
			}
		}
	})
	doc.Find("script").Each(func(_ int, s *goquery.Selection) {
		for _, m := range filedownloadPattern.FindAllStringSubmatch(s.Text(), -1) {
			if len(m) == 2 {
				add(m[1])
			}
		}
	})

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
