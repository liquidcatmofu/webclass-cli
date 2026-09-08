package main

import (
	"testing"

	"github.com/liquidcatmofu/webclass-cli/internal/webclass"
)

func TestAssignmentStatusLabelUsesScoreQuality(t *testing.T) {
	tests := []struct {
		name string
		a    webclass.Assignment
		want string
	}{
		{"full", webclass.Assignment{SubmissionStatus: webclass.SubmissionSubmitted, HasScore: true, Score: 10, HasMaxScore: true, MaxScore: 10}, "満点"},
		{"partial", webclass.Assignment{SubmissionStatus: webclass.SubmissionSubmitted, HasScore: true, Score: 8, HasMaxScore: true, MaxScore: 10}, "部分点"},
		{"zero", webclass.Assignment{SubmissionStatus: webclass.SubmissionSubmitted, HasScore: true, Score: 0, HasMaxScore: true, MaxScore: 10}, "0点"},
		{"score-only", webclass.Assignment{SubmissionStatus: webclass.SubmissionSubmitted, HasScore: true, Score: 8}, "採点済"},
		{"submitted", webclass.Assignment{SubmissionStatus: webclass.SubmissionSubmitted}, "提出済"},
		{"pending", webclass.Assignment{SubmissionStatus: webclass.SubmissionPending, HasMaxScore: true, MaxScore: 10}, "未提出"},
		{"resubmit", webclass.Assignment{SubmissionStatus: webclass.SubmissionResubmit, HasScore: true, Score: 5, HasMaxScore: true, MaxScore: 10}, "再提出"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := assignmentStatusLabel(tt.a); got != tt.want {
				t.Fatalf("assignmentStatusLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAssignmentScoreLabel(t *testing.T) {
	if got := assignmentScoreLabel(webclass.Assignment{HasScore: true, Score: 7.5, HasMaxScore: true, MaxScore: 10}); got != "7.5/10" {
		t.Fatalf("score label = %q", got)
	}
	if got := assignmentScoreLabel(webclass.Assignment{ScoreText: "*[N]", SubmissionStatus: webclass.SubmissionSubmitted}); got != "*[N]" {
		t.Fatalf("raw score label = %q", got)
	}
}
