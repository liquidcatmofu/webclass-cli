package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/liquidcatmofu/webclass-cli/internal/auth"
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
		for _, c := range courses {
			fmt.Printf("%s\t%s\n", c.ID, c.Name)
		}
		return nil

	case "pull":
		fs := flag.NewFlagSet("pull", flag.ContinueOnError)
		base := fs.String("base-url", defaultBaseURL, "WebClass base URL")
		dir := fs.String("dir", "webclass", "download directory")
		groupDirs := fs.Bool("group-dirs", false, "insert WebClass group folders between course and material directories")
		interval := fs.Duration("interval", time.Second, "minimum quiet interval between WebClass HTTP requests")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New("pull requires exactly one course ID; run `webclass courses` to list course IDs")
		}
		if *interval < 0 {
			return errors.New("pull --interval must not be negative")
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

		result, err := pull.Run(client, *dir, *selected, pull.Options{GroupDirs: *groupDirs})
		if err != nil {
			return err
		}
		fmt.Printf("\n%d new, %d changed, %d unchanged, %d failed\n", result.New, result.Changed, result.Unchanged, result.Failed)
		if result.Failed > 0 {
			return errors.New("some resources failed to download")
		}
		return nil

	case "help", "-h", "--help":
		return usage()
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usageText())
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
  webclass pull [--base-url URL] [--dir DIR] [--group-dirs] [--interval DURATION] <course-id>

The default WebClass instance is https://webclass.kosen-k.go.jp/webclass/.
Pull defaults to a 1s quiet interval between WebClass HTTP requests and never sends concurrent requests.
`
}
