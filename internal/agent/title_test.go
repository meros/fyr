package agent

import (
	"strings"
	"testing"
)

// V0.title-goal: the title call is asked for the goal of the work, like an
// issue title, and told not to name the step the agent is at. V0.auto-title:
// short, in the owner's language, never generic, and kept unless the goal
// moved.
func TestTitlePromptAsksForTheGoal(t *testing.T) {
	for _, want := range []string{"like an issue title", "newest goal", "Do not name the step the agent is at",
		"at most 50 characters", "language of the owner's own words", "[attached image]", "never write a generic title",
		"keep it word for word", "titles of the thread's changes"} {
		if !strings.Contains(titleSystem, want) {
			t.Errorf("the title prompt lacks %q", want)
		}
	}
	if strings.Contains(titleSystem, "works on now") {
		t.Error("the title prompt still asks for what the thread works on now")
	}
}
