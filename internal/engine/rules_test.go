package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// #46: the agent arms a watch with its tool; the owner sees it on the
// thread; the engine's clock fires it once at its nextCheck, which records
// rule.fired and removes it.
func TestWatchToolFiresOnce(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("hello")
	h.wait(id, "done", state(core.Done))
	at := time.Now().Add(time.Hour)
	out, isErr := callAs(t, h.e, id, "watch", map[string]any{"when": map[string]any{"kind": "at", "at": at.Format(time.RFC3339)}, "then": "notify", "note": "check the deploy"})
	if isErr || !strings.Contains(out, "Armed the watch w_") {
		t.Fatalf("watch: %s", out)
	}
	wire := map[string]any{}
	b, _ := json.Marshal(dtoOf(h.e, id))
	json.Unmarshal(b, &wire)
	rs, _ := wire["rules"].([]any)
	if len(rs) != 1 || rs[0].(map[string]any)["createdBy"] != core.ByAgent {
		t.Fatalf("the owner does not see the watch: %s", b)
	}
	count := func() int {
		n := 0
		evs, _ := h.e.Events(id, 0, 500)
		for _, ev := range evs {
			if ev.Type == "rule.fired" && strings.Contains(string(ev.Data), "check the deploy") {
				n++
			}
		}
		return n
	}
	h.e.Tick()
	if count() != 0 {
		t.Fatal("fired early")
	}
	h.e.Now = func() time.Time { return at.Add(time.Minute) }
	h.e.Tick()
	h.e.Tick()
	if th, _ := h.e.Thread(id); count() != 1 || len(th.Rules) != 0 {
		t.Fatalf("fired %d times, rules %+v", count(), th.Rules)
	}
	if out, isErr := callAs(t, h.e, id, "watch", map[string]any{"remove": "w_nope"}); !isErr {
		t.Fatalf("removed an unknown watch: %s", out)
	}
}

// #46: a snooze is the View's Rule; the wire view keeps snoozeUntil and
// snoozedAt, and the clock ends it.
func TestSnoozeWireAndEnd(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("hello")
	h.wait(id, "done", state(core.Done))
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	if r := h.cmd("thread.snooze", id, core.Args{"until": until.Format(time.RFC3339)}); !r.OK {
		t.Fatalf("snooze: %+v", r)
	}
	d := dtoOf(h.e, id)
	if d.View.SnoozeUntil == nil || !d.View.SnoozeUntil.Equal(until) || d.View.SnoozedAt == nil || d.Attention.State != core.Snoozed {
		t.Fatalf("view: %+v %+v", d.View, d.Attention)
	}
	h.e.Now = func() time.Time { return until.Add(time.Second) }
	h.e.Tick()
	if d := dtoOf(h.e, id); d.View.SnoozeUntil != nil || len(d.View.Rules) != 0 {
		t.Fatalf("the snooze did not end: %+v", d.View)
	}
}
