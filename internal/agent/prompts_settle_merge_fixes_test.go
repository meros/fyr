package agent

import (
	"strings"
	"testing"
)

// V0.settle-merge-fixes (1): the prompt names the call that asks for a merge
// and says a word in text asks for nothing.
func TestPromptMergeNamesTheCall(t *testing.T) {
	if strings.Contains(FyrPrompt, `ask "merge" right after`) {
		t.Error(`the prompt still says to ask "merge", which agents wrote as text`)
	}
	for _, want := range []string{`mcp__fyr__change_request {change, ask: "merge"}`, `Writing "merge" in your message asks for nothing`} {
		if !strings.Contains(FyrPrompt, want) {
			t.Errorf("the prompt does not say %q", want)
		}
	}
}
