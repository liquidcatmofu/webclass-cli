package pull

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/liquidcatmofu/webclass-cli/internal/state"
	"github.com/liquidcatmofu/webclass-cli/internal/webclass"
)

func TestValidMode(t *testing.T) {
	for _, mode := range []string{ModeNew, ModeFiles, ModeFull} {
		if !ValidMode(mode) {
			t.Fatalf("ValidMode(%q) = false", mode)
		}
	}
	if ValidMode("everything") {
		t.Fatal("unexpected mode accepted")
	}
}

func TestMaterialKnownMigratesOldManifestEntry(t *testing.T) {
	manifest := &state.Manifest{
		Version: 2,
		Entries: map[string]state.Entry{
			"old": {CourseID: "course", ResourceID: "material", ResourceTitle: "Known material"},
		},
		Materials: map[string]state.MaterialEntry{},
	}
	material := webclass.Resource{CourseID: "course", CourseName: "Course", ID: "material", Title: "Known material"}
	if !materialKnown(manifest, material, "Materials") {
		t.Fatal("old file entry should make material known")
	}
	if _, ok := manifest.Materials["course|material"]; !ok {
		t.Fatal("old manifest entry was not migrated to material-level state")
	}
}

func TestDownloadFilenameHintForKnownLinkTypes(t *testing.T) {
	tests := []struct {
		name     string
		resource webclass.Resource
		want     string
	}{
		{
			name: "attachment",
			resource: webclass.Resource{
				Title: "Week01",
				DownloadURL: "https://example.invalid/webclass/file_down.php?target_type=attach&file=abc&file_name=slides.pdf",
			},
			want: "slides.pdf",
		},
		{
			name: "opaque textbook pdf",
			resource: webclass.Resource{
				Title: "第2回 正規表現",
				DownloadURL: "https://example.invalid/webclass/data/course/x/b0b6db7f1cf1e354.pdf",
			},
			want: "第2回 正規表現.pdf",
		},
		{
			name: "textbook html page",
			resource: webclass.Resource{
				Title: "強化学習(5)",
				DownloadURL: "https://example.invalid/webclass/data/course/x/c25ce1b735b8764fa3e097c81eea860e.html#kind=webclass-cli-textbook-html&page=2&pages=2&title=%E5%BC%B7%E5%8C%96%E5%AD%A6%E7%BF%92%285%29",
			},
			want: "強化学習(5) - 02.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := downloadFilenameHint(tt.resource)
			if !ok || got != tt.want {
				t.Fatalf("downloadFilenameHint() = %q, %v; want %q, true", got, ok, tt.want)
			}
		})
	}
}

func TestKnownFileCanSkipOnlyWhenLocalPathExists(t *testing.T) {
	root := t.TempDir()
	resource := webclass.Resource{
		CourseID: "course", CourseName: "Course", ID: "material", Title: "Material",
		DownloadURL: "https://example.invalid/webclass/file_down.php?file=abc&file_name=slides.pdf",
	}
	rel := resourcePath(resource, "Materials", "slides.pdf", Options{GroupDirs: true})
	manifest := &state.Manifest{Version: 2, Entries: map[string]state.Entry{
		"course|material|slides.pdf": {
			CourseID: "course", ResourceID: "material", Filename: "slides.pdf", Path: rel,
		},
	}, Materials: map[string]state.MaterialEntry{}}

	if _, ok := knownFileCanSkip(root, manifest, resource, "Materials", Options{GroupDirs: true}); ok {
		t.Fatal("missing local file must not be skipped")
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, rel), []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := knownFileCanSkip(root, manifest, resource, "Materials", Options{GroupDirs: true}); !ok || got != rel {
		t.Fatalf("knownFileCanSkip() = %q, %v; want %q, true", got, ok, rel)
	}
}
