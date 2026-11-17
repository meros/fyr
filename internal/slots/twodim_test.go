// V0.163 (the owner's addition): CPU and memory are admitted as two resources, so a CPU-heavy job runs next to a
// memory-heavy one, and the wait names the resource that binds.
package slots

import (
	"strings"
	"testing"
)

const gb = 1 << 30

// A host that gives its agents 6 cores and 13 GB, with 60 % of that memory really free.
var host6 = Signals{Provider: "cgroups", Slowdown: f(1.1), Headroom: f(0.6), Stall: StallOK, CPU: 6, Memory: 13 * gb}

func TestV163CPUHeavyAndMemoryHeavyCoexist(t *testing.T) {
	cpuHeavy := Need{CPU: 3, Mem: 2 * gb}   // make test-go: about 3 cores, 2 GB
	memHeavy := Need{CPU: 0.6, Mem: 6 * gb} // a data build: little CPU, much memory
	if ok, why, _ := Decide(DefaultPolicy, host6, Full, 2, []Need{cpuHeavy}, memHeavy); !ok {
		t.Fatalf("a memory-heavy job beside a CPU-heavy one was held: %q", why)
	}
	// Two memory-heavy jobs: the second needs 6 GB, 13 − 6 = 7 GB of budget but only (0.6 − 0.1) × 13 = 6.5 GB
	// of real headroom above the reserve... it fits; a third does not.
	ok, why, res := Decide(DefaultPolicy, host6, Full, 2, []Need{memHeavy, memHeavy}, memHeavy)
	if ok || res != ResMem || !strings.Contains(why, "needs ≈ 6.4 GB memory") {
		t.Fatalf("three memory-heavy jobs: %v %q %q", ok, why, res)
	}
	// Two CPU-heavy jobs fill 6 cores; a third waits on CPU.
	ok, why, res = Decide(DefaultPolicy, host6, Full, 2, []Need{cpuHeavy, cpuHeavy}, cpuHeavy)
	if ok || res != ResCPU || why != "needs ≈ 50 % CPU, 0 % free" {
		t.Fatalf("three CPU-heavy jobs: %v %q %q", ok, why, res)
	}
}

func TestV163TwoMemoryHeavyJobsDoNotFitTheRealHeadroom(t *testing.T) {
	tight := host6
	tight.Headroom = f(0.4) // 5.2 GB really free; 1.3 GB of it is the reserve
	memHeavy := Need{CPU: 0.6, Mem: 6 * gb}
	ok, why, res := Decide(DefaultPolicy, tight, Full, 2, []Need{memHeavy}, memHeavy)
	if ok || res != ResMem || !strings.Contains(why, "memory,") {
		t.Fatalf("%v %q %q", ok, why, res)
	}
}

func TestV163BudgetFactorsPerResource(t *testing.T) {
	cpuHeavy := Need{CPU: 3, Mem: 2 * gb}
	// The CPU budget cut to 75 %: 4.5 cores; two CPU-heavy jobs no longer fit.
	if ok, _, res := Decide(DefaultPolicy, host6, Factors{CPU: 0.75, Mem: 1}, 2, []Need{cpuHeavy}, cpuHeavy); ok || res != ResCPU {
		t.Fatal("the CPU factor did not bind")
	}
	// A cut memory budget does not hold a CPU job that needs little memory.
	if ok, why, _ := Decide(DefaultPolicy, host6, Factors{CPU: 1, Mem: 0.5}, 2, []Need{cpuHeavy}, Need{CPU: 1, Mem: gb / 2}); !ok {
		t.Fatalf("%q", why)
	}
}

func TestV163NeedOfCarriesTheLearnedMeasures(t *testing.T) {
	// slot-costs keeps demand (cores) and peak (bytes) per class: they are the two needs as they are.
	n := NeedOf(3, 2*gb, 5, host6, 2)
	if n.CPU != 3 || n.Mem != 2*gb {
		t.Fatalf("%+v", n)
	}
	if n := NeedOf(0.2, gb/10, 1, host6, 2); n.CPU != 3 || n.Mem != 13*gb/2 {
		t.Fatalf("under three runs, the prior (half the host): %+v", n)
	}
	if !Cheap(Need{CPU: 0.5, Mem: gb}, host6) || Cheap(Need{CPU: 0.5, Mem: 3 * gb}, host6) {
		t.Fatal("cheap is both resources under 12.5 %")
	}
}
