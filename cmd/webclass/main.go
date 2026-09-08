package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/liquidcatmofu/webclass-cli/internal/auth"
	"github.com/liquidcatmofu/webclass-cli/internal/cache"
	"github.com/liquidcatmofu/webclass-cli/internal/pull"
	"github.com/liquidcatmofu/webclass-cli/internal/webclass"
)

const defaultBaseURL = "https://webclass.kosen-k.go.jp/webclass/"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "auth":
		fs := flag.NewFlagSet("auth", flag.ContinueOnError)
		base := fs.String("base-url", defaultBaseURL, "WebClass base URL")
		browser := fs.String("browser", "", "installed Chromium-based browser executable (auto-detected if omitted)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		u, err := url.Parse(*base)
		if err != nil {
			return err
		}
		if err := auth.BrowserLogin(context.Background(), u, *browser); err != nil {
			return err
		}
		client, err := webclass.New(*base)
		if err != nil {
			return err
		}
		if err := client.CheckSession(); err != nil {
			return fmt.Errorf("saved cookies did not produce a valid WebClass session: %w", err)
		}
		fmt.Println("Authentication succeeded.")
		return nil

	case "courses":
		fs := flag.NewFlagSet("courses", flag.ContinueOnError)
		base := fs.String("base-url", defaultBaseURL, "WebClass base URL")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		client, err := webclass.New(*base)
		if err != nil {
			return err
		}
		courses, err := client.Courses()
		if err != nil {
			return err
		}
		if err := cache.SaveCourses(courses); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not update course completion cache: %v\n", err)
		}
		for _, c := range courses {
			fmt.Printf("%s\t%s\n", c.ID, c.Name)
		}
		return nil

	case "assignments":
		fs := flag.NewFlagSet("assignments", flag.ContinueOnError)
		base := fs.String("base-url", defaultBaseURL, "WebClass base URL")
		interval := fs.Duration("interval", time.Second, "minimum quiet interval between WebClass HTTP requests")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() > 1 {
			return errors.New("assignments accepts at most one course ID")
		}
		if *interval < 0 {
			return errors.New("assignments --interval must not be negative")
		}

		client, err := webclass.New(*base)
		if err != nil {
			return err
		}
		client.SetRequestInterval(*interval)
		fmt.Printf("Request interval: %s; assignment contents are not opened\n", interval.String())

		courses, err := client.Courses()
		if err != nil {
			return err
		}
		if err := cache.SaveCourses(courses); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not update course completion cache: %v\n", err)
		}
		if fs.NArg() == 1 {
			courseID := fs.Arg(0)
			var selected []webclass.Course
			for _, course := range courses {
				if course.ID == courseID {
					selected = append(selected, course)
					break
				}
			}
			if len(selected) == 0 {
				return fmt.Errorf("course %q not found; run `webclass courses` to list course IDs", courseID)
			}
			courses = selected
		}

		failures := 0
		for i, course := range courses {
			assignments, err := client.Assignments(course)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: %s (%s): %v\n", course.Name, course.ID, err)
				failures++
				continue
			}
			if i > 0 {
				fmt.Println()
			}
			printAssignments(course, assignments)
		}
		if failures > 0 {
			return fmt.Errorf("failed to read assignments for %d course(s)", failures)
		}
		return nil

	case "pull":
		fs := flag.NewFlagSet("pull", flag.ContinueOnError)
		base := fs.String("base-url", defaultBaseURL, "WebClass base URL")
		dir := fs.String("dir", "webclass", "download directory")
		groupDirs := fs.Bool("group-dirs", false, "insert WebClass group folders between course and material directories")
		interval := fs.Duration("interval", time.Second, "minimum quiet interval between WebClass HTTP requests")
		material := fs.String("material", "", "pull only one material by exact title, unique substring, or contents ID")
		mode := fs.String("mode", pull.ModeNew, "pull mode: new (unknown materials only), files (new files only), full (download/hash all files)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New("pull requires exactly one course ID; run `webclass courses` to list course IDs")
		}
		if *interval < 0 {
			return errors.New("pull --interval must not be negative")
		}
		modeExplicit := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "mode" {
				modeExplicit = true
			}
		})
		if !modeExplicit && strings.TrimSpace(*material) != "" {
			// An explicitly selected material has traditionally been opened for an
			// update. Keep that useful behavior while the global default becomes
			// the lightweight new-material check.
			*mode = pull.ModeFiles
		}
		*mode = strings.ToLower(strings.TrimSpace(*mode))
		if !pull.ValidMode(*mode) {
			return fmt.Errorf("invalid pull mode %q; use new, files, or full", *mode)
		}
		courseID := fs.Arg(0)

		client, err := webclass.New(*base)
		if err != nil {
			return err
		}
		client.SetRequestInterval(*interval)
		fmt.Printf("Request interval: %s; concurrent requests: disabled\n", interval.String())

		courses, err := client.Courses()
		if err != nil {
			return err
		}
		if err := cache.SaveCourses(courses); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not update course completion cache: %v\n", err)
		}
		var selected *webclass.Course
		for i := range courses {
			if courses[i].ID == courseID {
				selected = &courses[i]
				break
			}
		}
		if selected == nil {
			return fmt.Errorf("course %q not found; run `webclass courses` to list course IDs", courseID)
		}

		result, err := pull.Run(client, *dir, *selected, pull.Options{GroupDirs: *groupDirs, Material: *material, Mode: *mode})
		if err != nil {
			return err
		}
		fmt.Printf("\n%d new, %d changed, %d unchanged, %d skipped, %d failed\n", result.New, result.Changed, result.Unchanged, result.Skipped, result.Failed)
		if result.Failed > 0 {
			return errors.New("some resources failed to download")
		}
		return nil

	case "completion":
		if len(args) != 2 {
			return errors.New("completion requires one shell: bash, zsh, fish, or powershell")
		}
		script, err := completionScript(args[1])
		if err != nil {
			return err
		}
		fmt.Print(script)
		return nil

	case "__complete":
		for _, candidate := range completionCandidates(args[1:]) {
			fmt.Println(candidate)
		}
		return nil

	case "help", "-h", "--help":
		return usage()
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usageText())
	}
}

