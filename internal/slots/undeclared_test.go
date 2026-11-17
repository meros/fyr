// V0.164 sections 6 and 12.4 (B1–B10): a flagged thread's use is one more Need in admission, beside the admitted jobs
// and not among them; a thread's own undeclared use does not count against its own request.
package slots

import (
	"strings"
	"testing"
	"time"
)

func extra(name string, cpu float64, mem uint64) Extra {
	return Extra{Owner: name, Name: name, Need: Need{CPU: cpu, Mem: mem}}
}

func TestB1AFlaggedThreadsCPUMakesTheHeadWait(t *testing.T) {
	held := []Need{{CPU: 2.4, Mem: gb}}                    // 40 % of 6 cores
	flagged := []Extra{extra("Map perf", 3, gb)}           // 50 %
	head := Need{CPU: 1.8, Mem: gb}                        // 30 %
	ok, why, res := DecideWith(DefaultPolicy, host6, Full, 2, held, flagged, "t_head", head)
	if ok || res != ResCPU || why != "needs ≈ 30 % CPU, 10 % free (Map perf uses 50 % undeclared)" {
		t.Fatalf("%v %q %q", ok, why, res)
	}
}

func TestB2WithoutTheFlaggedThreadItIsAdmitted(t *testing.T) {
	if ok, why, _ := DecideWith(DefaultPolicy, host6, Full, 2, []Need{{CPU: 2.4, Mem: gb}}, nil, "t_head", Need{CPU: 1.8, Mem: gb}); !ok {
		t.Fatal(why)
	}
	// Decide is DecideWith without extras.
	if ok, _, _ := Decide(DefaultPolicy, host6, Full, 2, []Need{{CPU: 2.4, Mem: gb}}, Need{CPU: 1.8, Mem: gb}); !ok {
		t.Fatal("Decide changed")
	}
}

func TestB3MemoryOfAFlaggedThreadBinds(t *testing.T) {
	held := []Need{{CPU: 0.5, Mem: 4 * gb}}
	flagged := []Extra{extra("Map perf", 0.2, 5*gb)}
	ok, why, res := DecideWith(DefaultPolicy, host6, Full, 2, held, flagged, "t_head", Need{CPU: 0.5, Mem: 6 * gb})
	if ok || res != ResMem || !strings.Contains(why, "needs ≈ 6.4 GB memory") || !strings.Contains(why, "Map perf uses") || !strings.Contains(why, "undeclared") {
		t.Fatalf("%v %q %q", ok, why, res)
	}
}

func TestB4TheDimensionsAreApart(t *testing.T) {
	held := []Need{{CPU: 0.1, Mem: gb / 2}}
	flagged := []Extra{extra("Map perf", 5, gb)} // CPU-heavy
	if ok, why, _ := DecideWith(DefaultPolicy, host6, Full, 2, held, flagged, "t_head", Need{CPU: 0.6, Mem: 6 * gb}); !ok {
		t.Fatalf("a memory-heavy head beside a CPU-heavy flagged thread: %q", why)
	}
}

func TestB5OneJobThatAskedAlwaysRuns(t *testing.T) {
	flagged := []Extra{extra("Map perf", 5.4, gb)} // 90 %
	if ok, why, _ := DecideWith(DefaultPolicy, host6, Full, 2, nil, flagged, "t_head", Need{CPU: 3, Mem: gb}); !ok {
		t.Fatalf("nothing admitted, so the head starts: %q", why)
	}
	// The memory rules can still hold it.
	low := host6
	low.Headroom = f(0.05)
	if ok, _, res := DecideWith(DefaultPolicy, low, Full, 2, nil, flagged, "t_head", Need{CPU: 3, Mem: gb}); ok || res != ResMem {
		t.Fatal("the memory reserve did not hold")
	}
}

