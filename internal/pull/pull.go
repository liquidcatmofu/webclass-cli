package pull

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	resources, stats, err := client.PullResources(course)
	if err != nil {
		return Result{}, fmt.Errorf("scan %s: %w", course.Name, err)
	}

	fmt.Printf("Materials: %d, started: %d, downloadable files: %d\n", stats.Materials, stats.Started, len(resources))
	printGroupOverview(stats)
	printGroupedIssues(stats)

	if stats.Materials == 0 {
		fmt.Fprintln(os.Stderr, "No entries categorized exactly as 資料 were found.")
		fmt.Fprintf(os.Stderr, "Diagnostic: documents=%d rows=%d frames=%d\n", stats.Documents, stats.Rows, stats.Frames)
		if len(stats.Categories) > 0 {
			parts := make([]string, 0, len(stats.Categories))
			for category, count := range stats.Categories {
				parts = append(parts, fmt.Sprintf("%s=%d", category, count))
			}
			sort.Strings(parts)
			fmt.Fprintf(os.Stderr, "Diagnostic categories: %s\n", strings.Join(parts, ", "))
		}
		for _, u := range stats.ScannedURLs {
			fmt.Fprintf(os.Stderr, "Diagnostic page: %s\n", u)
		}
	}

	var result Result
	currentGroup := ""
	for _, resource := range resources {
		group := stats.ResourceGroups[resource.ID]
		if group == "" {
			group = "(ungrouped)"
		}
		if group != currentGroup {
			fmt.Printf("\n[%s]\n", group)
			currentGroup = group
		}

		status, err := pullOne(client, root, manifest, resource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s: %v\n", resource.Title, err)
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

func printGroupOverview(stats webclass.PullResourceStats) {
	if len(stats.GroupOrder) == 0 {
		return
	}
	fmt.Println("Groups:")
	for _, group := range stats.GroupOrder {
		categories := stats.GroupCategories[group]
		parts := make([]string, 0, len(categories))
		for category, count := range categories {
			parts = append(parts, fmt.Sprintf("%s=%d", category, count))
		}
		sort.Strings(parts)
		suffix := ""
		if categories["資料"] == 0 {
			suffix = " (not opened)"
		}
		fmt.Printf("  %s: %s%s\n", group, strings.Join(parts, ", "), suffix)
	}
}

func printGroupedIssues(stats webclass.PullResourceStats) {
	for _, group := range stats.GroupOrder {
		var lines []string
		for _, item := range stats.SkippedLimited {
			if item.Group == group {
				lines = append(lines, "skip (execution limit): "+item.Title)
			}
		}
		for _, item := range stats.SkippedInteractive {
			if item.Group == group {
				lines = append(lines, "skip (requires input): "+item.Title)
			}
		}
		for _, item := range stats.NoFiles {
			if item.Group == group {
				lines = append(lines, "no downloadable file: "+item.Title)
			}
		}
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintf(os.Stderr, "\n[%s]\n", group)
		for _, line := range lines {
			fmt.Fprintf(os.Stderr, "  - %s\n", line)
		}
	}
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
			fmt.Printf("  = %s\n", rel)
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
	fmt.Printf("  %s %s\n", prefix, rel)
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
