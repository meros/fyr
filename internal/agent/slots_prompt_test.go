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

// #174: the prompt names the heavy commands, says a subagent's run counts, says to release when the job ends, what to
// do while queued, and that Fyr refuses a heavy command without a slot.
func TestH174PromptStatesTheSlotRuleConcretely(t *testing.T) {
	for _, w := range []string{"make test", "go test ./...", "yarn screenshot:diff", "npm run test:layout", "subagent", "as soon as the job ends", "When you are queued, do not start it", "Fyr refuses a heavy command that is not admitted", "go test ./pkg -run X"} {
		if !strings.Contains(FyrPrompt, w) {
			t.Errorf("the prompt lacks %q", w)
		}
	}
}

// V0.163: the prompt speaks a share of the host, not slots (the tool names stay).
func TestV163PromptSpeaksPercent(t *testing.T) {
	for _, w := range []string{"share of this host", "%", "mcp__fyr__slot_acquire {kind, command}", "mcp__fyr__slot_release"} {
		if !strings.Contains(FyrPrompt, w) {
			t.Errorf("the prompt lacks %q", w)
		}
	}
	if strings.Contains(FyrPrompt, "take a slot") || strings.Contains(FyrPrompt, "the slot is yours") {
		t.Error("the prompt still speaks slots")
	}
}
