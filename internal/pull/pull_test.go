package pull

import (
	"path/filepath"
	"testing"

	"github.com/liquidcatmofu/webclass-cli/internal/state"
	"github.com/liquidcatmofu/webclass-cli/internal/webclass"
)

func TestResourcePathDefaultLayout(t *testing.T) {
	resource := webclass.Resource{
		CourseID:   "02_26036",
		CourseName: "02_言語解析演習(2026)",
		Title:      "第1回 形式言語，正規言語",
	}
	got := resourcePath(resource, "資料", "slides.pdf", Options{})
	want := filepath.Join("02_言語解析演習(2026)", "第1回 形式言語，正規言語", "slides.pdf")
	if got != want {
		t.Fatalf("resourcePath() = %q, want %q", got, want)
	}
}

func TestResourcePathWithGroupDirs(t *testing.T) {
	resource := webclass.Resource{
		CourseID:   "02_26036",
		CourseName: "02_言語解析演習(2026)",
		Title:      "模範解答（第8回演習問題）",
	}
	got := resourcePath(resource, "達成度試験対策", "answer.pdf", Options{GroupDirs: true})
	want := filepath.Join("02_言語解析演習(2026)", "達成度試験対策", "模範解答（第8回演習問題）", "answer.pdf")
	if got != want {
		t.Fatalf("resourcePath() = %q, want %q", got, want)
	}
}

func TestResourcePathUngroupedDoesNotCreateSyntheticDirectory(t *testing.T) {
	resource := webclass.Resource{CourseID: "02_26036", CourseName: "course", Title: "material"}
	got := resourcePath(resource, "(ungrouped)", "file.pdf", Options{GroupDirs: true})
	want := filepath.Join("course", "material", "file.pdf")
	if got != want {
		t.Fatalf("resourcePath() = %q, want %q", got, want)
	}
}

func TestDownloadFilenameUsesMaterialTitleForOpaqueTextbookPDF(t *testing.T) {
	resource := webclass.Resource{
		Title:       "第2回 正規表現，NFAへの変換",
		DownloadURL: "https://webclass.example/webclass/data/course/02/02_26036/88e2/b0b6db7f1cf1e354.pdf",
	}
	dl := &webclass.Download{
		Filename: "b0b6db7f1cf1e354.pdf",
		URL:      "https://webclass.example/webclass/data/course/02/02_26036/88e2/b0b6db7f1cf1e354.pdf",
	}

	got, renamed := downloadFilename(resource, dl)
	want := "第2回 正規表現，NFAへの変換.pdf"
	if got != want || !renamed {
		t.Fatalf("downloadFilename() = %q, %v; want %q, true", got, renamed, want)
	}
}

func TestDownloadFilenamePreservesAttachmentFileName(t *testing.T) {
	resource := webclass.Resource{
		Title:       "Week01:Operations Research",
		DownloadURL: "https://webclass.example/webclass/file_down.php?target_type=attach&file=a5a3&file_name=Week01_SystemsEngineering_2026.pdf",
	}
	dl := &webclass.Download{
		Filename: "Week01_SystemsEngineering_2026.pdf",
		URL:      "https://webclass.example/webclass/download.php/Week01_SystemsEngineering_2026.pdf?file=a5a3&file_name=Week01_SystemsEngineering_2026.pdf",
	}

	got, renamed := downloadFilename(resource, dl)
	if got != "Week01_SystemsEngineering_2026.pdf" || renamed {
		t.Fatalf("downloadFilename() = %q, %v; want attachment filename, false", got, renamed)
	}
}

func TestDownloadFilenameDoesNotRenameExplicitHexAttachment(t *testing.T) {
	resource := webclass.Resource{
		Title:       "material",
		DownloadURL: "https://webclass.example/webclass/file_down.php?file_name=b0b6db7f1cf1e354.pdf",
	}
	dl := &webclass.Download{
		Filename: "b0b6db7f1cf1e354.pdf",
		URL:      "https://webclass.example/webclass/download.php/b0b6db7f1cf1e354.pdf?file_name=b0b6db7f1cf1e354.pdf",
	}

	got, renamed := downloadFilename(resource, dl)
	if got != "b0b6db7f1cf1e354.pdf" || renamed {
		t.Fatalf("downloadFilename() = %q, %v; explicit attachment filename must be preserved", got, renamed)
	}
}

func TestFindOpaqueManifestEntryForRenameMigration(t *testing.T) {
	resource := webclass.Resource{CourseID: "02_26036", ID: "contents-id"}
	manifest := &state.Manifest{Entries: map[string]state.Entry{
		"old": {
			CourseID:   "02_26036",
			ResourceID: "contents-id",
			Filename:   "b0b6db7f1cf1e354.pdf",
			SHA256:     "same-hash",
		},
		"other": {
			CourseID:   "02_26036",
			ResourceID: "other-contents",
			Filename:   "aaaaaaaaaaaaaaaa.pdf",
			SHA256:     "same-hash",
		},
	}}

	key, entry, ok := findOpaqueManifestEntry(manifest, resource, "same-hash")
	if !ok || key != "old" || entry.Filename != "b0b6db7f1cf1e354.pdf" {
		t.Fatalf("findOpaqueManifestEntry() = %q, %#v, %v", key, entry, ok)
	}
}
