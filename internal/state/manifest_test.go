package state

import (
	"path/filepath"
	"testing"
)

func TestManifestRoundTrip(t *testing.T) {
	root := t.TempDir()
	m, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	m.Entries["x"] = Entry{CourseID: "course", Filename: "file.pdf", SHA256: "abc"}
	m.Materials["course|material"] = MaterialEntry{
		CourseID: "course", ResourceID: "material", ResourceTitle: "資料", Group: "Materials",
	}
	if err := m.Save(root); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Entries["x"].Filename != "file.pdf" {
		t.Fatalf("round trip failed: %#v", got.Entries["x"])
	}
	if got.Materials["course|material"].ResourceTitle != "資料" {
		t.Fatalf("material round trip failed: %#v", got.Materials["course|material"])
	}
	if got.Version != 2 {
		t.Fatalf("manifest version = %d, want 2", got.Version)
	}
	if filepath.Base(filepath.Join(root, Filename)) != Filename {
		t.Fatal("unexpected manifest filename")
	}
}
