// V0.164 sections 6 and 12.4 (B1–B10): a flagged thread's use is one more Need in admission, beside the admitted jobs
// and not among them; a thread's own undeclared use does not count against its own request.
package slots

import (
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
)

func extra(name string, cpu float64, mem uint64) resource.Extra {
	return resource.Extra{Owner: name, Name: name, Need: resource.Need{CPU: cpu, Mem: mem}}
}

func TestB1AFlaggedThreadsCPUMakesTheHeadWait(t *testing.T) {
	held := []resource.Need{{CPU: 2.4, Mem: gb}}          // 40 % of 6 cores
	flagged := []resource.Extra{extra("Map perf", 3, gb)} // 50 %
	head := resource.Need{CPU: 1.8, Mem: gb}              // 30 %
	ok, why, res := guard.DecideWith(resource.DefaultPolicy, host6, resource.Full, 2, held, flagged, "t_head", head)
	if ok || res != resource.ResCPU || why != "needs ≈ 30 % CPU, 10 % free (Map perf uses 50 % undeclared)" {
		t.Fatalf("%v %q %q", ok, why, res)
	}
}

func TestB2WithoutTheFlaggedThreadItIsAdmitted(t *testing.T) {
	if ok, why, _ := guard.DecideWith(resource.DefaultPolicy, host6, resource.Full, 2, []resource.Need{{CPU: 2.4, Mem: gb}}, nil, "t_head", resource.Need{CPU: 1.8, Mem: gb}); !ok {
		t.Fatal(why)
	}
	// guard.Decide is guard.DecideWith without extras.
	if ok, _, _ := guard.Decide(resource.DefaultPolicy, host6, resource.Full, 2, []resource.Need{{CPU: 2.4, Mem: gb}}, resource.Need{CPU: 1.8, Mem: gb}); !ok {
		t.Fatal("guard.Decide changed")
	}
}

func TestB3MemoryOfAFlaggedThreadBinds(t *testing.T) {
	held := []resource.Need{{CPU: 0.5, Mem: 4 * gb}}
	flagged := []resource.Extra{extra("Map perf", 0.2, 5*gb)}
	ok, why, res := guard.DecideWith(resource.DefaultPolicy, host6, resource.Full, 2, held, flagged, "t_head", resource.Need{CPU: 0.5, Mem: 6 * gb})
	if ok || res != resource.ResMem || !strings.Contains(why, "needs ≈ 6.4 GB memory") || !strings.Contains(why, "Map perf uses") || !strings.Contains(why, "undeclared") {
		t.Fatalf("%v %q %q", ok, why, res)
	}
}

func TestB4TheDimensionsAreApart(t *testing.T) {
	held := []resource.Need{{CPU: 0.1, Mem: gb / 2}}
	flagged := []resource.Extra{extra("Map perf", 5, gb)} // CPU-heavy
	if ok, why, _ := guard.DecideWith(resource.DefaultPolicy, host6, resource.Full, 2, held, flagged, "t_head", resource.Need{CPU: 0.6, Mem: 6 * gb}); !ok {
		t.Fatalf("a memory-heavy head beside a CPU-heavy flagged thread: %q", why)
	}
}

func TestB5OneJobThatAskedAlwaysRuns(t *testing.T) {
	flagged := []resource.Extra{extra("Map perf", 5.4, gb)} // 90 %
	if ok, why, _ := guard.DecideWith(resource.DefaultPolicy, host6, resource.Full, 2, nil, flagged, "t_head", resource.Need{CPU: 3, Mem: gb}); !ok {
		t.Fatalf("nothing admitted, so the head starts: %q", why)
	}
	// The memory rules can still hold it.
	low := host6
	low.Headroom = f(0.05)
	if ok, _, res := guard.DecideWith(resource.DefaultPolicy, low, resource.Full, 2, nil, flagged, "t_head", resource.Need{CPU: 3, Mem: gb}); ok || res != resource.ResHeadroom {
		t.Fatal("the memory reserve did not hold")
	}
}

