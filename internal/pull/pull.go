package pull

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
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

type Options struct {
	GroupDirs bool
	Material  string
}

var (
	invalidFilename   = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)
	opaquePDFFilename = regexp.MustCompile(`(?i)^[0-9a-f]{16,64}\.pdf$`)
)

func Run(client *webclass.Client, root string, course webclass.Course, options Options) (Result, error) {
	manifest, err := state.Load(root)
	if err != nil {
		return Result{}, err
	}

	fmt.Printf("Scanning %s (%s)\n", course.Name, course.ID)
	materials, stats, err := client.PullPlan(course)
	if err != nil {
		return Result{}, fmt.Errorf("scan %s: %w", course.Name, err)
	}

	materials, err = selectMaterials(materials, stats, options.Material)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(options.Material) == "" {
		fmt.Printf("Materials: %d, candidates: %d\n", stats.Materials, len(materials))
	} else {
		fmt.Printf("Materials: %d, selected: %d (%q)\n", stats.Materials, len(materials), options.Material)
	}
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
	started, closed, downloadable := 0, 0, 0
	currentGroup := ""

	stop := func(cause error) (Result, error) {
		if saveErr := manifest.Save(root); saveErr != nil {
			return result, fmt.Errorf("%v; additionally failed to save manifest: %w", cause, saveErr)
		}
		return result, cause
	}

	for i, material := range materials {
		group := stats.ResourceGroups[material.ID]
		if group == "" {
			group = "(ungrouped)"
		}
		if group != currentGroup {
			fmt.Printf("\n[%s]\n", group)
			currentGroup = group
		}
		fmt.Printf("  [%d/%d] %s\n", i+1, len(materials), material.Title)

		session, err := client.OpenMaterialSession(material.PageURL)
		if err != nil {
			result.Failed++
			return stop(fmt.Errorf("open material %q: %w; refusing to open another material because close state is unknown", material.Title, err))
		}
		if session.Started {
			started++
		}

		switch session.State {
		case "limited":
			fmt.Fprintln(os.Stderr, "    - skip (execution limit)")
		case "interactive":
			fmt.Fprintln(os.Stderr, "    - skip (requires input)")
		case "no-files":
			fmt.Fprintln(os.Stderr, "    - no downloadable file")
		default:
			downloadable += len(session.Links)
			for _, link := range session.Links {
				resource := material
				resource.DownloadURL = link
				status, err := pullOne(client, root, manifest, resource, group, options)
				if err != nil {
					fmt.Fprintf(os.Stderr, "    ! download: %v\n", err)
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
		}

		if session.Started {
			if session.Close == nil {
				result.Failed++
				return stop(fmt.Errorf("material %q was started but no recognized close action was found; refusing to open the next material", material.Title))
			}
			if err := client.CloseMaterial(session.Close); err != nil {
				result.Failed++
				return stop(fmt.Errorf("close material %q: %w; refusing to open the next material", material.Title, err))
			}
			closed++
			fmt.Println("    closed")
		}
	}

	if err := manifest.Save(root); err != nil {
		return result, err
	}
	fmt.Printf("\nProcessed %d/%d candidates; started %d, closed %d, downloadable files %d\n", len(materials), len(materials), started, closed, downloadable)
	return result, nil
}

func selectMaterials(materials []webclass.Resource, stats webclass.PullResourceStats, query string) ([]webclass.Resource, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return materials, nil
	}

	var exact []webclass.Resource
	for _, material := range materials {
		if strings.EqualFold(strings.TrimSpace(material.Title), query) || material.ID == query {
			exact = append(exact, material)
		}
	}
	if len(exact) == 1 {
		return exact, nil
	}
	if len(exact) > 1 {
		return nil, ambiguousMaterialError(query, exact, stats)
	}

	q := strings.ToLower(query)
	var partial []webclass.Resource
	for _, material := range materials {
		group := stats.ResourceGroups[material.ID]
		qualified := group + "/" + material.Title
		if strings.Contains(strings.ToLower(material.Title), q) ||
			strings.Contains(strings.ToLower(qualified), q) ||
			strings.Contains(strings.ToLower(material.ID), q) {
			partial = append(partial, material)
		}
	}
	if len(partial) == 1 {
		return partial, nil
	}
	if len(partial) == 0 {
		return nil, fmt.Errorf("material %q not found; no material was opened", query)
	}
	return nil, ambiguousMaterialError(query, partial, stats)
}

func ambiguousMaterialError(query string, matches []webclass.Resource, stats webclass.PullResourceStats) error {
	lines := make([]string, 0, len(matches))
	for _, material := range matches {
		group := stats.ResourceGroups[material.ID]
		if group == "" {
			group = "(ungrouped)"
		}
		lines = append(lines, fmt.Sprintf("  %s/%s [%s]", group, material.Title, material.ID))
	}
	sort.Strings(lines)
	return fmt.Errorf("material %q is ambiguous; no material was opened:\n%s", query, strings.Join(lines, "\n"))
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

func pullOne(client *webclass.Client, root string, manifest *state.Manifest, resource webclass.Resource, group string, options Options) (string, error) {
	dl, err := client.OpenDownload(resource.DownloadURL)
	if err != nil {
		return "", err
	}
	defer dl.Body.Close()

	filename, renamedOpaque := downloadFilename(resource, dl)
	rel := resourcePath(resource, group, filename, options)
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
	oldKey := key

	if !exists && renamedOpaque {
		if migratedKey, migratedEntry, ok := findOpaqueManifestEntry(manifest, resource, hash); ok {
			oldKey, old, exists = migratedKey, migratedEntry, true
		}
	}

	if exists && old.SHA256 == hash {
		_, statErr := os.Stat(dst)
		pathChanged := filepath.Clean(old.Path) != filepath.Clean(rel)
		missing := os.IsNotExist(statErr)
		if statErr != nil && !missing {
			return "", statErr
		}

		if pathChanged || missing || oldKey != key {
			if err := replaceFile(tmpName, dst); err != nil {
				return "", err
			}
			if pathChanged {
				removeOldPath(root, old.Path, rel)
			}
			old.CourseName = resource.CourseName
			old.ResourceTitle = resource.Title
			old.Filename = filename
			old.Path = rel
			old.Size = n
			if oldKey != key {
				delete(manifest.Entries, oldKey)
			}
			manifest.Entries[key] = old
			if renamedOpaque || oldKey != key {
				fmt.Printf("    = %s (renamed)\n", rel)
			} else if pathChanged {
				fmt.Printf("    = %s (relocated)\n", rel)
			} else {
				fmt.Printf("    = %s (restored)\n", rel)
			}
			return "unchanged", nil
		}

		os.Remove(tmpName)
		fmt.Printf("    = %s\n", rel)
		return "unchanged", nil
	}

	status := "new"
	prefix := "+"
	if exists {
		status, prefix = "changed", "!"
	}
	if err := replaceFile(tmpName, dst); err != nil {
		return "", err
	}
	if exists && filepath.Clean(old.Path) != filepath.Clean(rel) {
		removeOldPath(root, old.Path, rel)
	}
	if oldKey != key {
		delete(manifest.Entries, oldKey)
	}
	manifest.Entries[key] = state.Entry{
		CourseID: resource.CourseID, CourseName: resource.CourseName,
		ResourceID: resource.ID, ResourceTitle: resource.Title,
		Filename: filename, Path: rel, SHA256: hash, Size: n, UpdatedAt: time.Now(),
	}
	fmt.Printf("    %s %s\n", prefix, rel)
	return status, nil
}

func downloadFilename(resource webclass.Resource, dl *webclass.Download) (string, bool) {
	filename := sanitize(dl.Filename)
	if filename == "" || filename == "download" {
		filename = sanitize(resource.Title)
	}

	if opaquePDFFilename.MatchString(filename) &&
		!hasFileNameParameter(resource.DownloadURL) &&
		!hasFileNameParameter(dl.URL) {
		title := sanitize(resource.Title)
		if title != "" && title != "resource" {
			if !strings.HasSuffix(strings.ToLower(title), ".pdf") {
				title += ".pdf"
			}
			return title, true
		}
	}

	return filename, false
}

func hasFileNameParameter(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.TrimSpace(u.Query().Get("file_name")) != ""
}

func findOpaqueManifestEntry(manifest *state.Manifest, resource webclass.Resource, hash string) (string, state.Entry, bool) {
	for key, entry := range manifest.Entries {
		if entry.CourseID != resource.CourseID || entry.ResourceID != resource.ID || entry.SHA256 != hash {
			continue
		}
		if opaquePDFFilename.MatchString(entry.Filename) {
			return key, entry, true
		}
	}
	return "", state.Entry{}, false
}

func resourcePath(resource webclass.Resource, group, filename string, options Options) string {
	courseDir := sanitize(resource.CourseName)
	resourceDir := sanitize(resource.Title)
	if courseDir == "" {
		courseDir = resource.CourseID
	}
	if resourceDir == "" {
		resourceDir = "resource"
	}

	parts := []string{courseDir}
	if options.GroupDirs {
		groupDir := sanitize(group)
		if groupDir != "" && group != "(ungrouped)" {
			parts = append(parts, groupDir)
		}
	}
	parts = append(parts, resourceDir, filename)
	return filepath.Join(parts...)
}

func replaceFile(tmpName, dst string) error {
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmpName, dst)
}

func removeOldPath(root, oldRel, newRel string) {
	if oldRel == "" || filepath.Clean(oldRel) == filepath.Clean(newRel) {
		return
	}
	oldPath, ok := managedPath(root, oldRel)
	if !ok {
		return
	}
	if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
		return
	}
	pruneEmptyParents(root, filepath.Dir(oldPath))
}

func managedPath(root, rel string) (string, bool) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	targetAbs, err := filepath.Abs(filepath.Join(root, rel))
	if err != nil {
		return "", false
	}
	inside, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) || filepath.IsAbs(inside) {
		return "", false
	}
	return targetAbs, true
}

func pruneEmptyParents(root, start string) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return
	}
	current, err := filepath.Abs(start)
	if err != nil {
		return
	}

	for {
		rel, err := filepath.Rel(rootAbs, current)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
			return
		}
		if err := os.Remove(current); err != nil {
			return
		}
		parent := filepath.Dir(current)
		if parent == current {
			return
		}
		current = parent
	}
}

func sanitize(name string) string {
	name = strings.TrimSpace(invalidFilename.ReplaceAllString(name, "_"))
	name = strings.Trim(name, ". ")
	if len(name) > 180 {
		name = name[:180]
	}
	return name
}
