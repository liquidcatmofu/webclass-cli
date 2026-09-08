package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const Filename = ".webclass-manifest.json"

type Entry struct {
	CourseID      string    `json:"course_id"`
	CourseName    string    `json:"course_name"`
	ResourceID    string    `json:"resource_id"`
	ResourceTitle string    `json:"resource_title"`
	Filename      string    `json:"filename"`
	Path          string    `json:"path"`
	SHA256        string    `json:"sha256"`
	Size          int64     `json:"size"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type MaterialEntry struct {
	CourseID      string    `json:"course_id"`
	CourseName    string    `json:"course_name"`
	ResourceID    string    `json:"resource_id"`
	ResourceTitle string    `json:"resource_title"`
	Group         string    `json:"group,omitempty"`
	SeenAt        time.Time `json:"seen_at"`
}

type Manifest struct {
	Version   int                      `json:"version"`
	Entries   map[string]Entry         `json:"entries"`
	Materials map[string]MaterialEntry `json:"materials,omitempty"`
}

func Load(root string) (*Manifest, error) {
	path := filepath.Join(root, Filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Manifest{Version: 2, Entries: map[string]Entry{}, Materials: map[string]MaterialEntry{}}, nil
		}
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if m.Entries == nil {
		m.Entries = map[string]Entry{}
	}
	if m.Materials == nil {
		m.Materials = map[string]MaterialEntry{}
	}
	if m.Version < 2 {
		m.Version = 2
	}
	return &m, nil
}

func (m *Manifest) Save(root string) error {
	if m.Version < 2 {
		m.Version = 2
	}
	if m.Entries == nil {
		m.Entries = map[string]Entry{}
	}
	if m.Materials == nil {
		m.Materials = map[string]MaterialEntry{}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(root, Filename)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
