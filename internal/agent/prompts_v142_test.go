package agent

import (
	"strings"
	"testing"
)

// SPEC V0.142 (5): the agent is told not to hold a background Bash for
// hours and how to run a long-lived process instead.
func TestFyrPromptSaysNoHoursLongBackgroundBash(t *testing.T) {
	for _, want := range []string{"Keep no background Bash for hours", "at most 2 h", "setsid nohup timeout", "pid file", "Monitor"} {
		if !strings.Contains(FyrPrompt, want) {
			t.Errorf("the Fyr prompt lacks %q", want)
		}
	}
}
