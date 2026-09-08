package webclass

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/liquidcatmofu/webclass-cli/internal/session"
)

var ErrNotAuthenticated = errors.New("WebClass session is missing or expired; run `webclass auth`")

type Client struct {
	Base *url.URL
	HTTP *http.Client
}

type rateLimitedTransport struct {
	base     http.RoundTripper
	interval time.Duration
	mu       sync.Mutex
	lastDone time.Time
}

type rateLimitedBody struct {
	io.ReadCloser
	once    sync.Once
	onClose func()
}

func (b *rateLimitedBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.onClose)
	return err
}

func (t *rateLimitedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Keep this lock until the response body is closed. That makes the guarantee
	// stronger than merely serializing request headers: a PDF body cannot still
	// be downloading when another WebClass request starts.
	t.mu.Lock()

	if t.interval > 0 && !t.lastDone.IsZero() {
		wait := time.Until(t.lastDone.Add(t.interval))
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-req.Context().Done():
				if !timer.Stop() {
					<-timer.C
				}
				t.mu.Unlock()
				return nil, req.Context().Err()
			case <-timer.C:
			}
		}
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		t.lastDone = time.Now()
		t.mu.Unlock()
		return nil, err
	}
	if resp.Body == nil {
		t.lastDone = time.Now()
		t.mu.Unlock()
		return resp, nil
	}

	resp.Body = &rateLimitedBody{
		ReadCloser: resp.Body,
		onClose: func() {
			t.lastDone = time.Now()
			t.mu.Unlock()
		},
	}
	return resp, nil
}

func New(baseURL string) (*Client, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("base URL must be absolute")
	}
	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}

	store, err := session.Load()
	if err != nil {
		return nil, err
	}
	jar, err := store.Jar(base)
	if err != nil {
		return nil, err
	}
	return &Client{
		Base: base,
		HTTP: &http.Client{Jar: jar, Timeout: 45 * time.Second},
	}, nil
}

// SetRequestInterval enforces a minimum quiet period between completed HTTP
// requests. The transport also serializes response bodies, so pull never talks
// to WebClass concurrently even if callers become concurrent in the future.
func (c *Client) SetRequestInterval(interval time.Duration) {
	if interval < 0 {
		interval = 0
	}
	base := c.HTTP.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if existing, ok := base.(*rateLimitedTransport); ok {
		existing.mu.Lock()
		existing.interval = interval
		existing.mu.Unlock()
		return
	}
	c.HTTP.Transport = &rateLimitedTransport{base: base, interval: interval}
}

func (c *Client) resolve(ref string) (*url.URL, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return nil, err
	}
	return c.Base.ResolveReference(u), nil
}

func (c *Client) get(rawURL string) (*http.Response, error) {
	u, err := c.resolve(rawURL)
	if err != nil {
		return nil, fmt.Errorf("resolve URL: %w", err)
	}
	resp, err := c.HTTP.Get(u.String())
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(resp.Request.URL.Hostname(), c.Base.Hostname()) || strings.Contains(resp.Request.URL.Path, "login.php") {
		resp.Body.Close()
		return nil, ErrNotAuthenticated
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("GET %s: %s: %s", u, resp.Status, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

func (c *Client) document(rawURL string) (*goquery.Document, *http.Response, error) {
	resp, err := c.get(rawURL)
	if err != nil {
		return nil, nil, err
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", resp.Request.URL, err)
	}
	return doc, resp, nil
}

func (c *Client) CheckSession() error {
	_, _, err := c.document(c.Base.String())
	return err
}
