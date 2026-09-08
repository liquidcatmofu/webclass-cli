package webclass

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestMaterialDownloadLinksFollowsTopLocationAndFrames(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/webclass/do_contents.php", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><script>window.top.location.href="/webclass/content-frame";</script></html>`)
	})
	mux.HandleFunc("/webclass/content-frame", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><frameset><frame name="webclass_chapter" src="/webclass/chapter"></frameset></html>`)
	})
	mux.HandleFunc("/webclass/chapter", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body>
<form name="menu" method="POST" action="/webclass/chapter-close">
  <input type="hidden" name="s" value="state-token">
  <input type="hidden" name="sendCmd" value="">
  <input type="button" name="quit" value="資料を閉じる" onclick="window.quit()">
</form>
<a onclick="filedownload('/webclass/file_down.php?file=x')">download</a>
</body></html>`)
	})

	base, err := url.Parse(server.URL + "/webclass/")
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Base: base, HTTP: server.Client()}

	session, err := client.OpenMaterialSession(server.URL + "/webclass/do_contents.php?reset_status=1&set_contents_id=abc")
	if err != nil {
		t.Fatal(err)
	}
	if session.State != "started" || !session.Started {
		t.Fatalf("session = %#v, want started", session)
	}
	if session.Close == nil {
		t.Fatal("started material must retain its close action")
	}
	if len(session.Links) != 1 {
		t.Fatalf("links = %#v, want exactly one", session.Links)
	}
	want := server.URL + "/webclass/file_down.php?file=x"
	if session.Links[0] != want {
		t.Fatalf("link = %q, want %q", session.Links[0], want)
	}
}

func TestPullPlanKeepsGroupsAndDoesNotOpenMaterials(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/webclass/course.php/02_26036/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body>
<section class="panel panel-default cl-contentsList_folder">
  <div class="panel-heading"><h4 class="panel-title">資料</h4></div>
  <section class="list-group-item cl-contentsList_listGroupItem" data-contents-id="material-id">
    <h4 class="cm-contentsList_contentName"><a href="/webclass/do_contents.php?reset_status=1&set_contents_id=material-id">第1回</a></h4>
    <div class="cl-contentsList_categoryLabel">資料</div>
  </section>
</section>
<section class="panel panel-default cl-contentsList_folder">
  <div class="panel-heading"><h4 class="panel-title">課題提出</h4></div>
  <section class="list-group-item cl-contentsList_listGroupItem" data-contents-id="report-id">
    <h4 class="cm-contentsList_contentName"><a href="/webclass/do_contents.php?reset_status=1&set_contents_id=report-id">提出</a></h4>
    <div class="cl-contentsList_categoryLabel">レポート</div>
  </section>
</section>
</body></html>`)
	})
	mux.HandleFunc("/webclass/do_contents.php", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("PullPlan must not open material content: %s", r.URL.String())
	})

	base, err := url.Parse(server.URL + "/webclass/")
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Base: base, HTTP: server.Client()}
	course := Course{ID: "02_26036", Name: "言語解析演習", URL: client.courseIndexURL("02_26036")}

	materials, stats, err := client.PullPlan(course)
	if err != nil {
		t.Fatal(err)
	}
	if len(materials) != 1 {
		t.Fatalf("materials = %#v, want one material candidate", materials)
	}
	if materials[0].ID != "material-id" {
		t.Fatalf("resource ID = %q, want data-contents-id", materials[0].ID)
	}
	if stats.ResourceGroups["material-id"] != "資料" {
		t.Fatalf("material group = %q", stats.ResourceGroups["material-id"])
	}
	if stats.GroupCategories["課題提出"]["レポート"] != 1 {
		t.Fatalf("report group summary = %#v", stats.GroupCategories["課題提出"])
	}
}

func TestCloseMaterialPostsQuitCommand(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	called := false
	mux.HandleFunc("/webclass/txtbk_show_chapter.php", func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("sendCmd"); got != "quit" {
			t.Fatalf("sendCmd = %q, want quit", got)
		}
		if got := r.Form.Get("s"); got != "state-token" {
			t.Fatalf("s = %q, want preserved hidden value", got)
		}
		fmt.Fprint(w, `<html><body>closed</body></html>`)
	})

	base, err := url.Parse(server.URL + "/webclass/")
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Base: base, HTTP: server.Client()}
	chapterURL, _ := url.Parse(server.URL + "/webclass/current-chapter")
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><body>
<form name="menu" method="POST" action="/webclass/txtbk_show_chapter.php">
  <input type="hidden" name="s" value="state-token">
  <input type="hidden" name="sendCmd" value="">
  <input type="button" name="quit" value="資料を閉じる" onclick="window.quit()">
</form>
</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	action := closeActionFromDocument(doc, chapterURL, base.Hostname())
	if action == nil {
		t.Fatal("close action not found")
	}
	if err := client.CloseMaterial(action); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("close endpoint was not called")
	}
}

func TestExecutionLimitTextDoesNotConfuseUsageCount(t *testing.T) {
	if hasExecutionLimitText("利用回数 20") {
		t.Fatal("usage count must not be treated as an execution limit")
	}
	if !hasExecutionLimitText("実行回数：1回") {
		t.Fatal("explicit execution limit must be detected")
	}
	if hasExecutionLimitText("実行回数：無制限") {
		t.Fatal("unlimited must not be treated as limited")
	}
}
