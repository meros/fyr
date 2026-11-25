package engine

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// quietRig is a thread whose turn ended on a background command that never
// ends, with a clock and a cgroup the test moves.
type quietRig struct {
	h      *harness
	id     string
	skew   atomic.Int64
	cpu    atomic.Int64
	frozen atomic.Bool
}

func newQuietRig(t *testing.T, prompt string) *quietRig {
	t.Helper()
	r := &quietRig{h: newHarness(t, time.Hour)}
	r.h.e.Now = func() time.Time { return time.Now().Add(time.Duration(r.skew.Load())) }
	r.h.e.ThreadCPUTime = func(string) (time.Duration, bool) { return time.Duration(r.cpu.Load()), true }
	r.h.e.ThreadFrozen = func(string) bool { return r.frozen.Load() }
	r.id = r.h.create(prompt)
	r.h.wait(r.id, "the turn ended on the background command", func(t core.Thread, a core.Attention) bool {
		return a.State == core.Waiting && !t.Running && len(t.Tasks) == 1 && t.Tasks[0].Background
	})
	return r
}

// pass moves the clock n minutes, one sweep a minute, and the cgroup by
// cores in each.
func (r *quietRig) pass(n int, cores float64) {
	for range n {
		r.skew.Add(int64(time.Minute))
		r.cpu.Add(int64(cores * float64(time.Minute)))
		r.h.e.sweepQuietWaits()
	}
}

// told counts the turns the notice started: the fake agent echoes what it
// reads, and only the notice starts a turn after the first.
func (r *quietRig) told() int {
	th, _ := r.h.e.Thread(r.id)
	if th.RunEnds > 1 && !strings.Contains(th.Summary, "almost no CPU") {
		r.h.t.Fatalf("the agent read %q", th.Summary)
	}
	return max(th.RunEnds-1, 0)
}

func (r *quietRig) reason() string {
	th, _ := r.h.e.Thread(r.id)
	v := r.h.viewOf(r.id)
	return core.ComputeWith(&th, &v, time.Now(), nil).Reason
}

func (r *quietRig) task() core.Task {
	th, _ := r.h.e.Thread(r.id)
	return th.Tasks[0]
}

// The 18 Nov stall: the awaited work is over, the wait never ends. The agent
// is woken, once, and the thread shows what it waits for and since when.
func TestStuckBackgroundWaitWakesTheAgentOnce(t *testing.T) {
	r := newQuietRig(t, "bgstuck")
	if got := r.reason(); got != "Running in background: Wait for both experiments to finish" {
		t.Fatalf("reason %q", got)
	}
	r.pass(10, 3) // the experiment still runs: no word
	if r.told() != 0 || r.task().QuietSince != nil {
		t.Fatalf("told during real work: %d %+v", r.told(), r.task())
	}
	r.pass(7, 0.01) // it ended; only the poll is left
	r.h.wait(r.id, "the agent was told and its turn ended", func(th core.Thread, _ core.Attention) bool {
		return r.told() == 1 && !th.Running
	})
	if k := r.task(); k.QuietSince == nil || !k.QuietTold || k.Status != core.TaskRunning {
		t.Fatalf("task %+v", k)
	}
	if !hasEvent(r.h.events(r.id), "task", `"kind":"quiet"`) {
		t.Fatal("the timeline does not say it")
	}
	r.pass(20, 0.01)
	time.Sleep(100 * time.Millisecond)
	if n := r.told(); n != 1 {
		t.Fatalf("told %d times", n)
	}
}

func TestBusyBackgroundJobIsNeverCalledStuck(t *testing.T) {
	r := newQuietRig(t, "bgstuck")
	for range 6 { // a build with pauses shorter than QuietAfter
		r.pass(4, 0.01)
		r.pass(1, 2)
	}
	if r.told() != 0 || r.task().QuietSince != nil {
		t.Fatalf("told %d, %+v", r.told(), r.task())
	}
}

func TestQuietMarkClearsWhenTheJobWorksAgain(t *testing.T) {
	r := newQuietRig(t, "bgstuck")
	r.pass(7, 0)
	r.h.wait(r.id, "told", func(th core.Thread, _ core.Attention) bool { return r.told() == 1 && !th.Running })
	// Two sweeps: a sweep during the agent's turn drops the watch, and the
	// first one after it then only takes a new reading.
	r.pass(2, 2)
	if k := r.task(); k.QuietSince != nil {
		t.Fatalf("still marked quiet: %+v", k)
	}
}

func TestMonitorAndFrozenWorkAreNotStuck(t *testing.T) {
	m := newQuietRig(t, "bgmonitor")
	m.pass(30, 0)
	if m.told() != 0 {
		t.Fatal("a quiet Monitor was reported")
	}
	if got := m.reason(); got != "Running in background: CI on the PR" {
		t.Fatalf("reason %q", got)
	}
	f := newQuietRig(t, "bgstuck")
	f.frozen.Store(true)
	f.pass(30, 0)
	if f.told() != 0 {
		t.Fatal("frozen work was reported")
	}
}

// A suspended host measures nothing: the first look after it starts over.
func TestAGapInTheClockIsNotQuiet(t *testing.T) {
	r := newQuietRig(t, "bgstuck")
	r.pass(2, 0)
	r.skew.Add(int64(2 * time.Hour))
	r.h.e.sweepQuietWaits()
	r.pass(2, 0)
	if r.told() != 0 {
		t.Fatal("told after a gap")
	}
}

// V0.quiet-remote-wait: on 25 Nov every quiet wake was a wait on a CI run,
// a deploy or a job. A command that visibly waits on a remote system is
// left alone for QuietRemoteAfter, and then told once like any other.
func TestRemoteWaitIsToldMuchLater(t *testing.T) {
	r := newQuietRig(t, "bgremote")
	if !r.task().RemoteWait {
		t.Fatalf("not read as a remote wait: %+v", r.task())
	}
	r.pass(int(QuietRemoteAfter/time.Minute)-5, 0.01)
	time.Sleep(100 * time.Millisecond)
	if r.told() != 0 || r.task().QuietTold {
		t.Fatalf("told %d about a remote wait, %+v", r.told(), r.task())
	}
	r.pass(10, 0.01)
	r.h.wait(r.id, "told after QuietRemoteAfter", func(th core.Thread, _ core.Attention) bool {
		return r.told() == 1 && !th.Running
	})
	r.pass(30, 0.01)
	time.Sleep(100 * time.Millisecond)
	if n := r.told(); n != 1 {
		t.Fatalf("told %d times", n)
	}
}

// A local wait keeps the short count.
func TestLocalWaitIsNotARemoteWait(t *testing.T) {
	r := newQuietRig(t, "bgstuck")
	if r.task().RemoteWait {
		t.Fatalf("a local loop read as a remote wait: %+v", r.task())
	}
}
