package core

import (
	"strings"
	"testing"
)

// SPEC V0.merge-github-trouble (3): a merge GitHub's own trouble stopped tells
// the agent to wait and ask again, not to fix the PR, whatever GitHub's words
// say beside it.
func TestMergeGitHubTroubleNextIsWait(t *testing.T) {
	text := "not merged: " + GitHubFailed + ": GitHub answered with its own error on 4 tries, and the PR is still open (GraphQL: Something went wrong while executing your query; a rule)"
	next := refusedNext(&PR{Base: "main"}, text)
	if !strings.Contains(next, "wait a few minutes, and ask again") || strings.Contains(next, "fix the cause") {
		t.Fatalf("next step %q", next)
	}
}
