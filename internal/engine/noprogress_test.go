package engine

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// fakeClock is the engine's clock in a test: it moves only when told.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func noProgressNotice(t core.Thread) bool {
	return slices.ContainsFunc(t.Feed.Queue, func(n core.Notice) bool { return n.Key == "no-progress" })
}

// V0.no-progress (#448): with the fake agent and the engine's clock moved by
// hand, a run in a tool that hangs (no tool call, no output) is blocked at
// NoProgressAfter, the agent is told once, and the tool's end clears it.
func TestNoProgressBlocksSilentRun(t *testing.T) {
	h := newHarness(t, time.Minute)
	clk := &fakeClock{now: time.Date(2026, 11, 20, 9, 0, 0, 0, time.Local)}
	h.e.Now = clk.Now
	id := h.create("stall")
	h.wait(id, "the hanging tool", func(t core.Thread, _ core.Attention) bool {
		return t.Running && hasEvent(h.events(id), "msg", "WebFetch")
	})

	clk.add(core.NoProgressAfter - time.Second)
	h.e.Tick()
	if th := h.wait(id, "working", runLive); th.NoProgress != nil {
		t.Fatalf("blocked before %s: %+v", core.NoProgressAfter, th.NoProgress)
	}
	clk.add(time.Second)
	h.e.Tick()
	th := h.wait(id, "blocked", state(core.Blocked))
	v := h.viewOf(id)
	a := core.Compute(&th, &v, clk.Now())
	if a.Group != core.GroupUnblock || a.Word != "No progress for 20 min" || !strings.Contains(a.Subject, "last: ") || !strings.Contains(a.Subject, " at 09:00") {
		t.Fatalf("attention %+v", a)
	}
	if !noProgressNotice(th) {
		t.Fatalf("no notice queued: %+v", th.Feed.Queue)
	}
	// Later clocks tell the agent nothing more.
	clk.add(30 * time.Minute)
	h.e.Tick()
	th, _ = h.e.Thread(id)
	n := 0
	for _, ev := range h.events(id) {
		if ev.Type == core.EventNoProgress {
			n++
		}
	}
	if n != 1 || th.NoProgress == nil {
		t.Fatalf("%d no-progress events, state %+v", n, th.NoProgress)
	}

	// The tool ends: progress clears the state.
	if r := h.cmd("thread.send", id, core.Args{"text": "release"}); !r.OK {
		t.Fatalf("send: %+v", r)
	}
	h.wait(id, "cleared", func(t core.Thread, _ core.Attention) bool { return t.NoProgress == nil })
	if !hasEvent(h.events(id), core.EventProgress, "") {
		t.Fatal("no agent.progress event")
	}
}

// V0.no-progress: a Bash call that runs long (a build) is progress.
func TestNoProgressLongBashIsWork(t *testing.T) {
	h := newHarness(t, time.Minute)
	clk := &fakeClock{now: time.Date(2026, 11, 20, 9, 0, 0, 0, time.Local)}
	h.e.Now = clk.Now
	id := h.create("hang")
	h.wait(id, "the long command", func(t core.Thread, _ core.Attention) bool { return t.Running && t.OpenBash == 1 })
	for range 3 {
		clk.add(core.NoProgressAfter)
		h.e.Tick()
	}
	th, _ := h.e.Thread(id)
	v := h.viewOf(id)
	if a := core.Compute(&th, &v, clk.Now()); a.State != core.Working || th.NoProgress != nil || noProgressNotice(th) {
		t.Fatalf("long Bash: %+v %+v", a, th.NoProgress)
	}
	if r := h.cmd("thread.interrupt", id, nil); !r.OK {
		t.Fatalf("interrupt: %+v", r)
	}
	h.wait(id, "stopped", func(t core.Thread, _ core.Attention) bool { return !t.Running })
}
