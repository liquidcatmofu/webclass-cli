package main

import "testing"

func TestCompletionCandidatesForPullMode(t *testing.T) {
	got := completionCandidates([]string{"pull", "--mode", ""})
	if len(got) != 3 || got[0] != "new" || got[1] != "files" || got[2] != "full" {
		t.Fatalf("mode completion = %#v", got)
	}

	got = completionCandidates([]string{"pull", "--mode=f"})
	if len(got) != 2 || got[0] != "--mode=files" || got[1] != "--mode=full" {
		t.Fatalf("mode= completion = %#v", got)
	}
}
