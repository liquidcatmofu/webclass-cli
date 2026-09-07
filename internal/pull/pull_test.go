package pull

import (
	"path/filepath"
	"testing"

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
