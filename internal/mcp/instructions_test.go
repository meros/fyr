package mcp

import (
	"strings"
	"testing"
)

// V0.prompt-fits-setup: the initialize answer carries the key rules, so a
// subagent, which never sees the appended system prompt, has them too.
func TestInstructionsCarryTheKeyRules(t *testing.T) {
	_, out := rpc(t, newServer(), "tok", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"})
	got, _ := out["result"].(map[string]any)["instructions"].(string)
	if got != Instructions {
		t.Fatalf("instructions %q", got)
	}
	for _, want := range []string{"subagents too", "return at once", "[Fyr]", `change_request {change, ask: "merge"}`, "Never run sudo", "root: true", "goes on a card with ask", "instead of polling gh"} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q", want)
		}
	}
}
