// Decision 0029 slice S2 (#186): admission by a budget of costs, gated by the slowdown and memory; one job always
// runs; a host without a provider keeps the fixed count.
package slots

import (
	"strings"
	"testing"
)

func f(v float64) *float64 { return &v }

func TestS2DecideByTheRules(t *testing.T) {
	// V0.163: a host of 4 cores and 8 GiB; the prior (1/N of N 2) is 2 cores and 4 GiB.
	p := Policy{SlowdownMax: 1.5, MemoryReserve: 0.10}
	host := func(slow, free float64, st Stall) Signals {
		return Signals{Provider: "cgroups", Slowdown: f(slow), Headroom: f(free), Stall: st, CPU: 4, Memory: 8 * gb}
	}
	job := Need{CPU: 2, Mem: 2 * gb}
	idle := Need{CPU: 0.2, Mem: gb / 2}
	cheap := Need{CPU: 0.4, Mem: gb / 2}
	for _, c := range []struct {
		name string
		sig  Signals
		held []Need
		head Need
		ok   bool
		why  string
	}{
		{"fits", host(1.1, 0.6, StallOK), []Need{job}, job, true, ""},
		{"over the CPU budget", host(1.1, 0.6, StallOK), []Need{job, job}, job, false, "needs ≈ 50 % CPU, 0 % free"},
		{"an idle hold leaves room", host(1.1, 0.6, StallOK), []Need{job, idle}, Need{CPU: 1.5, Mem: gb}, true, ""},
		{"slow CPU holds a normal job", host(2.1, 0.6, StallOK), []Need{job}, job, false, "new jobs run 2.1× slower"},
		{"slow CPU lets a cheap job in", host(2.1, 0.6, StallOK), []Need{job}, cheap, true, ""},
		{"progress: nothing runs, slow CPU", host(5, 0.6, StallOK), nil, job, true, ""},
		{"memory short holds even with nothing running", host(1, 0.06, StallOK), nil, job, false, "memory: 6 % free"},
		{"an elevated stall alone holds nothing (cache reclaim at memory.high)", host(1, 0.6, StallElevated), nil, job, true, ""},
		{"a critical stall holds", host(1, 0.6, StallCritical), nil, job, false, "memory stalls (critical)"},
		{"no provider: the fixed count", Signals{}, []Need{job}, job, true, ""},
		{"no provider: full", Signals{}, []Need{idle, idle}, job, false, "2 of 2 heavy jobs run"},
		{"no slowdown yet: budget and memory only", Signals{Provider: "proc", Headroom: f(0.6), CPU: 4, Memory: 8 * gb}, []Need{job}, job, true, ""},
	} {
		ok, why, _ := Decide(p, c.sig, Full, 2, c.held, c.head)
		if ok != c.ok || !strings.Contains(why, c.why) || (c.ok && why != "") {
			t.Errorf("%s: got %v %q, want %v %q", c.name, ok, why, c.ok, c.why)
		}
	}
}

func TestS2ThePoolAsksAdmitAndKeepsItsOrder(t *testing.T) {
	p := New(2)
	open := false
	p.Admit = func(head Holder, held []Holder) (bool, string, string) {
		if len(held) == 0 || open {
			return true, "", ""
		}
		return false, "CPU: new jobs run 2.0× slower", ResCPU
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
	p.Host = func() Signals { return Signals{Provider: "cgroups", CPU: 4, Memory: 8 * gb} }
	p.Cost = func(h Holder) Need {
		if h.Owner == "idle" {
			return Need{CPU: 0.2, Mem: gb / 2}
		}
		return Need{CPU: 2, Mem: gb}
	}
	p.Acquire("busy", Test)
	p.Acquire("idle", E2E)
	s := p.Snapshot()
	if s.Held[0].CPUPct != 50 || s.Held[0].MemPct != 13 || s.Held[1].CPUPct != 5 || *s.CPUPct != 55 || *s.MemPct != 19 || s.Tight != ResCPU {
		t.Fatalf("%+v %+v", s, s.Held)
	}
	// Older fronts read the one-number cost and use for one release.
	if s.Used == nil || *s.Used != 1.13 || s.Held[0].Cost != 1 {
		t.Fatalf("old fields %+v", s)
	}
}
