package agent

import (
	"strings"
	"testing"
)

// V0.settle-request: the prompt steers a finished agent to settle_request.
func TestFyrPromptNamesSettleRequest(t *testing.T) {
	if !strings.Contains(FyrPrompt, "mcp__fyr__settle_request {why}") {
		t.Fatal("FyrPrompt lacks settle_request")
	}
}
