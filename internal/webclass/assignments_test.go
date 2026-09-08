package webclass

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestAssignmentsReadsCourseAndScoresWithoutOpeningContents(t *testing.T) {
	openedContents := false
	mux := http.NewServeMux()
	mux.HandleFunc("/webclass/course.php/C1/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body>
<div class="cl-contentsList_listGroupItem" data-contents-id="material">
  <div class="cm-contentsList_contentName"><a href="/webclass/do_contents.php?set_contents_id=material">講義資料</a></div>
  <span class="cl-contentsList_categoryLabel">資料</span>
</div>
<div class="cl-contentsList_listGroupItem" data-contents-id="report">
  <div class="cm-contentsList_contentName"><a href="/webclass/do_contents.php?set_contents_id=report">レポート1</a></div>
  <span class="cl-contentsList_categoryLabel">レポート</span>
  <span class="cm-contentsList_contentDetailListItemData">2026/09/01 00:00 - 2026/09/12 23:59</span>
</div>
<div class="cl-contentsList_listGroupItem" data-contents-id="quiz">
  <div class="cm-contentsList_contentName"><div>確認テスト</div><span class="cl-contentsList_new">New</span></div>
  <span class="cl-contentsList_categoryLabel">自習</span>
  <span class="cm-contentsList_contentDetailListItemData">締め切り: 2026/09/10 18:30</span>
</div>
<div class="cl-contentsList_listGroupItem" data-contents-id="retry">
  <div class="cm-contentsList_contentName">再提出レポート</div>
  <span class="cl-contentsList_categoryLabel">レポート</span>
  <span class="text-danger">再提出が必要です。</span>
</div>
</body></html>`))
	})
	mux.HandleFunc("/webclass/course.php/C1/scores", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<table id="PersonalScoreSheet">
<tr><th class="contents-title">レポート1</th><td>未</td></tr>
<tr><th class="contents-title">確認テスト</th><td>8</td></tr>
<tr><th class="contents-title">再提出レポート</th><td>10</td></tr>
</table>`))
	})
	mux.HandleFunc("/webclass/do_contents.php", func(w http.ResponseWriter, r *http.Request) {
		openedContents = true
		http.Error(w, "must not open", http.StatusInternalServerError)
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	base, err := url.Parse(server.URL + "/webclass/")
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Base: base, HTTP: server.Client()}
	course := Course{ID: "C1", Name: "テスト科目", URL: server.URL + "/webclass/course.php/C1/"}

	got, err := client.Assignments(course)
	if err != nil {
		t.Fatal(err)
	}
	if openedContents {
		t.Fatal("Assignments opened do_contents.php")
	}
	if len(got) != 3 {
		t.Fatalf("len(assignments) = %d, want 3: %#v", len(got), got)
	}

	// Deadline sorting puts 確認テスト before レポート1; undated retry is last.
	if got[0].Title != "確認テスト" || got[0].DeadlineText != "2026/09/10 18:30" || got[0].SubmissionStatus != SubmissionSubmitted {
		t.Fatalf("first assignment = %#v", got[0])
	}
	if got[1].Title != "レポート1" || got[1].DeadlineText != "2026/09/12 23:59" || got[1].SubmissionStatus != SubmissionPending {
		t.Fatalf("second assignment = %#v", got[1])
	}
	if got[2].Title != "再提出レポート" || got[2].SubmissionStatus != SubmissionResubmit {
		t.Fatalf("third assignment = %#v", got[2])
	}
}

func TestParseAssignmentDeadline(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"締め切り： 2026/09/20 12:34", "2026/09/20 12:34"},
		{"2026/09/01 09:00 - 2026/10/02 23:59", "2026/10/02 23:59"},
	}
	for _, tt := range tests {
		_, got, ok := parseAssignmentDeadline(tt.input)
		if !ok || got != tt.want {
			t.Fatalf("parseAssignmentDeadline(%q) = %q, %v; want %q, true", tt.input, got, ok, tt.want)
		}
	}
}
