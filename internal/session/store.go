package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const appDirName = "webclass-cli"

type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Expires  int64  `json:"expires,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	HTTPOnly bool   `json:"http_only,omitempty"`
}

type Store struct {
	Cookies []Cookie `json:"cookies"`
}

func ConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve config directory: %w", err)
	}
	return filepath.Join(dir, appDirName), nil
}

func SessionPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

func BrowserProfileDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "browser-profile"), nil
}

func Save(cookies []Cookie) error {
	path, err := SessionPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(Store{Cookies: cookies}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write session: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace session: %w", err)
	}
	return nil
}

func Load() (Store, error) {
	path, err := SessionPath()
	if err != nil {
		return Store{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Store{}, fmt.Errorf("no saved session; run `webclass auth` first")
		}
		return Store{}, fmt.Errorf("read session: %w", err)
	}
	var store Store
	if err := json.Unmarshal(data, &store); err != nil {
		return Store{}, fmt.Errorf("decode session: %w", err)
	}
	return store, nil
}

func (s Store) Jar(base *url.URL) (http.CookieJar, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}
	now := time.Now()
	byURL := map[string][]*http.Cookie{}
	for _, c := range s.Cookies {
		if c.Expires > 0 && time.Unix(c.Expires, 0).Before(now) {
			continue
		}
		domain := strings.TrimPrefix(c.Domain, ".")
		if domain == "" {
			domain = base.Hostname()
		}
		scheme := "http"
		if c.Secure || strings.EqualFold(base.Scheme, "https") {
			scheme = "https"
		}
		u := &url.URL{Scheme: scheme, Host: domain, Path: "/"}
		path := c.Path
		if path == "" {
			path = "/"
		}
		hc := &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     path,
			Secure:   c.Secure,
			HttpOnly: c.HTTPOnly,
		}
		if c.Expires > 0 {
			hc.Expires = time.Unix(c.Expires, 0)
		}
		byURL[u.String()] = append(byURL[u.String()], hc)
	}
	for raw, cookies := range byURL {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse cookie origin: %w", err)
		}
		jar.SetCookies(u, cookies)
	}
	return jar, nil
}
