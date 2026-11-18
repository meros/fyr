package agent

import (
	"strings"
	"testing"
)

// SPEC V0.143 (5): the agent is told not to hold a background Bash for
// hours and how to run a long-lived process instead.
func TestFyrPromptSaysNoHoursLongBackgroundBash(t *testing.T) {
	for _, want := range []string{"Keep no background Bash for hours", "at most 2 h", "setsid nohup timeout", "pid file", "Monitor"} {
		if !strings.Contains(FyrPrompt, want) {
			t.Errorf("the Fyr prompt lacks %q", want)
		}
	}
}

// V0.195: how to wait for background work.
func TestFyrPromptSaysHowToWait(t *testing.T) {
	for _, want := range []string{"run the job itself with Bash run_in_background", "Never detach a job and then poll", "loop under timeout", "exits when it cannot tell"} {
		if !strings.Contains(FyrPrompt, want) {
			t.Errorf("the Fyr prompt lacks %q", want)
		}
	}
}
