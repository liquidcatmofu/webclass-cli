package webclass

import "testing"

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
