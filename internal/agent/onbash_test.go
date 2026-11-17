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
	m.SetOnBash(func(th, c string) { got = append(got, [2]string{th, c}) })
	s := &session{thread: "t_a"}
	m.hook(s, "gate", claude.Msg{"tool_name": "Bash", "tool_input": map[string]any{"command": "make test"}})
	m.hook(s, "gate", claude.Msg{"tool_name": "Bash", "agent_id": "sub1", "tool_input": map[string]any{"command": "yarn screenshot:diff"}})
	m.hook(s, "gate", claude.Msg{"tool_name": "Bash", "tool_input": map[string]any{"command": "gh pr merge 1 --squash"}})
	if len(got) != 2 || got[0] != [2]string{"t_a", "make test"} || got[1] != [2]string{"t_a", "yarn screenshot:diff"} {
		t.Fatalf("%v", got)
	}
}
