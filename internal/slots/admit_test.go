// Decision 0029 slice S2 (#186): admission by a budget of costs, gated by the slowdown and memory; one job always
// runs; a host without a provider keeps the fixed count.
package slots

import (
	"strings"
	"testing"
)

func f(v float64) *float64 { return &v }

func TestS2DecideByTheRules(t *testing.T) {
	p := Policy{SlowdownMax: 1.5, MemoryReserve: 0.10}
	calm := Signals{Provider: "cgroups", Slowdown: f(1.1), Headroom: f(0.4), Stall: StallOK}
	for _, c := range []struct {
		name string
		sig  Signals
		held []float64
		head float64
		ok   bool
		why  string
	}{
		{"fits", calm, []float64{1}, 1, true, ""},
		{"over the budget", calm, []float64{1, 1}, 1, false, "needs ≈ 50 %, 0 % free"},
		{"an idle hold leaves room", calm, []float64{1, 0.1}, 0.5, true, ""},
		{"slow CPU holds a normal job", Signals{Provider: "cgroups", Slowdown: f(2.1), Headroom: f(0.4), Stall: StallOK}, []float64{1}, 1, false, "new jobs run 2.1× slower"},
		{"slow CPU lets a cheap job in", Signals{Provider: "cgroups", Slowdown: f(2.1), Headroom: f(0.4), Stall: StallOK}, []float64{1}, 0.25, true, ""},
		{"progress: nothing runs, slow CPU", Signals{Provider: "cgroups", Slowdown: f(5), Headroom: f(0.4), Stall: StallOK}, nil, 1, true, ""},
		{"memory short holds even with nothing running", Signals{Provider: "cgroups", Slowdown: f(1), Headroom: f(0.06), Stall: StallOK}, nil, 1, false, "memory: 6 % free"},
		{"an elevated stall alone holds nothing (cache reclaim at memory.high)", Signals{Provider: "cgroups", Slowdown: f(1), Headroom: f(0.4), Stall: StallElevated}, nil, 1, true, ""},
		{"a critical stall holds", Signals{Provider: "cgroups", Slowdown: f(1), Headroom: f(0.4), Stall: StallCritical}, nil, 1, false, "memory stalls (critical)"},
		{"no provider: the fixed count", Signals{}, []float64{1}, 1, true, ""},
		{"no provider: full", Signals{}, []float64{0.1, 0.1}, 1, false, "2 of 2 heavy jobs run"},
		{"no slowdown yet: budget and memory only", Signals{Provider: "proc", Headroom: f(0.5)}, []float64{1}, 1, true, ""},
	} {
		ok, why := Decide(p, c.sig, 2, 2, c.held, c.head)
		if ok != c.ok || !strings.Contains(why, c.why) || (c.ok && why != "") {
			t.Errorf("%s: got %v %q, want %v %q", c.name, ok, why, c.ok, c.why)
		}
	}
}

func TestS2ThePoolAsksAdmitAndKeepsItsOrder(t *testing.T) {
	p := New(2)
	open := false
	p.Admit = func(head Holder, held []Holder) (bool, string) {
		if len(held) == 0 || open {
			return true, ""
		}
		return false, "CPU: new jobs run 2.0× slower"
	}
	if held, _ := p.Acquire("a", Test); !held {
		t.Fatal("progress: the first job is admitted")
	}
	if held, ahead := p.Acquire("b", Test); held || ahead != 0 {
		t.Fatal("b should wait")
	}
	p.Acquire("c", Build)
	if s := p.Snapshot(); s.Why != "CPU: new jobs run 2.0× slower" || len(s.Waiting) != 2 || s.Waiting[0].Owner != "b" {
		t.Fatalf("%+v", s)
	}
	var granted []string
	p.OnGrant = func(owner, _ string) { granted = append(granted, owner) }
	open = true
	p.Fill() // the host's tick: conditions changed, nothing was released
	if s := p.Snapshot(); len(s.Held) != 3 || s.Why != "" || len(granted) != 2 || granted[0] != "b" {
		t.Fatalf("after the fill %+v %v", s, granted)
	}
}

func TestS2SnapshotCarriesCostsAndUse(t *testing.T) {
	p := New(2)
	p.Cost = func(h Holder) float64 {
		if h.Owner == "idle" {
			return 0.1
		}
		return 1
	}
	p.Acquire("busy", Test)
	p.Acquire("idle", E2E)
	s := p.Snapshot()
	if s.Used == nil || *s.Used != 1.1 || s.Held[0].Cost != 1 || s.Held[1].Cost != 0.1 {
		t.Fatalf("%+v", s)
	}
}
