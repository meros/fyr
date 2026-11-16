package agent

import (
	"strings"
	"testing"
)

// SPEC 0.5 V0.86 (5): every agent is told to take a slot before a heavy job.
func TestV086PromptTellsAboutSlots(t *testing.T) {
	for _, w := range []string{"mcp__fyr__slot_acquire", "mcp__fyr__slot_release", "run ends"} {
		if !strings.Contains(FyrPrompt, w) {
			t.Errorf("the prompt does not say %q", w)
		}
	}
}
