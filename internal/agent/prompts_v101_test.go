package agent

import (
	"strings"
	"testing"
)

// SPEC V0.101 (4): the system prompt carries the changes paragraph of
// decision 0024 section 7 and no upkeep of the changes.
func TestPromptChangesParagraph(t *testing.T) {
	if !strings.Contains(FyrPrompt, ChangesPrompt) {
		t.Fatal("the changes paragraph is missing")
	}
	for _, old := range []string{"change_unchanged", "change_status", "change_revise", "change_attach", "pr_link", "Keep your open changes current"} {
		if strings.Contains(FyrPrompt, old) {
			t.Errorf("the prompt still names %q", old)
		}
	}
}
