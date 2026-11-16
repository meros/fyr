package agent

import (
	"testing"

	"github.com/meros/fyr/internal/cli"
)

// SPEC 0.5 V0.79 (1): the composer's list leaves out the CLI's internal
// commands (a "__" name) and the ones that drive its terminal screen or a
// setting Fyr has its own control for. Names from a live list, 16 Nov.
func TestCommandsLeaveOutInternalAndTerminalOnes(t *testing.T) {
	var raw []any
	for _, n := range []string{
		"morning-coordinator", "dev:review", "compact", "init", "security-review",
		"__remote-workflow", "workflow-launch-exec", "heapdump", "reload-plugins", "reload-skills",
		"color", "focus", "autocompact", "auto-mode-setup", "effort", "rename", "clear",
	} {
		raw = append(raw, cli.Msg{"name": n, "description": n})
	}
	got := map[string]bool{}
	for _, c := range commandsOf(cli.Msg{"response": cli.Msg{"commands": raw}}) {
		got[c.Name] = true
	}
	for _, keep := range []string{"morning-coordinator", "dev:review", "compact", "init", "security-review"} {
		if !got[keep] {
			t.Errorf("%s is missing", keep)
		}
	}
	for _, drop := range []string{"__remote-workflow", "workflow-launch-exec", "heapdump", "reload-plugins", "reload-skills", "color", "focus", "autocompact", "auto-mode-setup", "effort", "rename", "clear"} {
		if got[drop] {
			t.Errorf("%s is in the list", drop)
		}
	}
}
