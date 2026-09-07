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
	if filepath.Base(filepath.Join(root, Filename)) != Filename {
		t.Fatal("unexpected manifest filename")
	}
}
