package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/store"
)

// turnEvents builds a log: each step is a type and its data.
func turnEvents(steps ...any) []store.Event {
	var evs []store.Event
	for i := 0; i < len(steps); i += 2 {
		b, _ := json.Marshal(steps[i+1])
		evs = append(evs, store.Event{Seq: int64(len(evs) + 1), At: time.Unix(int64(1000+len(evs)), 0), Type: steps[i].(string), Data: b})
	}
	return evs
}

func agentText(s string) map[string]any {
	return map[string]any{"type": "assistant", "parent_tool_use_id": nil, "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": s}}}}
}

// order names the turns: "U:<text>" for a user turn, "A:<text>" for an agent text, "end" for a run's end.
func order(ts []turn) string {
	var out []string
	for _, t := range ts {
		switch t.Kind {
		case turnUser:
			out = append(out, "U:"+t.Text)
		case turnAgent:
			for _, p := range t.Parts {
				out = append(out, "A:"+p.Text)
			}
		case turnEnded:
			out = append(out, "end")
		}
	}
	return strings.Join(out, " ")
}

// V0.midrun-answer: a message with an id is placed where the agent read it, as the web places it.
func TestReadTurnsPlacesMessageWhereRead(t *testing.T) {
	evs := turnEvents(
		"user", map[string]any{"text": "go"},
		"msg", agentText("first answer"),
		"user", map[string]any{"text": "also this", "id": "m1"},
		"msg", agentText("still on the first"),
		"message.state", map[string]any{"id": "m1", "state": "queued"},
		"message.state", map[string]any{"id": "m1", "state": "read"},
		"msg", agentText("on it"),
		"run.ended", map[string]any{},
	)
	if got, want := order(readTurns(evs)), "U:go A:first answer A:still on the first U:also this A:on it end"; got != want {
		t.Fatalf("order\n got %s\nwant %s", got, want)
	}
}

func TestReadTurnsCancelledAndLostTakeTheirPlaceToo(t *testing.T) {
	for _, st := range []string{"cancelled", "lost"} {
		evs := turnEvents(
			"user", map[string]any{"text": "x", "id": "m1"},
			"msg", agentText("a"),
			"message.state", map[string]any{"id": "m1", "state": st},
			"msg", agentText("b"),
		)
		if got, want := order(readTurns(evs)), "A:a U:x A:b"; got != want {
			t.Fatalf("%s: got %s, want %s", st, got, want)
		}
	}
}

// A message whose state never arrives stays where it was sent; two sent at one place keep their order.
func TestReadTurnsUnreadMessageStaysWhereSent(t *testing.T) {
	evs := turnEvents(
		"msg", agentText("a"),
		"user", map[string]any{"text": "one", "id": "m1"},
		"user", map[string]any{"text": "two", "id": "m2"},
		"msg", agentText("b"),
		"user", map[string]any{"text": "three", "id": "m3"},
		"message.state", map[string]any{"id": "m3", "state": "read"},
		"msg", agentText("c"),
	)
	if got, want := order(readTurns(evs)), "A:a U:one U:two A:b U:three A:c"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	// one event alone (messages() reads them one by one) still yields its turn
	if n := messages(evs[1]); n != 1 {
		t.Fatalf("messages of one held user event = %d, want 1", n)
	}
}
