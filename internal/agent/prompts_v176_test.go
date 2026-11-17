package agent

import (
	"strings"
	"testing"
)

// V0.176 (6): the prompt asks the agent to clean up, prefer timeouts and
// declare a process that must stay.
func TestV176PromptAsksToCleanUpAndDeclare(t *testing.T) {
	for _, want := range []string{"Clean up what you start", "prefer a timeout", "30 min after your run ends", "when the thread settles", "mcp__fyr__keep_process"} {
		if !strings.Contains(FyrPrompt, want) {
			t.Errorf("FyrPrompt lacks %q", want)
		}
	}
}
