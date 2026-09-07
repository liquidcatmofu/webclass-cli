package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/liquidcatmofu/webclass-cli/internal/session"
	"github.com/liquidcatmofu/webclass-cli/internal/webclass"
)

const coursesFilename = "courses.json"

type Course struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CourseCache struct {
	Version int      `json:"version"`
	Courses []Course `json:"courses"`
}

func coursesPath() (string, error) {
	dir, err := session.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, coursesFilename), nil
}

func SaveCourses(courses []webclass.Course) error {
	path, err := coursesPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	cache := CourseCache{Version: 1, Courses: make([]Course, 0, len(courses))}
	for _, course := range courses {
		if course.ID == "" {
			continue
		}
		cache.Courses = append(cache.Courses, Course{ID: course.ID, Name: course.Name})
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("encode course cache: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write course cache: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace course cache: %w", err)
	}
	return nil
}

func LoadCourses() ([]Course, error) {
	path, err := coursesPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read course cache: %w", err)
	}
	var cache CourseCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("decode course cache: %w", err)
	}
	return cache.Courses, nil
}
