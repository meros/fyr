// V0.admission-tidy: a holder's own undeclared flag is not counted beside its
// held need, a quiet heavy call that is still open is not an idle hold, and a
// class's need can be asked by name for the gate's heavier-command check.
package guard

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
)

// V0.admission-tidy (6): a thread just admitted keeps its undeclared flag until
// the detector's next reading; its held need stands for that use.
func TestAdmissionTidyAHoldersFlagIsNotCountedTwice(t *testing.T) {
	a, h, j := setup(2)
	head := resource.Holder{Owner: "head", Kind: resource.Test}
	held := []resource.Holder{{Owner: "a", Kind: resource.Test}}
	j.live["a"] = use{cores: 4, grew: 8 * gb}
	h.und = []resource.Undeclared{{Thread: "a", Name: "proj", CPU: 2, Mem: 4 * gb}}
	if ok, why, _ := a.Admit(head, held); !ok {
		t.Fatalf("the holder's flag counted beside its hold: %s", why)
	}
	// Another thread's flag still counts.
	h.und = []resource.Undeclared{{Thread: "other", Name: "proj", CPU: 2, Mem: 4 * gb}}
	if ok, _, _ := a.Admit(head, held); ok {
		t.Fatal("admitted past another thread's undeclared work")
	}
}

// V0.admission-tidy (4): a heavy call that is still open (the sweep keeps its
// hold up to HeavyCallStale) counts its prediction however quiet it is.
func TestAdmissionTidyAnOpenCallIsNotIdle(t *testing.T) {
	a, _, j := setup(2)
	j.active["quiet"] = now.Add(-5 * time.Minute)
	j.live["quiet"] = use{cores: 0, grew: 1 * gb}
	hd := resource.Holder{Owner: "quiet", Kind: resource.Test}
	if c := a.Cost(hd); c.CPU != IdleShare*8 {
		t.Fatalf("setup: not idle without an open call: %+v", c)
	}
	a.Open = func(th string) bool { return th == "quiet" }
	if c := a.Cost(hd); c != (resource.Need{CPU: 4, Mem: 8 * gb}) {
		t.Fatalf("an open call counted idle: %+v", c)
	}
}

// V0.admission-tidy (1): the need of a class by name, for the gate.
func TestAdmissionTidyClassNeed(t *testing.T) {
	a, _, j := setup(2)
	j.learned["t1/go test"] = resource.Learned{Demand: 1, Peak: 1 * gb, Runs: 5}
	if n := a.ClassNeed("t1", "go test"); n != (resource.Need{CPU: 1, Mem: 1 * gb}) {
		t.Fatalf("learned: %+v", n)
	}
	if n := a.ClassNeed("t1", "yarn e2e"); n != (resource.Need{CPU: 4, Mem: 8 * gb}) {
		t.Fatalf("unlearned is the prior: %+v", n)
	}
}