func TestB6AThreadsOwnUseDoesNotCountAgainstItsOwnRequest(t *testing.T) {
	held := []Need{{CPU: 1, Mem: gb}}
	flagged := []Extra{extra("t_me", 3, gb)} // 50 %, its own
	flagged[0].Owner = "t_me"
	if ok, why, _ := DecideWith(DefaultPolicy, host6, Full, 2, held, flagged, "t_me", Need{CPU: 3, Mem: gb}); !ok {
		t.Fatalf("its own use was counted: %q", why)
	}
	if ok, _, _ := DecideWith(DefaultPolicy, host6, Full, 2, held, flagged, "t_other", Need{CPU: 3, Mem: gb}); ok {
		t.Fatal("another thread's request ignored the flagged use")
	}
}

func TestB7B8TheEpisodeEndsAndTheQueueMovesAtOnce(t *testing.T) {
	flagged := []Extra{extra("Map perf", 3, gb)}
	p := New(2)
	p.Host = func() Signals { return host6 }
	p.Cost = func(h Holder) Need { return Need{CPU: 2.4, Mem: gb} }
	var granted []string
	p.OnGrant = func(o, k string) { granted = append(granted, o) }
	p.Admit = func(head Holder, held []Holder) (bool, string, string) {
		var needs []Need
		for range held {
			needs = append(needs, Need{CPU: 2.4, Mem: gb})
		}
		return DecideWith(DefaultPolicy, host6, Full, 2, needs, flagged, head.Owner, Need{CPU: 1.8, Mem: gb})
	}
	p.Acquire("a", Test)
	if held, _ := p.Acquire("b", Test); held {
		t.Fatal("b fits beside the flagged thread")
	}
	if s := p.Snapshot(); !strings.Contains(s.Why, "undeclared") {
		t.Fatalf("why %q", s.Why)
	}
	flagged = nil // dismissed or not, the counting went with the episode
	p.Fill()
	if len(granted) != 1 || granted[0] != "b" {
		t.Fatalf("granted %v", granted)
	}
}

func TestB9TheSnapshotListsThemAndTheUsedPercentagesIncludeThem(t *testing.T) {
	p := New(2)
	p.Host = func() Signals { return host6 }
	p.Cost = func(h Holder) Need { return Need{CPU: 1.2, Mem: gb} } // 20 % CPU, 7.7 % memory
	since := time.Date(2026, 11, 17, 14, 0, 0, 0, time.UTC)
	told := since.Add(time.Minute)
	p.Undeclared = func() []Undeclared {
		return []Undeclared{{Thread: "t_a", CPU: 3, Mem: 5 * gb, Since: since, Command: "yarn pre-push:check", Told: &told, Dismissed: true, OverCPU: true, OverMem: true}}
	}
	p.Acquire("t_job", Test)
	s := p.Snapshot()
	if len(s.Undeclared) != 1 {
		t.Fatalf("%+v", s.Undeclared)
	}
	u := s.Undeclared[0]
	if u.Thread != "t_a" || u.CPUPct != 50 || u.MemPct != 38 || u.Command != "yarn pre-push:check" || u.Told == nil || !u.Dismissed {
		t.Fatalf("%+v", u)
	}
	if s.UndeclaredCPUPct == nil || *s.UndeclaredCPUPct != 50 || *s.UndeclaredMemPct != 38 {
		t.Fatalf("shares %v %v", s.UndeclaredCPUPct, s.UndeclaredMemPct)
	}
	if *s.CPUPct != 70 || *s.MemPct != 46 || *s.UsedPct != 70 || s.Tight != ResCPU || *s.CPUFreePct != 30 {
		t.Fatalf("used %v %v %v %q", *s.CPUPct, *s.MemPct, *s.UsedPct, s.Tight)
	}
	// It is not one of the held jobs.
	if len(s.Held) != 1 {
		t.Fatal("held")
	}
}

func TestB10AHostWithoutAProviderListsNothingAndCountsJobs(t *testing.T) {
	p := New(2)
	p.Host = func() Signals { return Signals{} }
	p.Cost = func(Holder) Need { return Need{} }
	p.Undeclared = func() []Undeclared { return []Undeclared{{Thread: "t_a", CPU: 3}} }
	p.Acquire("a", Test)
	s := p.Snapshot()
	if len(s.Undeclared) != 0 || s.UndeclaredCPUPct != nil || *s.UsedPct != 50 {
		t.Fatalf("%+v", s)
	}
}
