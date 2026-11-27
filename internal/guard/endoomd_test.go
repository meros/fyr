package guard

import (
	"slices"
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
)

// overOomdSig is the agents' stall past oomd's 85 % limit.
func overOomdSig() resource.Signals {
	sig := critical()
	sig.OomdLimit, sig.UnitStall = 85, f(92)
	return sig
}

func endInputs(v *fakeView, sig resource.Signals, held ...resource.Holder) Inputs {
	in := inputs(v, sig, held...)
	in.Can.End = true
	return in
}

// V0.oomd-frozen-work: past oomd's limit for OomdEndAfter the guard kills the
// frozen work that holds most, not the agent, and tells its thread.
func TestEndBeforeOomdKillsTheFrozenWorkFirst(t *testing.T) {
	v := &fakeView{mem: map[string]uint64{"b": 3e9, "c": 6e9}}
	in := endInputs(v, overOomdSig(), hold("a", time.Hour), hold("c", time.Minute))
	st0 := State{Frozen: []Frozen{{Owner: "b", At: t0.Add(-time.Minute), Undeclared: true, Cmd: "yarn check"}}, Short: true, OverOomd: t0.Add(-OomdEndAfter)}
	h := &host{ok: true, kill: 4, sig: overOomdSig()}
	st := Run(in, st0, h.apply)
	if !slices.ContainsFunc(h.applied, func(d Decision) bool { return d.Kind == Kill && d.Thread == "b" && d.Undeclared }) {
		t.Fatalf("did not kill b's frozen work: %v", h.kinds())
	}
	if slices.ContainsFunc(h.applied, func(d Decision) bool { return d.Kind == Kill && d.Thread != "b" }) {
		t.Fatalf("killed more than one thread's work: %+v", h.applied)
	}
	if len(st.Frozen) != 0 || !st.OverOomd.IsZero() {
		t.Fatalf("state %+v", st)
	}
	if n := h.notices("b"); len(n) != 1 || n[0]["ended"] != "yarn check" || n[0]["work"] != true {
		t.Fatalf("notice %+v", n)
	}
}

// Not before OomdEndAfter over the limit, and the clock starts again below it.
func TestEndBeforeOomdWaitsAndResets(t *testing.T) {
	v := &fakeView{mem: map[string]uint64{"c": 6e9}}
	in := endInputs(v, overOomdSig(), hold("a", time.Hour), hold("c", time.Minute))
	h := &host{ok: true, kill: 4, sig: overOomdSig()}
	st := Run(in, State{}, h.apply)
	if slices.Contains(h.kinds(), Kill) || !st.OverOomd.Equal(t0) {
		t.Fatalf("first tick over the limit: kinds %v over %v", h.kinds(), st.OverOomd)
	}
	h = &host{ok: true, kill: 4, sig: overOomdSig()}
	if st = Run(in, State{OverOomd: t0.Add(-OomdEndAfter + time.Second)}, h.apply); slices.Contains(h.kinds(), Kill) {
		t.Fatalf("killed before %s over the limit", OomdEndAfter)
	}
	sig := overOomdSig()
	sig.UnitStall = f(80)
	h = &host{ok: true, kill: 4, sig: sig}
	if st = Run(endInputs(v, sig, hold("a", time.Hour), hold("c", time.Minute)), State{OverOomd: t0.Add(-time.Minute)}, h.apply); slices.Contains(h.kinds(), Kill) || !st.OverOomd.IsZero() {
		t.Fatalf("below the limit: kinds %v over %v", h.kinds(), st.OverOomd)
	}
}

// With nothing frozen, the running heavy work that holds most, never the
// oldest: a job's hold goes back so its rerun is admitted again.
func TestEndBeforeOomdKillsTheLargestRunningWorkButTheOldest(t *testing.T) {
	v := &fakeView{mem: map[string]uint64{"a": 9e9, "c": 6e9, "d": 2e9}}
	in := endInputs(v, overOomdSig(), hold("a", time.Hour), hold("c", time.Minute), hold("d", 2*time.Minute))
	h := &host{ok: true, kill: 2, sig: overOomdSig()}
	Run(in, State{OverOomd: t0.Add(-time.Minute)}, h.apply)
	if !slices.ContainsFunc(h.applied, func(d Decision) bool { return d.Kind == Kill && d.Thread == "c" && !d.Undeclared }) {
		t.Fatalf("did not kill c's job: %+v", h.applied)
	}
	if !slices.ContainsFunc(h.applied, func(d Decision) bool { return d.Kind == Release && d.Thread == "c" }) {
		t.Fatalf("c's hold not released: %v", h.kinds())
	}
	// The oldest alone is never ended.
	h = &host{ok: true, kill: 2, sig: overOomdSig()}
	Run(endInputs(v, overOomdSig(), hold("a", time.Hour)), State{OverOomd: t0.Add(-time.Minute)}, h.apply)
	if slices.Contains(h.kinds(), Kill) {
		t.Fatal("ended the only, oldest, heavy work")
	}
}

// Running undeclared work is moved out of cli/ (frozen) before the kill, so
// the cgroup.kill of undeclared/ never takes the agent.
func TestEndBeforeOomdMovesUndeclaredWorkFirst(t *testing.T) {
	v := &fakeView{mem: map[string]uint64{"a": 1e9, "w": 7e9}}
	in := endInputs(v, overOomdSig(), hold("a", time.Hour))
	in.Flags = []resource.Undeclared{{Thread: "w", Since: t0.Add(-time.Minute), Command: "yarn smoke", MemOver: true}}
	h := &host{ok: true, kill: 3, sig: overOomdSig()}
	Run(in, State{OverOomd: t0.Add(-time.Minute)}, h.apply)
	i := slices.IndexFunc(h.applied, func(d Decision) bool { return d.Kind == FreezeUndeclared && d.Thread == "w" })
	j := slices.IndexFunc(h.applied, func(d Decision) bool { return d.Kind == Kill && d.Thread == "w" && d.Undeclared })
	if i < 0 || j < i {
		t.Fatalf("not moved, then killed: %v", h.kinds())
	}
	if n := h.notices("w"); len(n) != 1 || n[0]["ended"] != "yarn smoke" {
		t.Fatalf("notice %+v", n)
	}
}
