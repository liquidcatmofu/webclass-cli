package webclass

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestTextbookHTMLBodyLinksFromJSONData(t *testing.T) {
	base, err := url.Parse("https://example.test/webclass/chapter")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><body>
<input type="hidden" name="contents_name" value="強化学習(5)">
<script id="json-data" type="application/json">{
  "text_url":"/webclass/txtbk_show_text.php?page=1&file=aaaaaaaaaaaaaaaa.html&contents_url=%2Fwebclass%2Fdata%2Fcourse%2F01%2Fdemo%2F",
  "text_urls":{
    "1":"/webclass/txtbk_show_text.php?page=1&file=aaaaaaaaaaaaaaaa.html&contents_url=%2Fwebclass%2Fdata%2Fcourse%2F01%2Fdemo%2F",
    "2":"/webclass/txtbk_show_text.php?page=2&file=bbbbbbbbbbbbbbbb.html&contents_url=%2Fwebclass%2Fdata%2Fcourse%2F01%2Fdemo%2F"
  }
}</script>
</body></html>`))
	if err != nil {
		t.Fatal(err)
	}

	links := textbookHTMLBodyLinks(doc, base, base.Hostname())
	if len(links) != 2 {
		t.Fatalf("links = %#v, want 2", links)
	}
	for i, raw := range links {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		wantPath := "/webclass/data/course/01/demo/aaaaaaaaaaaaaaaa.html"
		if i == 1 {
			wantPath = "/webclass/data/course/01/demo/bbbbbbbbbbbbbbbb.html"
		}
		if u.Path != wantPath {
			t.Fatalf("link[%d] path = %q, want %q", i, u.Path, wantPath)
		}
		title, page, total, ok := textbookHTMLInfo(raw)
		if !ok || title != "強化学習(5)" || page != i+1 || total != 2 {
			t.Fatalf("link[%d] info = %q %d/%d ok=%v", i, title, page, total, ok)
		}
	}
}

func TestOpenPullDownloadPreservesHTMLBody(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	followed := false
	mux.HandleFunc("/webclass/data/course/01/demo/aaaaaaaaaaaaaaaa.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><body><a href="/webclass/should-not-follow">first link</a><h1>教材本文</h1></body></html>`)
	})
	mux.HandleFunc("/webclass/should-not-follow", func(w http.ResponseWriter, r *http.Request) {
		followed = true
		fmt.Fprint(w, "wrong")
	})

	base, err := url.Parse(server.URL + "/webclass/")
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Base: base, HTTP: server.Client()}
	direct, _ := url.Parse(server.URL + "/webclass/data/course/01/demo/aaaaaaaaaaaaaaaa.html")
	raw := markTextbookHTMLURL(direct, "強化学習(5)", 1, 2)

	dl, err := client.OpenPullDownload(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Body.Close()
	body, err := io.ReadAll(dl.Body)
	if err != nil {
		t.Fatal(err)
	}
	if followed {
		t.Fatal("HTML textbook body must not be treated as a landing page")
	}
	if !strings.Contains(string(body), "教材本文") {
		t.Fatalf("body = %q", body)
	}
	if dl.Filename != "強化学習(5) - 01.html" {
		t.Fatalf("filename = %q", dl.Filename)
	}
}

func TestDiscoverMaterialViewTraversesSrcdoc(t *testing.T) {
	base, err := url.Parse("https://example.test/webclass/current")
	if err != nil {
		t.Fatal(err)
	}
	srcdoc := `<!DOCTYPE html><html><body>
<form name="menu" method="POST" action="/webclass/close">
<input type="hidden" name="sendCmd" value="">
<input type="button" value="資料を閉じる">
</form>
<a onclick="filedownload('/webclass/file_down.php?file=x')">添付資料</a>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><frameset><frame srcdoc="` + strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;").Replace(srcdoc) + `"></frameset></html>`))
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Base: base, HTTP: http.DefaultClient}
	links, closeAction, err := client.discoverMaterialView(doc, base, 0, map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || !strings.Contains(links[0], "file_down.php") {
		t.Fatalf("links = %#v", links)
	}
	if closeAction == nil || !closeAction.ImpliesStarted {
		t.Fatalf("close action = %#v", closeAction)
	}
}
