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
	m.SetOnBash(func(th, c string) string { got = append(got, [2]string{th, c}); return "" })
	s := &session{thread: "t_a"}
	m.hook(s, "gate", claude.Msg{"tool_name": "Bash", "tool_input": map[string]any{"command": "make test"}})
	m.hook(s, "gate", claude.Msg{"tool_name": "Bash", "agent_id": "sub1", "tool_input": map[string]any{"command": "yarn screenshot:diff"}})
	m.hook(s, "gate", claude.Msg{"tool_name": "Bash", "tool_input": map[string]any{"command": "gh pr merge 1 --squash"}})
	if len(got) != 2 || got[0] != [2]string{"t_a", "make test"} || got[1] != [2]string{"t_a", "yarn screenshot:diff"} {
		t.Fatalf("%v", got)
	}
}

// #174: a reason from OnBash refuses the command with that reason, for the agent to read.
func TestH174GateHookRefusesWithTheReason(t *testing.T) {
	m := New(&stubHost{}, Config{})
	m.SetOnBash(func(th, c string) string {
		if c == "make test" {
			return "Heavy job (make test) without a slot: call mcp__fyr__slot_acquire first"
		}
		return ""
	})
	s := &session{thread: "t_a"}
	out := m.hook(s, "gate", claude.Msg{"tool_name": "Bash", "tool_input": map[string]any{"command": "make test"}})
	h, _ := out["hookSpecificOutput"].(claude.Msg)
	if h["permissionDecision"] != "deny" || h["permissionDecisionReason"] != "Heavy job (make test) without a slot: call mcp__fyr__slot_acquire first" {
		t.Fatalf("%v", out)
	}
	if out := m.hook(s, "gate", claude.Msg{"tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}}); out != nil {
		t.Fatalf("a quick command: %v", out)
	}
}

// V0.164 (N7): a subagent's Bash call carries the undeclared-work note as additionalContext, from the host's func,
// which gives it once per episode; the main agent's own calls do not ask for it.
func TestN7SubagentBashCarriesTheUndeclaredNote(t *testing.T) {
	m := New(&stubHost{}, Config{})
	asked := 0
	m.SetUndeclaredNote(func(th string) string {
		asked++
		if asked == 1 {
			return "This thread runs heavy work Fyr did not admit"
		}
		return ""
	})
	s := &session{thread: "t_a"}
	if out := m.hook(s, "gate", claude.Msg{"tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}}); out != nil || asked != 0 {
		t.Fatalf("the main agent asked: %v %d", out, asked)
	}
	out := m.hook(s, "gate", claude.Msg{"tool_name": "Bash", "agent_id": "sub1", "tool_input": map[string]any{"command": "ls"}})
	h, _ := out["hookSpecificOutput"].(claude.Msg)
	if h["additionalContext"] != "This thread runs heavy work Fyr did not admit" || h["permissionDecision"] != nil {
		t.Fatalf("%v", out)
	}
	if out := m.hook(s, "gate", claude.Msg{"tool_name": "Bash", "agent_id": "sub1", "tool_input": map[string]any{"command": "ls"}}); out != nil {
		t.Fatalf("twice: %v", out)
	}
}
