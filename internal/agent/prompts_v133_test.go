package agent

import (
	"strings"
	"testing"
)

// SPEC V0.133: the agent is told that the CLI's reject text in a turn no
// message started is not the owner, and never to say "you asked me to stop".
func TestFyrPromptSaysTheRejectIsNotTheOwner(t *testing.T) {
	for _, want := range []string{RejectText, "is not the user", "Say which call was blocked", "never tell the user they asked you to stop"} {
		if !strings.Contains(FyrPrompt, want) {
			t.Errorf("the Fyr prompt lacks %q", want)
		}
	}
}
