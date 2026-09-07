package auth

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"

	"github.com/liquidcatmofu/webclass-cli/internal/session"
)

func BrowserLogin(ctx context.Context, base *url.URL, browserBin string) error {
	profileDir, err := session.BrowserProfileDir()
	if err != nil {
		return err
	}

	if strings.TrimSpace(browserBin) == "" {
		var found bool
		browserBin, found = launcher.LookPath()
		if !found {
			return fmt.Errorf("no installed Chromium-based browser found; install Chrome/Chromium/Edge or pass --browser PATH")
		}
	}

	// Do not let Rod download its pinned Chromium build. Besides avoiding a large
	// implicit download, this also avoids antivirus false positives around
	// downloaded browser binaries on Windows.
	//
	// Leakless is disabled as well. Rod enables it by default and materializes a
	// helper executable on Windows; that helper has a history of Defender false
	// positives. This auth flow closes the browser explicitly, so the extra
	// watchdog process is unnecessary here.
	l := launcher.New().
		Bin(browserBin).
		Leakless(false).
		UserDataDir(profileDir).
		Headless(false)

	controlURL, err := l.Launch()
	if err != nil {
		return fmt.Errorf("launch browser %q: %w", browserBin, err)
	}

	browser := rod.New().ControlURL(controlURL).Context(ctx)
	if err := browser.Connect(); err != nil {
		return fmt.Errorf("connect to browser: %w", err)
	}
	defer browser.Close()

	fmt.Printf("Using browser: %s\n", browserBin)

	loginURL := base.ResolveReference(&url.URL{Path: "login.php", RawQuery: "auth_mode=SAML"}).String()
	page, err := browser.Page(protoTarget(loginURL))
	if err != nil {
		return fmt.Errorf("open login page: %w", err)
	}

	fmt.Println("Complete sign-in and two-factor authentication in the opened browser.")
	fmt.Println("Waiting for WebClass login to complete...")

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			info, err := page.Info()
			if err != nil {
				continue
			}
			u, err := url.Parse(info.URL)
			if err != nil || !sameHost(u.Hostname(), base.Hostname()) {
				continue
			}
			if !strings.HasPrefix(u.Path, base.Path) || strings.Contains(u.Path, "login.php") {
				continue
			}

			cookies, err := browser.GetCookies()
			if err != nil {
				return fmt.Errorf("read browser cookies: %w", err)
			}
			var saved []session.Cookie
			for _, c := range cookies {
				if !domainMatches(c.Domain, base.Hostname()) {
					continue
				}
				expires := int64(0)
				if c.Expires > 0 {
					expires = int64(c.Expires)
				}
				saved = append(saved, session.Cookie{
					Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path,
					Expires: expires, Secure: c.Secure, HTTPOnly: c.HTTPOnly,
				})
			}
			if len(saved) == 0 {
				continue
			}
			if err := session.Save(saved); err != nil {
				return err
			}
			fmt.Printf("Saved %d WebClass cookie(s).\n", len(saved))
			return nil
		}
	}
}

func sameHost(a, b string) bool { return strings.EqualFold(a, b) }

func domainMatches(domain, host string) bool {
	domain = strings.TrimPrefix(strings.ToLower(domain), ".")
	host = strings.ToLower(host)
	return host == domain || strings.HasSuffix(host, "."+domain)
}
