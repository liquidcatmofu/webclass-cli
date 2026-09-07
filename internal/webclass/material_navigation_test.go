package webclass

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
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
		fmt.Fprint(w, `<html><a onclick="filedownload('/webclass/file_down.php?file=x')">download</a></html>`)
	})

	base, err := url.Parse(server.URL + "/webclass/")
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Base: base, HTTP: server.Client()}

	links, state, err := client.materialDownloadLinksV2(server.URL + "/webclass/do_contents.php?reset_status=1&set_contents_id=abc")
	if err != nil {
		t.Fatal(err)
	}
	if state != "started" {
		t.Fatalf("state = %q, want started", state)
	}
	if len(links) != 1 {
		t.Fatalf("links = %#v, want exactly one", links)
	}
	want := server.URL + "/webclass/file_down.php?file=x"
	if links[0] != want {
		t.Fatalf("link = %q, want %q", links[0], want)
	}
}

func TestPullResourcesKeepsGroupsAndOnlyOpensMaterials(t *testing.T) {
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
		if r.URL.Query().Get("set_contents_id") != "material-id" {
			t.Fatalf("non-material content was opened: %s", r.URL.String())
		}
		fmt.Fprint(w, `<script>window.top.location.href="/webclass/material-frame";</script>`)
	})
	mux.HandleFunc("/webclass/material-frame", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<a href="/webclass/file_down.php?file=material.pdf">material</a>`)
	})

	base, err := url.Parse(server.URL + "/webclass/")
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Base: base, HTTP: server.Client()}
	course := Course{ID: "02_26036", Name: "言語解析演習", URL: client.courseIndexURL("02_26036")}

	resources, stats, err := client.PullResources(course)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 {
		t.Fatalf("resources = %#v, want one material", resources)
	}
	if resources[0].ID != "material-id" {
		t.Fatalf("resource ID = %q, want data-contents-id", resources[0].ID)
	}
	if stats.ResourceGroups["material-id"] != "資料" {
		t.Fatalf("material group = %q", stats.ResourceGroups["material-id"])
	}
	if stats.GroupCategories["課題提出"]["レポート"] != 1 {
		t.Fatalf("report group summary = %#v", stats.GroupCategories["課題提出"])
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
