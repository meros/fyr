package agent

import (
	"testing"

	"github.com/meros/fyr/internal/claude"
)

// Decision 0029 S1 (#185): the gate hook passes each Bash command of a thread (its subagents' too) to OnBash, so
// a slot hold is named by the heavy command it runs; a denied merge is not passed on.
func TestS1GateHookHearsBashCommands(t *testing.T) {
	var got [][2]string
	m := New(&stubHost{}, Config{})
	m.SetOnBash(func(c BashCall) string { got = append(got, [2]string{c.Thread, c.Command}); return "" }, nil)
	s := &session{thread: "t_a"}
	m.hook(s, "gate", claude.Msg{"tool_name": "Bash", "tool_input": map[string]any{"command": "make test"}})
	m.hook(s, "gate", claude.Msg{"tool_name": "Bash", "agent_id": "sub1", "tool_input": map[string]any{"command": "yarn screenshot:diff"}})
	m.hook(s, "gate", claude.Msg{"tool_name": "Bash", "tool_input": map[string]any{"command": "gh pr merge 1 --squash"}})
	if len(got) != 2 || got[0] != [2]string{"t_a", "make test"} || got[1] != [2]string{"t_a", "yarn screenshot:diff"} {
		t.Fatalf("%v", got)
	}
}

// V0.209: a reason from OnBash (a full host) refuses the command with that reason, for the agent to read.
func TestH174GateHookRefusesWithTheReason(t *testing.T) {
	m := New(&stubHost{}, Config{})
	m.SetOnBash(func(c BashCall) string {
		if c.Command == "make test" {
			return "Heavy job (make test): no room yet"
		}
		return ""
	}, nil)
	s := &session{thread: "t_a"}
	out := m.hook(s, "gate", claude.Msg{"tool_name": "Bash", "tool_input": map[string]any{"command": "make test"}})
	h, _ := out["hookSpecificOutput"].(claude.Msg)
	if h["permissionDecision"] != "deny" || h["permissionDecisionReason"] != "Heavy job (make test): no room yet" {
		t.Fatalf("%v", out)
	}
	if out := m.hook(s, "gate", claude.Msg{"tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}}); out != nil {
		t.Fatalf("a quick command: %v", out)
	}
}

// V0.209: the gate passes the call's id and whether it runs in the background, and the end of the call is heard from
// the PostToolUse hook and from the stream's tool_result, a failed one's too.
func TestV209TheCallsStartAndEndAreHeard(t *testing.T) {
	var got BashCall
	var done []string
	m := New(&stubHost{}, Config{})
	m.SetOnBash(func(c BashCall) string { got = c; return "" }, func(th, id string) { done = append(done, th+" "+id) })
	s := &session{thread: "t_a"}
	m.hook(s, "gate", claude.Msg{"tool_name": "Bash", "tool_use_id": "tu1", "tool_input": map[string]any{"command": "make test", "run_in_background": true}})
	if got != (BashCall{Thread: "t_a", Command: "make test", ToolUse: "tu1", Background: true}) {
		t.Fatalf("%+v", got)
	}
	m.toolResults(s, claude.Msg{"message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "tu2", "is_error": true}, map[string]any{"type": "text"}}}})
	if len(done) != 1 || done[0] != "t_a tu2" {
		t.Fatalf("%v", done)
	}
}
