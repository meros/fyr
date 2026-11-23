// SPEC 0.5 V0.147: the job tracker calls nothing out of itself under its
// lock, and GET /api/load answers while the pool or the tracker is busy.
package cgroup

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
	"github.com/meros/fyr/internal/slots"
)

func within(t *testing.T, d time.Duration, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	select {
	case <-done:
	case <-time.After(d):
		buf := make([]byte, 1<<20)
		t.Fatalf("%s hung:\n%s", what, buf[:runtime.Stack(buf, true)])
	}
}

// Project is the engine's ThreadProject, which takes the engine's lock; the
// engine's tick asks Active under that lock. A Project that asks the
// tracker back stands for that cycle: it must not wait on the tracker.
func TestV147RecordAsksProjectWithoutTheLock(t *testing.T) {
	j := &Jobs{}
	j.Project = func(th string) string {
		j.Active(th)
		j.Live(th)
		return "fyr"
	}
	p := slots.New(1)
	p.Acquire("t_a", resource.Test)
	t0 := time.Now()
	j.tick(nil, nil, p.Snapshot(), nil, nil, t0)
	j.tick(nil, nil, p.Snapshot(), nil, nil, t0.Add(5*time.Second))
	p.Release("t_a")
	within(t, 5*time.Second, "an ended hold's record", func() {
		j.tick(nil, nil, p.Snapshot(), nil, nil, t0.Add(10*time.Second))
	})
	if rs := j.Records(); len(rs) != 1 || rs[0].Class != "fyr: test" {
		t.Fatalf("%+v", rs)
	}
	if cs := j.View().Classes; len(cs) != 1 || cs[0].Class != "fyr: test" {
		t.Fatalf("the view did not follow: %+v", cs)
	}
}

func TestV147LoadAnswersWhileThePoolAndTheTrackerAreBusy(t *testing.T) {
	proc := t.TempDir()
	write(t, filepath.Join(proc, "loadavg"), "1.00 2.00 3.00 1/10 1\n")
	p := slots.New(1)
	j := &Jobs{}
	s := &Sampler{Proc: proc, Cores: 4, Slots: p, Jobs: j}
	p.Acquire("t_a", resource.Test)
	j.Saw("t_a", "make test")
	j.tick(nil, nil, p.Snapshot(), nil, nil, time.Now())
	if l := s.Get(); len(l.Slots.Held) != 1 {
		t.Fatalf("%+v", l.Slots)
	}
	// Admission runs under the pool's lock; one that hangs holds it.
	block, entered := make(chan struct{}), make(chan struct{})
	p.Admit = func(resource.Holder, []resource.Holder) (bool, string, string) {
		close(entered)
		<-block
		return false, "x", ""
	}
	go p.Acquire("t_b", resource.Test)
	<-entered
	j.mu.Lock() // and the tracker's lock is held too
	within(t, 2*time.Second, "GET /api/load", func() {
		l := s.Get()
		if l.Slots == nil || len(l.Slots.Held) != 1 || l.Slots.Held[0].Owner != "t_a" {
			t.Errorf("not the last snapshot: %+v", l.Slots)
		}
	})
	within(t, 2*time.Second, "Signals", func() { s.Signals() })
	j.mu.Unlock()
	close(block)
}

// Decision 0029 S3 on V0.147's order: the S3 paths that run under the pool's lock (Admit, Backfill and Cost asking
// the tracker: ClassOf, Learned, LiveUse, Active) answer while the engine is stuck, since none calls Project, and
// learning stores without j.mu.
func TestS3PoolCallbacksNeverReachTheEngine(t *testing.T) {
	stuck := make(chan struct{})
	j := &Jobs{KV: kv{}, Project: func(string) string { <-stuck; return "fyr" }} // the engine, stuck
	p := slots.New(1)
	p.Admit = func(head resource.Holder, held []resource.Holder) (bool, string, string) {
		j.ClassOf(head.Owner, head.Kind)
		j.Learned("fyr: test")
		j.LiveUse(head.Owner)
		j.Active(head.Owner)
		return len(held) == 0, "busy", ""
	}
	p.Backfill = func(head, c resource.Holder, held []resource.Holder) bool {
		j.ClassOf(c.Owner, c.Kind)
		return true
	}
	within(t, 2*time.Second, "admission with the engine stuck", func() {
		p.Acquire("t_a", resource.Test)
		p.Acquire("t_b", resource.Test)
		p.Acquire("t_c", resource.Test)
	})
	close(stuck)
}
