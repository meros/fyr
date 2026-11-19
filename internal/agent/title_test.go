package agent

import (
	"strings"
	"testing"
)

// V0.title-goal: the title call is asked for the goal of the work, like an
// issue title, and told not to name the step the agent is at.
func TestTitlePromptAsksForTheGoal(t *testing.T) {
	for _, want := range []string{"title of an issue", "goal of the work", "newest goal", "Do not name the step the agent is at now"} {
		if !strings.Contains(titleSystem, want) {
			t.Errorf("the title prompt lacks %q", want)
		}
	}
	if strings.Contains(titleSystem, "works on now") {
		t.Error("the title prompt still asks for what the thread works on now")
	}
}
