// Decision 0029 S3 (#187): slot_acquire may name the job's command, so its class (and cost) is known before it runs; a
// running job heavier than predicted tells its thread once; the owner resets a learned class.
package engine

import (
	"strings"
	"testing"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/slots"
)

func TestS3SlotAcquireNamesTheJob(t *testing.T) {
	e, _, _ := bare(t)
	p := slots.New(2)
	p.Now = e.Now // one clock for the pool and the engine
	e.UseSlots(p)
	a := bareThread(t, e)
	running(e, a)
	var got [2]string
	e.SlotJob = func(th, cmd string) { got = [2]string{th, cmd} }
	if out, isErr := callAs(t, e, a, "slot_acquire", map[string]any{"kind": "test", "command": "make test"}); isErr || !strings.Contains(out, "Admitted:") {
		t.Fatalf("%v %q", isErr, out)
	}
	if got != [2]string{a, "make test"} {
		t.Fatalf("%v", got)
	}
}

func TestS3HeavierThanPredictedIsToldOnce(t *testing.T) {
	e, _, _ := bare(t)
	p := slots.New(2)
	p.Now = e.Now
	e.UseSlots(p)
	a := bareThread(t, e)
	running(e, a)
	p.Acquire(a, slots.Test)
	e.SlotOver = func(owner string) (string, float64, float64, bool) { return "fyr: make test", 80, 50, owner == a }
	e.Tick()
	if !told(e, a, "Your job (fyr: make test) uses ≈ 80 % of this host; Fyr expected ≈ 50 %") {
		th, _ := e.Thread(a)
		t.Fatalf("%+v", th.Feed.Queue)
	}
	th, _ := e.Thread(a)
	n := len(th.Feed.Queue)
	e.Tick()
	if th, _ := e.Thread(a); len(th.Feed.Queue) != n {
		t.Fatal("told twice")
	}
}

func TestS3OwnerResetsAClass(t *testing.T) {
	e, _, _ := bare(t)
	p := slots.New(2)
	p.Now = e.Now // one clock for the pool and the engine
	e.UseSlots(p)
	var reset string
	e.JobReset = func(class string) bool { reset = class; return class == "fyr: make test" }
	r := e.Command(api.Cmd{CommandID: "c1", Action: core.KindJobReset, Args: map[string]any{"class": "fyr: make test"}})
	if !r.OK || reset != "fyr: make test" {
		t.Fatalf("%+v %q", r, reset)
	}
	if r := e.Command(api.Cmd{CommandID: "c2", Action: core.KindJobReset, Args: map[string]any{"class": "nope"}}); r.OK {
		t.Fatalf("an unknown class: %+v", r)
	}
}
