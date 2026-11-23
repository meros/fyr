// V0.admission-memory (decision 0029 section 2): a job sizes itself as admission counts it. Under three runs a
// class's need is never below the prior, so a first run its memory.high held back cannot teach a low peak that
// the next limit ratchets down; and an overrun of a class's first run raises the peak that run teaches.
package cgroup

import (
	"testing"

	"github.com/meros/fyr/internal/guard/resource"
)

func TestAdmissionMemorySizeOfSaysTheRuns(t *testing.T) {
	const gb = 1 << 30
	// guard.SizeFor sizes a class under three runs with the prior
	// (guard/joblimit_test.go); the job tracker says the runs.
	j := &Jobs{}
	j.mu.Lock()
	j.load()
	j.table["fyr: make test-go"] = resource.Learned{Demand: 0.5, Peak: gb, Runs: 1}
	j.projects = map[string]string{"t_a": "fyr"}
	j.mu.Unlock()
	j.Saw("t_a", "make test-go")
	if z := j.SizeOf("t_a", false); z.Runs != 1 || !z.Known {
		t.Fatalf("SizeOf does not say the runs: %+v", z)
	}
}

func TestAdmissionMemoryAFirstRunsOverrunIsKept(t *testing.T) {
	j := &Jobs{}
	j.RaiseOverrun("fyr: make test") // its first run hit its memory.high
	j.mu.Lock()
	j.load()
	j.learn(JobRecord{Class: "fyr: make test", Demand: 1, Peak: 1000})
	j.mu.Unlock()
	if l, _ := j.Learned("fyr: make test"); l.Peak != 1200 {
		t.Fatalf("the first run's overrun was lost: peak %d", l.Peak)
	}
	j.mu.Lock()
	j.learn(JobRecord{Class: "fyr: make test", Demand: 1, Peak: 1000})
	j.mu.Unlock()
	if l, _ := j.Learned("fyr: make test"); l.Peak != 1080 {
		t.Fatalf("the raise applied twice: peak %d", l.Peak)
	}
}
