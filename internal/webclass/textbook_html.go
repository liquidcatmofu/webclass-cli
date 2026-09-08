package webclass

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const textbookHTMLMarker = "webclass-cli-textbook-html"

type textbookJSONConfig struct {
	TextURL  string            `json:"text_url"`
	TextURLs map[string]string `json:"text_urls"`
}

type textbookHTMLPage struct {
	Page  int
	Total int
	URL   string
	Title string
}

// textbookHTMLBodyLinks extracts WebClass textbook-body HTML files from the
// json-data configuration embedded in txtbk_show_chapter pages. WebClass uses
// txtbk_show_text.php as a viewer even when the actual body is a static .html
// file. We derive the same-origin static file URL from WebClass-provided
// contents_url + file parameters and mark it locally in the URL fragment. The
// marker is never sent to WebClass; it only tells OpenPullDownload to preserve
// the HTML body instead of treating it as a download landing page.
func textbookHTMLBodyLinks(doc *goquery.Document, base *url.URL, host string) []string {
	title := strings.TrimSpace(doc.Find(`input[name="contents_name"]`).First().AttrOr("value", ""))
	if title == "" {
		title = strings.TrimSpace(doc.Find("title").First().Text())
		title = strings.TrimSpace(strings.TrimSuffix(title, "- WebClass"))
	}

	seen := map[string]bool{}
	var pages []textbookHTMLPage
	doc.Find("script#json-data").Each(func(_ int, s *goquery.Selection) {
		var cfg textbookJSONConfig
		if err := json.Unmarshal([]byte(strings.TrimSpace(s.Text())), &cfg); err != nil {
			return
		}

		rawByPage := map[string]string{}
		for key, raw := range cfg.TextURLs {
			rawByPage[key] = raw
		}
		if len(rawByPage) == 0 && strings.TrimSpace(cfg.TextURL) != "" {
			rawByPage["1"] = cfg.TextURL
		}
		if len(rawByPage) == 0 {
			return
		}

		keys := make([]string, 0, len(rawByPage))
		for key := range rawByPage {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			ai, aerr := strconv.Atoi(keys[i])
			bj, berr := strconv.Atoi(keys[j])
			if aerr == nil && berr == nil {
				return ai < bj
			}
			return keys[i] < keys[j]
		})

		total := len(keys)
		for index, key := range keys {
			raw := strings.TrimSpace(rawByPage[key])
			viewer := base.ResolveReference(mustParse(raw))
			if !strings.EqualFold(viewer.Hostname(), host) {
				continue
			}

			file := strings.TrimSpace(viewer.Query().Get("file"))
			ext := strings.ToLower(path.Ext(file))
			if ext != ".html" && ext != ".htm" {
				continue
			}
			contentsURL := strings.TrimSpace(viewer.Query().Get("contents_url"))
			if contentsURL == "" {
				continue
			}
			contentsBase := base.ResolveReference(mustParse(contentsURL))
			if !strings.EqualFold(contentsBase.Hostname(), host) {
				continue
			}
			if !strings.HasSuffix(contentsBase.Path, "/") {
				contentsBase.Path += "/"
			}
			fileRef, err := url.Parse(file)
			if err != nil {
				continue
			}
			direct := contentsBase.ResolveReference(fileRef)
			if !strings.EqualFold(direct.Hostname(), host) {
				continue
			}

			page := index + 1
			if parsed, err := strconv.Atoi(key); err == nil && parsed > 0 {
				page = parsed
			} else if parsed, err := strconv.Atoi(viewer.Query().Get("page")); err == nil && parsed > 0 {
				page = parsed
			}
			marked := markTextbookHTMLURL(direct, title, page, total)
			if seen[marked] {
				continue
			}
			seen[marked] = true
			pages = append(pages, textbookHTMLPage{Page: page, Total: total, URL: marked, Title: title})
		}
	})

	sort.Slice(pages, func(i, j int) bool {
		if pages[i].Page != pages[j].Page {
			return pages[i].Page < pages[j].Page
		}
		return pages[i].URL < pages[j].URL
	})
	out := make([]string, 0, len(pages))
	for _, page := range pages {
		out = append(out, page.URL)
	}
	return out
}

func markTextbookHTMLURL(u *url.URL, title string, page, total int) string {
	copyURL := *u
	marker := url.Values{}
	marker.Set("kind", textbookHTMLMarker)
	marker.Set("title", title)
	marker.Set("page", strconv.Itoa(page))
	marker.Set("pages", strconv.Itoa(total))
	copyURL.Fragment = marker.Encode()
	return copyURL.String()
}

func textbookHTMLInfo(raw string) (title string, page, total int, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment == "" {
		return "", 0, 0, false
	}
	marker, err := url.ParseQuery(u.Fragment)
	if err != nil || marker.Get("kind") != textbookHTMLMarker {
		return "", 0, 0, false
	}
	page, _ = strconv.Atoi(marker.Get("page"))
	total, _ = strconv.Atoi(marker.Get("pages"))
	if page <= 0 {
		page = 1
	}
	if total <= 0 {
		total = 1
	}
	return marker.Get("title"), page, total, true
}

// OpenPullDownload behaves like OpenDownload for ordinary files, but downloads
// marked textbook HTML bodies verbatim. This avoids OpenDownload's normal HTML
// landing-page resolution, which is correct for file_down.php but wrong for an
// actual textbook body whose content type is text/html.
func (c *Client) OpenPullDownload(rawURL string) (*Download, error) {
	title, page, total, isTextbookHTML := textbookHTMLInfo(rawURL)
	if !isTextbookHTML {
		return c.OpenDownload(rawURL)
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	u.Fragment = ""
	if !strings.EqualFold(u.Hostname(), c.Base.Hostname()) {
		return nil, fmt.Errorf("refusing cross-origin textbook HTML download: %s", u.String())
	}
	resp, err := c.get(u.String())
	if err != nil {
		return nil, err
	}

	ext := path.Ext(resp.Request.URL.Path)
	if ext == "" {
		ext = ".html"
	}
	filename := strings.TrimSpace(title)
	if filename == "" {
		filename = strings.TrimSuffix(path.Base(resp.Request.URL.Path), path.Ext(resp.Request.URL.Path))
	}
	if total > 1 {
		filename = fmt.Sprintf("%s - %02d", filename, page)
	}
	if !strings.HasSuffix(strings.ToLower(filename), strings.ToLower(ext)) {
		filename += ext
	}

	return &Download{
		Body:     resp.Body,
		Filename: filename,
		Size:     resp.ContentLength,
		URL:      resp.Request.URL.String(),
	}, nil
}
