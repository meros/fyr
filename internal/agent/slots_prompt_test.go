package agent

import (
	"strings"
	"testing"
)

// V0.209: an agent has nothing to manage for a heavy job; it hears only what to do when the host has no room.
func TestV209PromptAsksNothingForAHeavyJob(t *testing.T) {
	for _, w := range []string{"need nothing from you", "subagent", "do not work around it", "run the command again"} {
		if !strings.Contains(FyrPrompt, w) {
			t.Errorf("the prompt lacks %q", w)
		}
	}
	for _, w := range []string{"slot_acquire", "slot_release", "take a slot"} {
		if strings.Contains(FyrPrompt, w) {
			t.Errorf("the prompt still says %q", w)
		}
	}
}
