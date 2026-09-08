package main

import (
	"path/filepath"
	"testing"

	"github.com/liquidcatmofu/webclass-cli/internal/cache"
	"github.com/liquidcatmofu/webclass-cli/internal/state"
	"github.com/liquidcatmofu/webclass-cli/internal/webclass"
)

func TestCompletionCandidatesUseCourseCacheAndManifestWithoutNetwork(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()

	if err := cache.SaveCourses([]webclass.Course{
		{ID: "02_26036", Name: "言語解析演習"},
		{ID: "02_26046", Name: "システム工学"},
		{ID: "02_26098", Name: "人工知能"},
	}); err != nil {
		t.Fatal(err)
	}

	manifest := &state.Manifest{Version: 1, Entries: map[string]state.Entry{
		"a": {CourseID: "02_26036", CourseName: "言語解析演習", ResourceID: "one", ResourceTitle: "第1回 演習問題"},
		"b": {CourseID: "02_26036", CourseName: "言語解析演習", ResourceID: "two", ResourceTitle: "第2回 正規表現，NFAへの変換"},
	}}
	if err := manifest.Save(dir); err != nil {
		t.Fatal(err)
	}

	got := completionCandidates([]string{"pull", "--dir", dir, "02_"})
	if len(got) != 3 || got[0] != "02_26036" || got[1] != "02_26046" || got[2] != "02_26098" {
		t.Fatalf("course completion = %#v", got)
	}

	got = completionCandidates([]string{"assignments", "02_260"})
	if len(got) != 3 || got[0] != "02_26036" || got[1] != "02_26046" || got[2] != "02_26098" {
		t.Fatalf("assignments course completion = %#v", got)
	}

	got = completionCandidates([]string{"pull", "--dir", dir, "--material", "第"})
	if len(got) != 2 || got[0] != "第1回 演習問題" || got[1] != "第2回 正規表現，NFAへの変換" {
		t.Fatalf("material completion = %#v", got)
	}

	commands := completionCandidates([]string{"a"})
	if len(commands) != 2 || commands[0] != "auth" || commands[1] != "assignments" {
		t.Fatalf("command completion = %#v", commands)
	}

	if _, err := filepath.Abs(dir); err != nil {
		t.Fatal(err)
	}
}

func TestCompletionScriptsExistForSupportedShells(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		script, err := completionScript(shell)
		if err != nil {
			t.Fatalf("completionScript(%q): %v", shell, err)
		}
		if script == "" {
			t.Fatalf("completionScript(%q) returned empty script", shell)
		}
	}
}
