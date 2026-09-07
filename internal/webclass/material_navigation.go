package webclass

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	autoLocationPattern = regexp.MustCompile(`(?is)(?:window\s*\.\s*)?(?:top\s*\.\s*)?location(?:\s*\.\s*href)?\s*=\s*['\"]([^'\"]+)['\"]`)
	metaRefreshPattern  = regexp.MustCompile(`(?i)(?:^|;)\s*url\s*=\s*['\"]?([^'\";]+)`)
)

// materialDownloadLinksV2 follows WebClass's actual content-launch flow.
// In WebClass 12.x, do_contents.php can answer with JavaScript such as
// window.top.location.href="..." rather than an HTTP redirect or a start form.
// Only same-origin automatic navigation is followed. A side-effecting start
// action is still performed at most once and only after the caller has already
// proved the course-list row is category "資料".
func (c *Client) materialDownloadLinksV2(pageURL string) ([]string, string, error) {
	current := pageURL
	visitedPages := map[string]bool{}
	started := false

	var doc *goquery.Document
	var base *url.URL

	for hop := 0; hop < 10; hop++ {
		if doc == nil {
			if visitedPages[current] {
				return nil, "no-files", nil
			}
			loadedDoc, resp, err := c.document(current)
			if err != nil {
				return nil, "", err
			}
			doc = loadedDoc
			base = resp.Request.URL
			visitedPages[base.String()] = true
		}

		frameVisited := map[string]bool{base.String(): true}
		links, err := c.discoverDownloadLinksInDocument(doc, base, 0, frameVisited)
		if err != nil {
			return nil, "", err
		}
		if len(links) > 0 {
			if started {
				return links, "started", nil
			}
			return links, "direct", nil
		}

		if hasExecutionLimit(doc) {
			return nil, "limited", nil
		}

		// do_contents.php commonly returns a tiny document whose only purpose is
		// assigning window.top.location.href to the real WebClass content frame.
		if next := automaticNavigationURL(doc, base, c.Base.Hostname()); next != "" && !visitedPages[next] {
			if strings.HasSuffix(strings.ToLower(base.Path), "/do_contents.php") {
				started = true
			}
			current = next
			doc = nil
			base = nil
			continue
		}

		if !started {
			form, submitName, submitValue, interactive := findStartForm(doc)
			if form != nil {
				if interactive {
					return nil, "interactive", nil
				}
				startedDoc, startedResp, err := c.submitStartForm(base, form, submitName, submitValue)
				if err != nil {
					return nil, "", err
				}
				doc = startedDoc
				base = startedResp.Request.URL
				visitedPages[base.String()] = true
				started = true
				continue
			}

			if next := startLinkURL(doc, base, c.Base.Hostname()); next != "" {
				current = next
				doc = nil
				base = nil
				started = true
				continue
			}
		}

		return nil, "no-files", nil
	}

	return nil, "no-files", nil
}

func hasExecutionLimitText(text string) bool {
	text = strings.Join(strings.Fields(text), " ")
	if !strings.Contains(text, "実行回数") && !strings.Contains(text, "回数制限") {
		return false
	}
	if strings.Contains(text, "実行回数 無制限") ||
		strings.Contains(text, "実行回数：無制限") ||
		strings.Contains(text, "実行回数: 無制限") ||
		strings.Contains(text, "実行回数の制限 なし") ||
		strings.Contains(text, "実行回数の制限：なし") ||
		strings.Contains(text, "回数制限 なし") ||
		strings.Contains(text, "回数制限：なし") {
		return false
	}
	return executionLimitPattern.MatchString(text) || strings.Contains(text, "回数制限あり") || strings.Contains(text, "回数制限：あり")
}

func automaticNavigationURL(doc *goquery.Document, base *url.URL, host string) string {
	var raw string
	doc.Find("script").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if m := autoLocationPattern.FindStringSubmatch(s.Text()); len(m) == 2 {
			raw = strings.TrimSpace(m[1])
			return false
		}
		return true
	})
	if raw == "" {
		doc.Find("[onload]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			if m := autoLocationPattern.FindStringSubmatch(s.AttrOr("onload", "")); len(m) == 2 {
				raw = strings.TrimSpace(m[1])
				return false
			}
			return true
		})
	}
	if raw == "" {
		doc.Find("meta[http-equiv]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			if !strings.EqualFold(strings.TrimSpace(s.AttrOr("http-equiv", "")), "refresh") {
				return true
			}
			if m := metaRefreshPattern.FindStringSubmatch(s.AttrOr("content", "")); len(m) == 2 {
				raw = strings.TrimSpace(m[1])
				return false
			}
			return true
		})
	}
	if raw == "" {
		return ""
	}
	return resolveSameOrigin(base, raw, host)
}

func startLinkURL(doc *goquery.Document, base *url.URL, host string) string {
	isStartText := func(s string) bool {
		s = strings.Join(strings.Fields(s), " ")
		return s == "開始" || s == "教材を開始" || s == "開始する"
	}

	var raw string
	doc.Find("a[href]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if !isStartText(s.Text()) {
			return true
		}
		raw = strings.TrimSpace(s.AttrOr("href", ""))
		return raw == ""
	})
	if raw == "" {
		doc.Find("button[onclick], input[onclick]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			text := s.Text()
			if goquery.NodeName(s) == "input" {
				text = s.AttrOr("value", "")
			}
			if !isStartText(text) {
				return true
			}
			if m := autoLocationPattern.FindStringSubmatch(s.AttrOr("onclick", "")); len(m) == 2 {
				raw = strings.TrimSpace(m[1])
				return false
			}
			return true
		})
	}
	if raw == "" || strings.HasPrefix(strings.ToLower(raw), "javascript:") {
		return ""
	}
	return resolveSameOrigin(base, raw, host)
}
