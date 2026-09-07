package webclass

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	autoLocationPattern = regexp.MustCompile(`(?is)(?:window\s*\.\s*)?(?:top\s*\.\s*)?location(?:\s*\.\s*href)?\s*=\s*['\"]([^'\"]+)['\"]`)
	metaRefreshPattern  = regexp.MustCompile(`(?i)(?:^|;)\s*url\s*=\s*['\"]?([^'\";]+)`)
)

type MaterialSession struct {
	Links   []string
	State   string
	Started bool
	Close   *MaterialCloseAction
}

type MaterialCloseAction struct {
	URL     string
	Method  string
	Referer string
	Values  url.Values
}

// OpenMaterialSession follows WebClass's actual content-launch flow for one
// material only. The caller must download all returned links and, when Started
// is true, call CloseMaterial before opening another material.
func (c *Client) OpenMaterialSession(pageURL string) (MaterialSession, error) {
	current := pageURL
	visitedPages := map[string]bool{}
	started := false
	var closeAction *MaterialCloseAction

	var doc *goquery.Document
	var base *url.URL

	for hop := 0; hop < 10; hop++ {
		if doc == nil {
			if visitedPages[current] {
				return MaterialSession{State: "no-files", Started: started, Close: closeAction}, nil
			}
			loadedDoc, resp, err := c.document(current)
			if err != nil {
				return MaterialSession{}, err
			}
			doc = loadedDoc
			base = resp.Request.URL
			visitedPages[base.String()] = true
		}

		frameVisited := map[string]bool{base.String(): true}
		links, foundClose, err := c.discoverMaterialView(doc, base, 0, frameVisited)
		if err != nil {
			return MaterialSession{}, err
		}
		if closeAction == nil && foundClose != nil {
			closeAction = foundClose
		}
		if len(links) > 0 {
			state := "direct"
			if started {
				state = "started"
			}
			return MaterialSession{Links: links, State: state, Started: started, Close: closeAction}, nil
		}

		if hasExecutionLimit(doc) {
			return MaterialSession{State: "limited", Started: started, Close: closeAction}, nil
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
					return MaterialSession{State: "interactive"}, nil
				}
				startedDoc, startedResp, err := c.submitStartForm(base, form, submitName, submitValue)
				if err != nil {
					return MaterialSession{}, err
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

		return MaterialSession{State: "no-files", Started: started, Close: closeAction}, nil
	}

	return MaterialSession{State: "no-files", Started: started, Close: closeAction}, nil
}

// materialDownloadLinksV2 is kept for focused parser tests. New pull code should
// use OpenMaterialSession so it can close a started material before the next one.
func (c *Client) materialDownloadLinksV2(pageURL string) ([]string, string, error) {
	session, err := c.OpenMaterialSession(pageURL)
	if err != nil {
		return nil, "", err
	}
	return session.Links, session.State, nil
}

func (c *Client) discoverMaterialView(doc *goquery.Document, base *url.URL, depth int, visited map[string]bool) ([]string, *MaterialCloseAction, error) {
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
		add(s.AttrOr("href", ""))
	})
	doc.Find("[onclick]").Each(func(_ int, s *goquery.Selection) {
		onclick := s.AttrOr("onclick", "")
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

	closeAction := closeActionFromDocument(doc, base, c.Base.Hostname())
	if depth <= 4 {
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

		for _, frame := range frames {
			if visited[frame] {
				continue
			}
			visited[frame] = true
			childDoc, resp, err := c.document(frame)
			if err != nil {
				continue
			}
			visited[resp.Request.URL.String()] = true
			links, childClose, err := c.discoverMaterialView(childDoc, resp.Request.URL, depth+1, visited)
			if err != nil {
				continue
			}
			for _, link := range links {
				found[link] = true
			}
			if closeAction == nil && childClose != nil {
				closeAction = childClose
			}
		}
	}

	links := make([]string, 0, len(found))
	for link := range found {
		links = append(links, link)
	}
	sort.Strings(links)
	return links, closeAction, nil
}

func closeActionFromDocument(doc *goquery.Document, base *url.URL, host string) *MaterialCloseAction {
	var result *MaterialCloseAction
	doc.Find("form").EachWithBreak(func(_ int, form *goquery.Selection) bool {
		hasClose := false
		form.Find("input, button, a").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			text := strings.TrimSpace(s.Text())
			if goquery.NodeName(s) == "input" {
				text = strings.TrimSpace(s.AttrOr("value", ""))
			}
			if strings.Contains(text, "資料を閉じる") {
				hasClose = true
				return false
			}
			return true
		})
		if !hasClose || form.Find("input[name='sendCmd']").Length() == 0 {
			return true
		}

		action := strings.TrimSpace(form.AttrOr("action", ""))
		target := base
		if action != "" {
			target = base.ResolveReference(mustParse(action))
		}
		if !strings.EqualFold(target.Hostname(), host) {
			return true
		}

		method := strings.ToUpper(strings.TrimSpace(form.AttrOr("method", "")))
		if method == "" {
			method = http.MethodGet
		}
		if method != http.MethodGet && method != http.MethodPost {
			return true
		}

		values := url.Values{}
		form.Find("input[name]").Each(func(_ int, s *goquery.Selection) {
			name := strings.TrimSpace(s.AttrOr("name", ""))
			if name == "" {
				return
			}
			typeAttr := strings.ToLower(strings.TrimSpace(s.AttrOr("type", "")))
			if typeAttr == "" {
				typeAttr = "text"
			}
			if typeAttr != "hidden" {
				return
			}
			values.Add(name, s.AttrOr("value", ""))
		})
		// The WebClass textbook page exposes document.app.quit() together with a
		// hidden sendCmd field. Its quit command is submitted through that field.
		values.Set("sendCmd", "quit")
		result = &MaterialCloseAction{
			URL: target.String(), Method: method, Referer: base.String(), Values: values,
		}
		return false
	})
	return result
}

func (c *Client) CloseMaterial(action *MaterialCloseAction) error {
	if action == nil {
		return fmt.Errorf("started material has no recognized close action")
	}
	target, err := url.Parse(action.URL)
	if err != nil {
		return err
	}
	if !strings.EqualFold(target.Hostname(), c.Base.Hostname()) {
		return fmt.Errorf("refusing cross-origin material close: %s", target)
	}

	var req *http.Request
	switch action.Method {
	case http.MethodGet:
		u := *target
		q := u.Query()
		for key, values := range action.Values {
			for _, value := range values {
				q.Add(key, value)
			}
		}
		u.RawQuery = q.Encode()
		req, err = http.NewRequest(http.MethodGet, u.String(), nil)
	case http.MethodPost:
		req, err = http.NewRequest(http.MethodPost, target.String(), strings.NewReader(action.Values.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	default:
		return fmt.Errorf("unsupported material close method %q", action.Method)
	}
	if err != nil {
		return err
	}
	if action.Referer != "" {
		req.Header.Set("Referer", action.Referer)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if !strings.EqualFold(resp.Request.URL.Hostname(), c.Base.Hostname()) || strings.Contains(resp.Request.URL.Path, "login.php") {
		return ErrNotAuthenticated
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("close material: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	// If WebClass simply redisplays the same active-material form, do not claim
	// success and do not allow pull to open another material.
	if doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(string(body))); parseErr == nil {
		if closeActionFromDocument(doc, resp.Request.URL, c.Base.Hostname()) != nil {
			return fmt.Errorf("close material: WebClass still returned an active material page")
		}
	}
	return nil
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
