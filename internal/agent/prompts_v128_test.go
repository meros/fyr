package agent

import (
	"strings"
	"testing"
)

// SPEC V0.128 (supersedes V0.49): a message typed on the View tab is an
// ordinary message, so the view prompt says nothing about one.
func TestViewPromptHasNoViewTabChat(t *testing.T) {
	for _, old := range []string{"Typed on the live view tab", "View tab", "spoken"} {
		if strings.Contains(ViewPrompt, old) {
			t.Errorf("the view prompt still names %q", old)
		}
	}
}