func TestB6AThreadsOwnUseDoesNotCountAgainstItsOwnRequest(t *testing.T) {
	held := []resource.Need{{CPU: 1, Mem: gb}}
	flagged := []resource.Extra{extra("t_me", 3, gb)} // 50 %, its own
	flagged[0].Owner = "t_me"
	if ok, why, _ := guard.DecideWith(resource.DefaultPolicy, host6, resource.Full, 2, held, flagged, "t_me", resource.Need{CPU: 3, Mem: gb}); !ok {
		t.Fatalf("its own use was counted: %q", why)
	}
	if ok, _, _ := guard.DecideWith(resource.DefaultPolicy, host6, resource.Full, 2, held, flagged, "t_other", resource.Need{CPU: 3, Mem: gb}); ok {
		t.Fatal("another thread's request ignored the flagged use")
	}
}

func TestB7B8TheEpisodeEndsAndTheQueueMovesAtOnce(t *testing.T) {
	flagged := []resource.Extra{extra("Map perf", 3, gb)}
	p := New(2)
	p.Host = func() resource.Signals { return host6 }
	p.Cost = func(h resource.Holder) resource.Need { return resource.Need{CPU: 2.4, Mem: gb} }
	var granted []string
	p.OnGrant = func(o, k string) { granted = append(granted, o) }
	p.Admit = func(head resource.Holder, held []resource.Holder) (bool, string, string) {
		var needs []resource.Need
		for range held {
			needs = append(needs, resource.Need{CPU: 2.4, Mem: gb})
		}
		return guard.DecideWith(resource.DefaultPolicy, host6, resource.Full, 2, needs, flagged, head.Owner, resource.Need{CPU: 1.8, Mem: gb})
	}
	p.Acquire("a", resource.Test)
	if held, _ := p.Acquire("b", resource.Test); held {
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
	p.Host = func() resource.Signals { return host6 }
	p.Cost = func(h resource.Holder) resource.Need { return resource.Need{CPU: 1.2, Mem: gb} } // 20 % CPU, 7.7 % memory
	since := time.Date(2026, 11, 17, 14, 0, 0, 0, time.UTC)
	p.Undeclared = func() []resource.Undeclared {
		return []resource.Undeclared{{Thread: "t_a", CPU: 3, Mem: 5 * gb, Since: since, Command: "yarn pre-push:check", OverCPU: true, OverMem: true}}
	}
	p.Acquire("t_job", resource.Test)
	s := p.Snapshot()
	if len(s.Undeclared) != 1 {
		t.Fatalf("%+v", s.Undeclared)
	}
	u := s.Undeclared[0]
	if u.Thread != "t_a" || u.CPUPct != 50 || u.MemPct != 38 || u.Command != "yarn pre-push:check" {
		t.Fatalf("%+v", u)
	}
	if s.UndeclaredCPUPct == nil || *s.UndeclaredCPUPct != 50 || *s.UndeclaredMemPct != 38 {
		t.Fatalf("shares %v %v", s.UndeclaredCPUPct, s.UndeclaredMemPct)
	}
	if *s.CPUPct != 70 || *s.MemPct != 46 || *s.UsedPct != 70 || s.Tight != resource.ResCPU || *s.CPUFreePct != 30 {
		t.Fatalf("used %v %v %v %q", *s.CPUPct, *s.MemPct, *s.UsedPct, s.Tight)
	}
	// It is not one of the held jobs.
	if len(s.Held) != 1 {
		t.Fatal("held")
	}
}

func TestB10AHostWithoutAProviderListsNothingAndCountsJobs(t *testing.T) {
	p := New(2)
	p.Host = func() resource.Signals { return resource.Signals{} }
	p.Cost = func(resource.Holder) resource.Need { return resource.Need{} }
	p.Undeclared = func() []resource.Undeclared { return []resource.Undeclared{{Thread: "t_a", CPU: 3}} }
	p.Acquire("a", resource.Test)
	s := p.Snapshot()
	if len(s.Undeclared) != 0 || s.UndeclaredCPUPct != nil || *s.UsedPct != 50 {
		t.Fatalf("%+v", s)
	}
}
