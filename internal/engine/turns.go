package engine

import (
	"encoding/json"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

// A turn is one entry of a thread's conversation, read from the log: what
// thread_read (V0.22) and a handover's conversation (V0.45) show. Both read
// the log through readTurns, so they know the same events.
type turn struct {
	Kind turnKind
	At   time.Time
	// turnUser: the words and who sent them (see speaker).
	From, Text string
	// turnAgent: the top-level assistant message, its parts in order.
	Parts []part
	// turnHandover.
	FromProfile, ToProfile, Rung string
	// turnEnded: the failure, "" for a run that ended well.
	Failure string
	// turnEffect: an effect's result.
	Action string
	OK     bool
}

type turnKind int

const (
	turnUser turnKind = iota
	turnAgent
	turnHandover
	turnEnded
	turnEffect
)

// part is a text block or a tool call of an agent turn.
type part struct {
	Text  string // a text block; "" for a tool call
	Tool  string // a tool call's name
	Input any
}

// readTurns reads the conversation from events. Subagent messages and other
// events are left out.
func readTurns(evs []store.Event) []turn {
	var out []turn
	for _, ev := range evs {
		t := turn{At: ev.At}
		switch ev.Type {
		case "user":
			var d struct{ Text, From string }
			json.Unmarshal(ev.Data, &d)
			t.Kind, t.Text, t.From = turnUser, d.Text, d.From
		case "msg":
			var d struct {
				Type   string `json:"type"`
				Parent any    `json:"parent_tool_use_id"`
				Msg    struct {
					Content []map[string]any `json:"content"`
				} `json:"message"`
			}
			json.Unmarshal(ev.Data, &d)
			if d.Type != "assistant" || d.Parent != nil {
				continue
			}
			t.Kind = turnAgent
			for _, c := range d.Msg.Content {
				switch c["type"] {
				case "text":
					s, _ := c["text"].(string)
					t.Parts = append(t.Parts, part{Text: s})
				case "tool_use":
					nm, _ := c["name"].(string)
					t.Parts = append(t.Parts, part{Tool: nm, Input: c["input"]})
				}
			}
		case "handover":
			var d struct{ From, To, Rung string }
			json.Unmarshal(ev.Data, &d)
			t.Kind, t.FromProfile, t.ToProfile, t.Rung = turnHandover, d.From, d.To, d.Rung
		case "run.ended":
			var d struct {
				Failure *struct{ Message string } `json:"failure"`
			}
			json.Unmarshal(ev.Data, &d)
			t.Kind = turnEnded
			if d.Failure != nil {
				t.Failure = d.Failure.Message
			}
		case "effect.result":
			var d struct {
				Action string
				OK     bool
				Text   string
			}
			json.Unmarshal(ev.Data, &d)
			t.Kind, t.Action, t.OK, t.Text = turnEffect, d.Action, d.OK, d.Text
		default:
			continue
		}
		out = append(out, t)
	}
	return out
}

// speaker names who wrote a user turn; person is the word for the owner
// ("User" for an agent reading, "Owner" in a handover).
func speaker(person, from string) string {
	switch from {
	case "", core.OldViewTyped:
		return person
	case agent.FromView:
		return person + " (live view)"
	case agent.FromHandover:
		return "Fyr"
	}
	return "Thread " + from
}
