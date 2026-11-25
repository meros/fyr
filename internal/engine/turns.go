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
	// Files are the ids and names of the files the owner attached.
	Files []file
	// Images is how many pasted images the message carried.
	Images int
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

// file is an attachment of a user message.
type file struct{ ID, Name, MediaType string }

// part is a text block or a tool call of an agent turn.
type part struct {
	Text  string // a text block; "" for a tool call
	Tool  string // a tool call's name
	Input any
}

// readTurns reads the conversation from events. Subagent messages and other
// events are left out.
//
// V0.midrun-answer: a user message with an id takes its place where the agent
// read it (its message.state read, cancelled or lost event), as the web
// conversation places it (V0.33), so thread_read, a handover and the web show
// one order. A message whose state never arrives stays where it was sent.
func readTurns(evs []store.Event) []turn {
	var out []turn
	held := map[string]turn{}
	heldAt := map[string]int{} // the index in out where each held message was sent
	var sent []string          // held ids in the order they were sent
	for _, ev := range evs {
		t := turn{At: ev.At}
		switch ev.Type {
		case "message.state":
			var d struct{ ID, State string }
			json.Unmarshal(ev.Data, &d)
			h, ok := held[d.ID]
			if !ok || (d.State != "read" && d.State != "cancelled" && d.State != "lost") {
				continue
			}
			delete(held, d.ID)
			delete(heldAt, d.ID)
			out = append(out, h)
			continue
		case "user":
			var d struct {
				ID          string `json:"id"`
				Text, From  string
				Images      int
				Attachments []struct {
					ID, Name, MediaType string
				}
			}
			json.Unmarshal(ev.Data, &d)
			t.Kind, t.Text, t.From, t.Images = turnUser, d.Text, d.From, d.Images
			for _, a := range d.Attachments {
				t.Files = append(t.Files, file{a.ID, a.Name, a.MediaType})
			}
			if d.ID != "" {
				held[d.ID] = t
				heldAt[d.ID] = len(out)
				sent = append(sent, d.ID)
				continue
			}
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
	if len(held) == 0 {
		return out
	}
	// Messages never read go back where they were sent. Inserted the latest
	// sent first, so the earlier places stay right and two sent at one place
	// keep their order.
	for k := len(sent) - 1; k >= 0; k-- {
		id := sent[k]
		if _, ok := held[id]; !ok {
			continue
		}
		i := heldAt[id]
		out = append(out[:i], append([]turn{held[id]}, out[i:]...)...)
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
