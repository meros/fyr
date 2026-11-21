package agent

import (
	"strings"
	"testing"
)

// V0.prompt-open-pr: on a GitHub setup the prompt tells the agent to push,
// open the PR, and request merge when a code change is ready, rather than
// stopping at a local commit; non-GitHub repos keep promptNoGitHub.
func TestPromptOpenPRWhenReady(t *testing.T) {
	want := []string{
		"push the branch, open a pull request with gh pr create",
		`mcp__fyr__change_request {change, ask: "merge"}`,
		"do not stop at a local commit",
	}
	for _, w := range want {
		if !strings.Contains(FyrPrompt, w) {
			t.Errorf("FullSetup prompt lacks %q", w)
		}
	}
	noGH := Prompt(Setup{Git: true, OS: "linux", Pkexec: true})
	if strings.Contains(noGH, "open a pull request with gh pr create") {
		t.Error("non-GitHub git setup should not tell the agent to open a PR")
	}
	if !strings.Contains(noGH, promptNoGitHub) {
		t.Error("non-GitHub git setup lacks promptNoGitHub")
	}
}
