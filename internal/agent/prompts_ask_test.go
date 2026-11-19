package agent

import (
	"strings"
	"testing"
)

// V0.ask-tool: a question goes on a card, not in the message's text.
func TestFyrPromptSaysAskOnACard(t *testing.T) {
	for _, want := range []string{"mcp__fyr__ask", "Never leave a question or a choice for the user in your message text", "never for permission to go on"} {
		if !strings.Contains(FyrPrompt, want) {
			t.Errorf("the Fyr prompt lacks %q", want)
		}
	}
}
