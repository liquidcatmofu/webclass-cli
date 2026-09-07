package pull

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/liquidcatmofu/webclass-cli/internal/state"
	"github.com/liquidcatmofu/webclass-cli/internal/webclass"
)

type Result struct {
	New       int
	Changed   int
	Unchanged int
	Failed    int
}

var invalidFilename = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)

func Run(client *webclass.Client, root string, course webclass.Course) (Result, error) {
	manifest, err := state.Load(root)
	if err != nil {
		return Result{}, err
	}

	fmt.Printf("Scanning %s (%s)\n", course.Name, course.ID)
	resources, err := client.Resources(course)
	if err != nil {
		return Result{}, fmt.Errorf("scan %s: %w", course.Name, err)
	}
	if len(resources) == 0 {
		fmt.Fprintln(os.Stderr, "No directly downloadable files found in material entries.")
		fmt.Fprintln(os.Stderr, "Only entries categorized exactly as 資料 are scanned; start-required materials are not entered yet.")
	}

	var result Result
	for _, resource := range resources {
		status, err := pullOne(client, root, manifest, resource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "! %s / %s: %v\n", course.Name, resource.Title, err)
			result.Failed++
			continue
		}
		switch status {
		case "new":
			result.New++
		case "changed":
			result.Changed++
		case "unchanged":
			result.Unchanged++
		}
	}
	if err := manifest.Save(root); err != nil {
		return result, err
	}
	return result, nil
}

func pullOne(client *webclass.Client, root string, manifest *state.Manifest, resource webclass.Resource) (string, error) {
	dl, err := client.OpenDownload(resource.DownloadURL)
	if err != nil {
		return "", err
	}
	defer dl.Body.Close()

	filename := sanitize(dl.Filename)
	if filename == "" || filename == "download" {
		filename = sanitize(resource.Title)
	}
	courseDir := sanitize(resource.CourseName)
	resourceDir := sanitize(resource.Title)
	if courseDir == "" {
		courseDir = resource.CourseID
	}
	if resourceDir == "" {
		resourceDir = "resource"
	}
	rel := filepath.Join(courseDir, resourceDir, filename)
	dst := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".webclass-*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(tmp, h), dl.Body)
	closeErr := tmp.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	hash := hex.EncodeToString(h.Sum(nil))
	key := resource.CourseID + "|" + resource.ID + "|" + filename
	old, exists := manifest.Entries[key]
	status := "new"
	prefix := "+"
	if exists {
		if old.SHA256 == hash {
			os.Remove(tmpName)
			fmt.Printf("= %s / %s\n", resource.CourseName, rel)
			return "unchanged", nil
		}
		status, prefix = "changed", "!"
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return "", err
	}
	manifest.Entries[key] = state.Entry{
		CourseID: resource.CourseID, CourseName: resource.CourseName,
		ResourceID: resource.ID, ResourceTitle: resource.Title,
		Filename: filename, Path: rel, SHA256: hash, Size: n, UpdatedAt: time.Now(),
	}
	fmt.Printf("%s %s / %s\n", prefix, resource.CourseName, rel)
	return status, nil
}

func sanitize(name string) string {
	name = strings.TrimSpace(invalidFilename.ReplaceAllString(name, "_"))
	name = strings.Trim(name, ". ")
	if len(name) > 180 {
		name = name[:180]
	}
	return name
}
