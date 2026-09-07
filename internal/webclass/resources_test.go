package webclass

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestStableResourceIDDropsTemporaryQuery(t *testing.T) {
	got := stableResourceID("https://webclass.example/webclass/course.php/abc/resource?id=1&acs_=temporary")
	want := "/webclass/course.php/abc/resource"
	if got != want {
		t.Fatalf("stableResourceID() = %q, want %q", got, want)
	}
}

func TestLooksDownloadable(t *testing.T) {
	for _, raw := range []string{
		"https://example.invalid/webclass/file_down.php?file=x",
		"https://example.invalid/webclass/download.php/foo",
		"https://example.invalid/material.pdf",
	} {
		if !looksDownloadable(raw) {
			t.Errorf("looksDownloadable(%q) = false", raw)
		}
	}
	if looksDownloadable("https://example.invalid/course.php/123") {
		t.Error("course page should not be considered directly downloadable")
	}
}

func TestHasExecutionLimit(t *testing.T) {
	tests := []struct {
		name string
		html string
		want bool
	}{
		{"not shown", `<html><body><button>開始</button></body></html>`, false},
		{"limited count", `<html><body>実行回数：1回</body></html>`, true},
		{"limited upper bound", `<html><body>実行回数の制限 上限 2 回</body></html>`, true},
		{"unlimited", `<html><body>実行回数：無制限</body></html>`, false},
		{"no limit", `<html><body>実行回数の制限：なし</body></html>`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			if err != nil {
				t.Fatal(err)
			}
			if got := hasExecutionLimit(doc); got != tt.want {
				t.Fatalf("hasExecutionLimit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindStartForm(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`
		<form action="/start" method="post">
			<input type="hidden" name="acs_" value="token">
			<input type="submit" name="start" value="開始">
		</form>`))
	if err != nil {
		t.Fatal(err)
	}
	form, name, value, interactive := findStartForm(doc)
	if form == nil {
		t.Fatal("start form not found")
	}
	if name != "start" || value != "開始" {
		t.Fatalf("submitter = %q/%q", name, value)
	}
	if interactive {
		t.Fatal("hidden-only form must not be interactive")
	}
}

func TestFindStartFormRejectsPasswordInput(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`
		<form action="/start" method="post">
			<input type="password" name="password">
			<button type="submit" name="start" value="1">開始</button>
		</form>`))
	if err != nil {
		t.Fatal(err)
	}
	form, _, _, interactive := findStartForm(doc)
	if form == nil {
		t.Fatal("start form not found")
	}
	if !interactive {
		t.Fatal("password form must be interactive")
	}
}
