package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

// #45, decision 0020 (3): an effect runs at most once per effect id across a
// host restart. The host dies after a pressed script started; the next host
// does not run it again, and its result lands: the run ends, failed, with
// why, and the agent's thread records it.
func TestEffectAtMostOnceAcrossRestart(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	started, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(release) })
	h.e.Scripts = func(context.Context, string, []string) (string, int, error) {
		close(started)
		<-release // the host dies before the script returns
		return "applied", 0, nil
	}
	if err := declareOffer(h.e, id, "thread", core.ActionSpec{ID: "apply", Label: "Apply", Run: core.RunScript, Argv: []string{"make", "apply"}}); err != nil {
		t.Fatal(err)
	}
	th, _ := h.e.Thread(id)
	if r := h.cmdID("p1", "action.run", id, core.Args{"on": "thread", "action": "apply", "digest": findOffer(th.Offers, "apply").Digest}); !r.OK {
		t.Fatalf("press: %+v", r)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the script did not start")
	}

	// The next host, on the same database.
	h2 := startHarness(t, h.dir, h.db, time.Minute)
	var again atomic.Int32
	h2.e.Scripts = func(context.Context, string, []string) (string, int, error) {
		again.Add(1)
		return "applied", 0, nil
	}
	if err := h2.e.ResumeEffects(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the lost result landed", func() bool {
		th, _ := h2.e.Thread(id)
		return len(th.Runs) == 1 && !th.Runs[0].Running
	})
	th, _ = h2.e.Thread(id)
	if th.Runs[0].OK || !strings.Contains(th.Runs[0].Output, "not run again") {
		t.Fatalf("run %+v", th.Runs[0])
	}
	if !hasEvent(h2.events(id), "action", `"ended":true`) {
		t.Fatal("no ended action event")
	}
	// A third start finds nothing left to do.
	if err := h2.e.ResumeEffects(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if n := again.Load(); n != 0 {
		t.Fatalf("the script ran %d more times after the restart", n)
	}
}

// #45: a durable effect that finished, whose result did not land before
// the host stopped, lands its stored result after the restart, once; the
// old runner's late report is not landed again.
func TestEffectResultLandsAfterRestart(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("x")
	h.wait(id, "done", state(core.Done))
	started, release := make(chan struct{}), make(chan struct{})
	h.e.Scripts = func(context.Context, string, []string) (string, int, error) {
		close(started)
		<-release
		return "40 passed", 0, nil
	}
	_ = declareOffer(h.e, id, "thread", core.ActionSpec{ID: "e2e", Label: "E2E", Run: core.RunScript, Argv: []string{"make", "e2e"}})
	th, _ := h.e.Thread(id)
	if r := h.cmdID("p1", "action.run", id, core.Args{"on": "thread", "action": "e2e", "digest": findOffer(th.Offers, "e2e").Digest}); !r.OK {
		t.Fatalf("press: %+v", r)
	}
	<-started
	open, _ := h.e.st.OpenEffects()
	if len(open) != 1 || open[0].State != store.EffectStarted {
		t.Fatalf("effects %+v", open)
	}
	// The runner stored its result; the host died before it landed.
	var ef core.Effect
	json.Unmarshal(open[0].Data, &ef)
	h.e.st.MoveEffect(ef.ID, store.EffectStarted, store.EffectFinished, core.EffectResult{Effect: ef, OK: true, Text: "41 passed"})
	h2 := startHarness(t, h.dir, h.db, time.Minute)
	if err := h2.e.ResumeEffects(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the stored result landed", func() bool {
		th, _ := h2.e.Thread(id)
		return len(th.Runs) == 1 && !th.Runs[0].Running
	})
	close(release) // the old runner reports late
	time.Sleep(50 * time.Millisecond)
	th, _ = h2.e.Thread(id)
	if !th.Runs[0].OK || !strings.Contains(th.Runs[0].Output, "41 passed") {
		t.Fatalf("run %+v", th.Runs[0])
	}
	if open, _ := h2.e.st.OpenEffects(); len(open) != 0 {
		t.Fatalf("open effects after landing: %+v", open)
	}
	n := 0
	for _, ev := range h2.events(id) {
		if ev.Type == "action" && strings.Contains(string(ev.Data), `"ended":true`) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d results landed, want 1", n)
	}
}
