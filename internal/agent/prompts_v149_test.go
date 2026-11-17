package agent

import (
	"strings"
	"testing"
)

// SPEC V0.149 (7): never a command in prose for the owner to type; the Run
// card instead, one script, root instead of sudo.
func TestFyrPromptSaysUseTheRunCard(t *testing.T) {
	for _, want := range []string{"mcp__fyr__run_request", "Never ask the user to type or paste a command", "one script", "set -e", "root: true instead of sudo"} {
		if !strings.Contains(FyrPrompt, want) {
			t.Errorf("the Fyr prompt lacks %q", want)
		}
	}
}
