package webclass

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var courseIDPattern = regexp.MustCompile(`/course\.php/([^/?#]+)`)

type Course struct {
	ID   string
	Name string
	URL  string
}

func (c *Client) Courses() ([]Course, error) {
	doc, resp, err := c.document(c.Base.String())
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var courses []Course
	doc.Find(`a[href*="/course.php/"]`).Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		abs, err := url.Parse(resp.Request.URL.ResolveReference(mustParse(href)).String())
		if err != nil {
			return
		}
		m := courseIDPattern.FindStringSubmatch(abs.Path)
		if len(m) != 2 || seen[m[1]] {
			return
		}
		name := strings.Join(strings.Fields(s.Text()), " ")
		if name == "" {
			name = m[1]
		}
		seen[m[1]] = true
		courses = append(courses, Course{ID: m[1], Name: name, URL: abs.String()})
	})
	if len(courses) == 0 {
		return nil, fmt.Errorf("no courses found; the WebClass dashboard layout may have changed")
	}
	return courses, nil
}

func mustParse(raw string) *url.URL {
	u, _ := url.Parse(raw)
	return u
}
