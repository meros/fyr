package engine

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// drain returns the frames of one kind waiting on ch.
func drain(ch chan Frame, event string) []map[string]any {
	var out []map[string]any
	for {
		select {
		case f := <-ch:
			if f.Event == event {
				var m map[string]any
				json.Unmarshal(f.Data, &m)
				out = append(out, m)
			}
		default:
			return out
		}
	}
}

// SPEC 0.5 V0.46: project.update sets and clears quiet hours; the thread and
// the project carry quietUntil inside the window; the tick sends them when
// the window starts and ends, and that is never an attention change (no
// push, nothing queued for the end of the window).
func TestQuietHours(t *testing.T) {
	h := newHarness(t, time.Minute)
	c := h.e.Command(Cmd{CommandID: "p", Action: "project.create", Args: core.Args{"name": "Work", "folder": h.dir}})
	pid := c.Result.(map[string]any)["id"].(string)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	h.m.StopAll()

	if r := h.cmd("project.update", "", core.Args{"id": pid, "quiet": map[string]any{"from": "25:00", "to": "06:00"}}); r.OK || r.Status != 400 {
		t.Fatalf("bad time accepted: %+v", r)
	}
	if r := h.cmd("project.update", "", core.Args{"id": pid, "quiet": map[string]any{"from": "17:00", "to": "06:00", "days": []any{"mon", "fri"}}}); !r.OK {
		t.Fatalf("%+v", r)
	}
	p := projectOf(h.e, id)
	if len(p.Quiet) != 1 || p.Quiet[0].From != "17:00" || p.Quiet[0].To != "06:00" || len(p.Quiet[0].Days) != 2 {
		t.Fatalf("quiet not stored: %+v", p.Quiet)
	}

	var mu sync.Mutex
	pushes := 0
	h.e.OnAttention = func(DTO, core.State) { mu.Lock(); pushes++; mu.Unlock() }
	ch := h.e.Hub.Subscribe()
	defer h.e.Hub.Unsubscribe(ch)

	// Friday 2026-11-13, local time of the host.
	fri := func(hh int) time.Time { return time.Date(2026, 11, 13, hh, 0, 0, 0, time.Local) }
	h.e.Now = func() time.Time { return fri(12) }
	h.e.Tick()
	if d := dtoOf(h.e, id); d.QuietUntil != nil {
		t.Fatalf("quiet at noon: %v", d.QuietUntil)
	}
	drain(ch, "thread")

	h.e.Now = func() time.Time { return fri(18) }
	h.e.Tick()
	want := time.Date(2026, 11, 14, 6, 0, 0, 0, time.Local)
	if d := dtoOf(h.e, id); d.QuietUntil == nil || !d.QuietUntil.Equal(want) {
		t.Fatalf("quietUntil %v want %v", d.QuietUntil, want)
	}
	if fs := drain(ch, "thread"); len(fs) != 1 || fs[0]["quietUntil"] == nil {
		t.Fatalf("the window start sent %v", fs)
	}
	for _, pd := range h.e.Projects() {
		if pd.ID == pid && (pd.QuietUntil == nil || !pd.QuietUntil.Equal(want)) {
			t.Fatalf("project quietUntil %v", pd.QuietUntil)
		}
	}
	h.e.Tick()
	if fs := drain(ch, "thread"); len(fs) != 0 {
		t.Fatalf("a tick inside the window sent %d threads again", len(fs))
	}

	// Saturday 06:00: the window ends; the thread comes back without a push.
	h.e.Now = func() time.Time { return want }
	h.e.Tick()
	if fs := drain(ch, "thread"); len(fs) != 1 || fs[0]["quietUntil"] != nil {
		t.Fatalf("the window end sent %v", fs)
	}
	mu.Lock()
	if pushes != 0 {
		t.Fatalf("%d attention changes at the window edges", pushes)
	}
	mu.Unlock()

	// Clearing it sends the thread without quietUntil at once.
	h.e.Now = func() time.Time { return fri(18) }
	h.e.Tick()
	drain(ch, "thread")
	if r := h.cmd("project.update", "", core.Args{"id": pid, "quiet": nil}); !r.OK {
		t.Fatalf("%+v", r)
	}
	if projectOf(h.e, id).Quiet != nil {
		t.Fatal("quiet not cleared")
	}
	if fs := drain(ch, "thread"); len(fs) != 1 || fs[0]["quietUntil"] != nil {
		t.Fatalf("clearing sent %v", fs)
	}

	// Decision 0014 addendum: a list of windows. Weeknights and the weekend
	// join from Friday 17:00 to Monday 06:00; a bad row is refused by number.
	weeknights := map[string]any{"from": "17:00", "to": "06:00", "days": []any{"sun", "mon", "tue", "wed", "thu", "fri"}}
	weekend := map[string]any{"from": "00:00", "to": "00:00", "days": []any{"sat", "sun"}}
	if r := h.cmd("project.update", "", core.Args{"id": pid, "quiet": []any{weeknights, map[string]any{"from": "9", "to": "10:00"}}}); r.OK || r.Status != 400 || !strings.Contains(r.Error, "window 2") {
		t.Fatalf("a bad second window: %+v", r)
	}
	if r := h.cmd("project.update", "", core.Args{"id": pid, "quiet": "17:00"}); r.OK || r.Status != 400 {
		t.Fatalf("a string accepted: %+v", r)
	}
	if r := h.cmd("project.update", "", core.Args{"id": pid, "quiet": []any{weeknights, weekend}}); !r.OK {
		t.Fatalf("%+v", r)
	}
	if q := projectOf(h.e, id).Quiet; len(q) != 2 || q[1].From != "00:00" {
		t.Fatalf("list not stored: %+v", q)
	}
	h.e.Tick()
	mon := time.Date(2026, 11, 16, 6, 0, 0, 0, time.Local)
	if d := dtoOf(h.e, id); d.QuietUntil == nil || !d.QuietUntil.Equal(mon) {
		t.Fatalf("joined quietUntil %v want %v", d.QuietUntil, mon)
	}
	if r := h.cmd("project.update", "", core.Args{"id": pid, "quiet": []any{}}); !r.OK || projectOf(h.e, id).Quiet != nil {
		t.Fatalf("an empty list did not clear: %+v", r)
	}
}
