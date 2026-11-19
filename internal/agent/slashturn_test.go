package agent

import (
	"testing"

	"github.com/meros/fyr/internal/core"
)

// V0.slash-first-turn: a message that starts a turn with a slash command takes no notices: the
// CLI runs a command only when its text starts with "/", and notices in
// front of it made the command plain text. They stay for the run's hooks.
func TestSlashCommandTakesNoTurnNotices(t *testing.T) {
	for _, c := range []struct {
		text string
		take bool
	}{{"/fake-skill topic", false}, {"hello", true}, {"  /fake-skill", false}} {
		h := &feedHost{cwd: t.TempDir()}
		m := fakeManager(t, h)
		if err := m.Do("t1", core.Effect{Kind: core.EffectSend, Text: c.text}); err != nil {
			t.Fatal(err)
		}
		_, takes, _ := h.state()
		took := len(takes) > 0 && takes[0] == core.AtTurn
		if took != c.take {
			t.Fatalf("%q: took notices at the turn = %v, want %v (%v)", c.text, took, c.take, takes)
		}
		m.StopAll()
	}
}
