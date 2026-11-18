// SPEC 0.5 V0.147: the engine, the slot pool, the job tracker and the load
// sampler, wired as fyrd wires them, never wait on each other in a cycle.
// fyrd hung on 17 Nov: the engine's tick held e.mu and asked the tracker
// (Jobs.mu) when a slot was last active, while the sampler's tick held
// Jobs.mu and asked the engine (e.mu) for an ended hold's project.
package engine

import (
	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/lockwatch"
	"github.com/meros/fyr/internal/slots"
)

// noHang fails the test with every goroutine's stack when f does not
// return within d.
func noHang(t *testing.T, d time.Duration, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	select {
	case <-done:
	case <-time.After(d):
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Fatalf("hung for %s:\n%s", d, buf[:n])
	}
}

func TestV147LoadSlotsAndTicksDoNotDeadlock(t *testing.T) {
	e, _, _ := bare(t)
	pool := slots.New(1)
	e.UseSlots(pool)
	jobs := &cgroup.Jobs{Project: e.ThreadProject}
	e.SlotActive = jobs.Active
	guardDeps(t, e)
	proc := t.TempDir()
	if err := os.WriteFile(filepath.Join(proc, "loadavg"), []byte("1.00 1.00 1.00 1/1 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sampler := &cgroup.Sampler{Proc: proc, Cores: 4, Slots: pool, Jobs: jobs}
	pool.Cost = func(h slots.Holder) slots.Need {
		if _, ok := jobs.Active(h.Owner); ok {
			return slots.Need{CPU: 1}
		}
		return slots.Need{CPU: 1}
	}
	pool.Admit = func(head slots.Holder, held []slots.Holder) (bool, string, string) {
		_ = sampler.Signals()
		return len(held) < 1, "", ""
	}
	a, b := bareThread(t, e), bareThread(t, e)
	running(e, a) // a running holder: sweepSlots asks SlotActive under e.mu
	running(e, b)
	pool.Acquire(a, slots.Test)

	noHang(t, 10*time.Second, func() {
		stop := time.Now().Add(1500 * time.Millisecond)
		var wg sync.WaitGroup
		loop := func(f func()) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for time.Now().Before(stop) {
					f()
				}
			}()
		}
		loop(e.Tick)
		loop(sampler.Tick)
		loop(func() { _ = sampler.Get() }) // GET /api/load
		loop(func() { _, _ = e.Thread(a) })
		loop(func() {
			// b's holds end between two sampler ticks: Jobs records them
			// and asks the engine for b's project.
			pool.Release(a)
			pool.Acquire(b, slots.Test)
			pool.Release(b)
			pool.Acquire(a, slots.Test)
		})
		wg.Wait()
	})
}

// The owner hears of a stall while e.mu is the stuck lock.
func TestV147StallIsToldWhileTheEngineIsStuck(t *testing.T) {
	e, _, _ := bare(t)
	sub := e.Hub.Subscribe()
	defer e.Hub.Unsubscribe(sub)
	s := lockwatch.Stall{At: time.Now().UTC(), What: "lock engine.mu held 11s", File: "/d/stalls/x.txt"}
	e.mu.Lock()
	noHang(t, 2*time.Second, func() { e.SetStall(s) })
	e.mu.Unlock()
	got, _ := e.StallState(time.Now()).(lockwatch.Stall)
	if got.File != s.File {
		t.Fatalf("%+v", got)
	}
	if e.StallState(time.Now().Add(25*time.Hour)) != nil {
		t.Fatal("a day-old stall still shows")
	}
	select {
	case f := <-sub:
		if f.Event != "stall" {
			t.Fatalf("%+v", f)
		}
	case <-time.After(time.Second):
		t.Fatal("no stall frame")
	}
}

// SPEC 0.5 V0.182: the hooks no other test reaches are called without e.mu
// too: the owner's heavy-job and update commands and the gate's refusal count.
func TestV182DepsAreCalledWithoutTheEngineLock(t *testing.T) {
	e, _, now, a := undEngine(t)
	var calls []string
	e.HeavyRemove = func(p string) bool { calls = append(calls, "remove "+p); return true }
	e.UndeclaredSettings = func(map[string]any) error { calls = append(calls, "settings"); return nil }
	e.UpdateNow = func() error { calls = append(calls, "update"); return nil }
	e.HeavyRefused = func(class string) { calls = append(calls, "refused "+class) }
	e.Undeclared = func() []slots.Undeclared { return []slots.Undeclared{flagFor(a, *now)} }
	guardDeps(t, e)
	for i, c := range []api.Cmd{
		{Action: core.KindHeavyRemove, Args: map[string]any{"pattern": "make check"}},
		{Action: core.KindUndeclaredSettings, Args: map[string]any{"on": false}},
		{Action: core.KindUpdateNow},
	} {
		c.CommandID = "c" + string(rune('0'+i))
		if r := e.Command(c); !r.OK {
			t.Fatalf("%s: %+v", c.Action, r)
		}
	}
	e.Slots.Acquire("t_other1", slots.Test)
	e.Slots.Acquire("t_other2", slots.Test)
	e.HeavyGate(HeavyCall{Thread: a, Class: "make check", ToolUse: "tu"})
	e.Tick()
	want := []string{"remove make check", "settings", "update", "refused make check"}
	if !slices.Equal(calls, want) {
		t.Fatalf("calls %v, want %v", calls, want)
	}
}