func printAssignments(course webclass.Course, assignments []webclass.Assignment) {
	fmt.Printf("[%s] %s\n", course.ID, course.Name)
	if len(assignments) == 0 {
		fmt.Println("  課題なし")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "期限\t状態\t種別\t課題")
	for _, assignment := range assignments {
		deadline := "-"
		if assignment.HasDeadline {
			deadline = assignment.DeadlineText
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", deadline, assignmentStatusLabel(assignment.SubmissionStatus), assignment.Category, assignment.Title)
	}
	_ = w.Flush()
}

func assignmentStatusLabel(status webclass.SubmissionStatus) string {
	switch status {
	case webclass.SubmissionSubmitted:
		return "提出済"
	case webclass.SubmissionPending:
		return "未提出"
	case webclass.SubmissionResubmit:
		return "再提出"
	default:
		return "不明"
	}
}

func usage() error {
	fmt.Fprint(os.Stderr, usageText())
	return nil
}

func usageText() string {
	return `webclass - CLI client for WebClass

Usage:
  webclass auth [--base-url URL] [--browser PATH]
  webclass courses [--base-url URL]
  webclass assignments [--base-url URL] [--interval DURATION] [course-id]
  webclass pull [--base-url URL] [--dir DIR] [--group-dirs] [--interval DURATION] [--material NAME] [--mode new|files|full] <course-id>
  webclass completion bash|zsh|fish|powershell

Assignments:
  Lists non-material entries from course pages and checks submission state using the score sheet.
  Assignment contents are never opened. Omit course-id to scan all courses.

Pull modes:
  new    Open only materials that have not been seen before. This is the default.
  files  Open all selected materials, but download only files not already in the local manifest.
  full   Download every discovered file and compare SHA-256 hashes, including replacements.

When --material is specified without --mode, files mode is used so an explicitly selected material is actually checked.
Works with WebClass instances that expose compatible WebClass 12.x HTML flows.
The current fallback base URL is https://webclass.kosen-k.go.jp/webclass/ for backward compatibility; use --base-url for another instance.
Pull and assignments default to a 1s quiet interval between WebClass HTTP requests.
Shell completion never accesses WebClass; course IDs come from the last courses/pull/assignments cache and material names come from the local manifest.
`
}
