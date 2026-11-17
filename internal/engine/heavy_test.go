// #174: a heavy command without a held slot is refused at the gate hook with the call that fixes it; a queued thread
// is told to wait; a held slot with no heavy job for a while gets one reminder to release it.
package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/slots"
)

func TestH174HeavyCommandWithoutASlotIsRefused(t *testing.T) {
	e, _, _ := bare(t)
	e.UseSlots(slots.New(1))
	a, b := bareThread(t, e), bareThread(t, e)
	running(e, a)
	running(e, b)
	if r := e.HeavyGate(a, ""); r != "" {
		t.Fatalf("a quick command was refused: %q", r)
	}
	r := e.HeavyGate(a, "make test")
	if !strings.Contains(r, "make test") || !strings.Contains(r, `mcp__fyr__slot_acquire {"kind":"test"}`) {
		t.Fatalf("no slot: %q", r)
	}
	if r := e.HeavyGate(a, "yarn screenshot:diff"); !strings.Contains(r, `{"kind":"e2e"}`) {
		t.Fatalf("the kind follows the class: %q", r)
	}
	e.Slots.Acquire(a, slots.Test)
	if r := e.HeavyGate(a, "make test"); r != "" {
		t.Fatalf("a holder was refused: %q", r)
	}
	e.Slots.Acquire(b, slots.Test)
	if r := e.HeavyGate(b, "go test ./..."); !strings.Contains(r, "queued") || !strings.Contains(r, "0 ahead") {
		t.Fatalf("queued: %q", r)
	}
	// Slots off: nothing is refused.
	e2, _, _ := bare(t)
	e2.UseSlots(slots.New(0))
	if r := e2.HeavyGate(bareThread(t, e2), "make test"); r != "" {
		t.Fatalf("slots off: %q", r)
	}
}

func TestH174AnIdleHeldSlotGetsOneReminder(t *testing.T) {
	e, _, now := bare(t)
	p := slots.New(2)
	p.Now = e.Now
	e.UseSlots(p)
	a := bareThread(t, e)
	running(e, a)
	start := *now
	active := start
	e.SlotActive = func(owner string) (time.Time, bool) { return active, owner == a }
	e.Slots.Acquire(a, slots.Test)
	*now = start.Add(4 * time.Minute)
	e.Tick()
	if told(e, a, "slot_release") {
		t.Fatal("reminded before 5 min")
	}
	*now = start.Add(5*time.Minute + time.Second)
	e.Tick()
	if !told(e, a, "You hold a test slot but no heavy job ran in it for 5 min") {
		th, _ := e.Thread(a)
		t.Fatalf("no reminder: %+v", th.Feed.Queue)
	}
	// Once per hold: the next tick says nothing new.
	th, _ := e.Thread(a)
	n := len(th.Feed.Queue)
	*now = start.Add(8 * time.Minute)
	e.Tick()
	if th, _ := e.Thread(a); len(th.Feed.Queue) != n {
		t.Fatalf("reminded twice: %+v", th.Feed.Queue)
	}
	if _, ok := e.Slots.Holding(a); !ok {
		t.Fatal("the reminder took the slot away")
	}
	_ = core.KindSlotGranted
}
