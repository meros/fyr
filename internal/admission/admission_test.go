// SPEC 0.5 V0.182: the slot admission policy, moved out of fyrd's main.go,
// keeps decision 0029's rules (S2 admission, S3 learned costs and EASY
// backfill, V0.163 two resources, V0.164 undeclared work).
package admission

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/slots"
)

const gb = 1 << 30

type fakeHost struct {
	sig slots.Signals
	und []slots.Undeclared
}

func (h *fakeHost) Signals() slots.Signals         { return h.sig }
func (h *fakeHost) Undeclared() []slots.Undeclared { return h.und }

type use struct {
	cores float64
	grew  uint64
}

type fakeJobs struct {
	learned map[string]cgroup.Learned // by class
	live    map[string]use            // by owner
	active  map[string]time.Time      // by owner
}

func (j *fakeJobs) Learned(c string) (cgroup.Learned, bool) { l, ok := j.learned[c]; return l, ok }
func (j *fakeJobs) ClassOf(thread, kind string) string      { return thread + "/" + kind }
func (j *fakeJobs) LiveUse(t string) (float64, uint64, bool) {
	u, ok := j.live[t]
	return u.cores, u.grew, ok
}
func (j *fakeJobs) Active(t string) (time.Time, bool) { at, ok := j.active[t]; return at, ok }

var now = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

// eight cores and 16 GB for the agents, no slowdown, plenty of memory.
func setup(n int) (*Admission, *fakeHost, *fakeJobs) {
	s, head := 1.0, 0.9
	h := &fakeHost{sig: slots.Signals{Provider: "cgroup", Slowdown: &s, Headroom: &head, Stall: slots.StallOK, CPU: 8, Memory: 16 * gb}}
	j := &fakeJobs{learned: map[string]cgroup.Learned{}, live: map[string]use{}, active: map[string]time.Time{}}
	pool := slots.New(n)
	a := New(pool, n, h, j, 0, 0)
	a.Now = func() time.Time { return now }
	return a, h, j
}

func TestV182NewKeepsTheDefaultsUnlessSet(t *testing.T) {
	a, h, j := setup(2)
	if a.Policy != slots.DefaultPolicy {
		t.Fatalf("%+v", a.Policy)
	}
	b := New(slots.New(2), 2, h, j, 2.5, 0.2)
	if b.Policy.SlowdownMax != 2.5 || b.Policy.MemoryReserve != 0.2 {
		t.Fatalf("%+v", b.Policy)
	}
}

func TestV182InstallSetsThePoolsPolicy(t *testing.T) {
	a, _, _ := setup(2)
	a.Install()
	if a.Pool.Host == nil || a.Pool.Cost == nil || a.Pool.Admit == nil || a.Pool.Backfill == nil {
		t.Fatal("the pool is not wired")
	}
}

func TestV182PredictIsThePriorUntilLearned(t *testing.T) {
	a, _, j := setup(2)
	h := slots.Holder{Owner: "t1", Kind: slots.Test}
	need, wall := a.Predict(h)
	if need != (slots.Need{CPU: 4, Mem: 8 * gb}) || wall != 0 {
		t.Fatalf("prior: %+v %v", need, wall)
	}
	j.learned["t1/test"] = cgroup.Learned{Demand: 6, Peak: 12 * gb, Runs: slots.LearnRuns, Walls: []float64{60, 120}}
	need, wall = a.Predict(h)
	if need != (slots.Need{CPU: 6, Mem: 12 * gb}) || wall != 120 {
		t.Fatalf("learned: %+v %v", need, wall)
	}
}

func TestV182CostOfPriorityIdleAndLiveHolds(t *testing.T) {
	a, _, j := setup(2)
	// The deploy's tests count the prior, whatever they use.
	j.live["deploy"] = use{cores: 7, grew: 15 * gb}
	if c := a.Cost(slots.Holder{Owner: "deploy", Priority: true}); c != (slots.Need{CPU: 4, Mem: 8 * gb}) {
		t.Fatalf("priority: %+v", c)
	}
	// An idle hold: a little CPU and the memory it holds now.
	j.active["idle"] = now.Add(-slots.IdleAfter)
	j.live["idle"] = use{cores: 3, grew: 2 * gb}
	if c := a.Cost(slots.Holder{Owner: "idle", Kind: slots.Test}); c != (slots.Need{CPU: slots.IdleShare * 8, Mem: 2 * gb}) {
		t.Fatalf("idle: %+v", c)
	}
	// A busy hold: at least what it uses now.
	j.active["busy"] = now.Add(-time.Minute)
	j.live["busy"] = use{cores: 7, grew: 1 * gb}
	if c := a.Cost(slots.Holder{Owner: "busy", Kind: slots.Test}); c != (slots.Need{CPU: 7, Mem: 8 * gb}) {
		t.Fatalf("busy: %+v", c)
	}
}

func TestV182AdmitCountsHoldersAndUndeclaredWork(t *testing.T) {
	a, h, j := setup(2)
	head := slots.Holder{Owner: "head", Kind: slots.Test}
	if ok, why, _ := a.Admit(head, nil); !ok {
		t.Fatalf("an empty host refuses: %s", why)
	}
	held := []slots.Holder{{Owner: "a", Kind: slots.Test}}
	j.live["a"] = use{cores: 4, grew: 8 * gb} // a has grown to its prior: nothing more to come (V0.admission-memory)
	if ok, why, _ := a.Admit(head, held); !ok {
		t.Fatalf("half and half refuses: %s", why)
	}
	// Undeclared work by another thread takes the room the head needed.
	h.und = []slots.Undeclared{{Thread: "other", Name: "proj", CPU: 4, Mem: 8 * gb}}
	if ok, _, binding := a.Admit(head, held); ok || binding == "" {
		t.Fatalf("admitted past undeclared work (binding %q)", binding)
	}
	// The head's own undeclared work is not counted twice.
	h.und = []slots.Undeclared{{Thread: "head", CPU: 4, Mem: 8 * gb}}
	if ok, why, _ := a.Admit(head, held); !ok {
		t.Fatalf("the head's own work counted: %s", why)
	}
}

func TestV182AdmitWaitsOnASlowHost(t *testing.T) {
	a, h, _ := setup(2)
	s := 2.0
	h.sig.Slowdown = &s
	if ok, _, _ := a.Admit(slots.Holder{Owner: "head", Kind: slots.Test}, []slots.Holder{{Owner: "a", Kind: slots.Test}}); ok {
		t.Fatal("admitted at 2× slowdown")
	}
	if th, _ := a.Thrashing(); th {
		t.Fatal("a slow host is not thrashing")
	}
}

func TestV182BackfillPassesAJobThatEndsFirst(t *testing.T) {
	a, _, j := setup(4)
	// One job holds three quarters of the host and ends in an hour.
	j.learned["a/build"] = cgroup.Learned{Demand: 6, Peak: 12 * gb, Runs: slots.LearnRuns, Walls: []float64{3600}}
	held := []slots.Holder{{Owner: "a", Kind: slots.Build, Since: now}}
	j.live["a"] = use{cores: 6, grew: 12 * gb} // at its peak: nothing more to come (V0.admission-memory)
	head := slots.Holder{Owner: "head", Kind: slots.Build}
	j.learned["head/build"] = cgroup.Learned{Demand: 6, Peak: 12 * gb, Runs: slots.LearnRuns, Walls: []float64{3600}}
	// c fits beside a, is not cheap, and ends in ten minutes: it passes.
	c := slots.Holder{Owner: "c", Kind: slots.Test}
	j.learned["c/test"] = cgroup.Learned{Demand: 2, Peak: 3 * gb, Runs: slots.LearnRuns, Walls: []float64{600}}
	if !a.Backfill(head, c, held) {
		t.Fatal("a short job that fits did not pass")
	}
	// Ending after a's expected end, it waits.
	j.learned["c/test"] = cgroup.Learned{Demand: 2, Peak: 3 * gb, Runs: slots.LearnRuns, Walls: []float64{7200}}
	if a.Backfill(head, c, held) {
		t.Fatal("a long job passed the head")
	}
	// One that does not fit never passes.
	j.learned["c/test"] = cgroup.Learned{Demand: 4, Peak: 8 * gb, Runs: slots.LearnRuns, Walls: []float64{10}}
	if a.Backfill(head, c, held) {
		t.Fatal("a job that does not fit passed")
	}
}

func TestV182OverWhenHeavierThanPredictedByAnEighth(t *testing.T) {
	a, _, j := setup(2)
	if _, _, _, over := a.Over("nobody"); over {
		t.Fatal("over without a hold")
	}
	a.Pool.Acquire("t1", slots.Test)
	j.learned["t1/test"] = cgroup.Learned{Demand: 2, Peak: 2 * gb, Runs: slots.LearnRuns}
	j.live["t1"] = use{cores: 3, grew: 2 * gb} // 37.5 % against 25 %: within an eighth
	if class, live, pred, over := a.Over("t1"); over || class != "t1/test" || live != 38 || pred != 25 {
		t.Fatalf("%s %v %v %v", class, live, pred, over)
	}
	j.live["t1"] = use{cores: 4.5, grew: 2 * gb} // 56 % against 25 %
	if _, live, _, over := a.Over("t1"); !over || live != 56 {
		t.Fatalf("%v %v", live, over)
	}
}

type serviceHost struct {
	*fakeHost
	svc []slots.Extra
}

func (h serviceHost) ServiceExtras() []slots.Extra { return h.svc }

// V0.services-not-jobs: a running service's reservation counts beside the jobs, so a batch job waits for it.
func TestServicesAreReservedInAdmission(t *testing.T) {
	a, h, _ := setup(4)
	sh := serviceHost{fakeHost: h, svc: []slots.Extra{{Owner: "t9", Name: "Shop", Need: slots.Need{Mem: 10 * gb}, Service: true}}}
	held := []slots.Holder{{Owner: "t1", Kind: slots.Test}} // the prior: a quarter of the host each
	head := slots.Holder{Owner: "t2", Kind: slots.Build}
	if ok, why, _ := a.Admit(head, held); !ok { // 12 GB of 16 free: the head fits
		t.Fatalf("without the service: %q", why)
	}
	a.Host = sh
	ok, why, _ := a.Admit(head, held)
	if ok || why == "" {
		t.Fatalf("admitted beside a 10 GB service: %v %q", ok, why)
	}
}

// V0.admission-memory (decision 0029 section 3 rule 3): what the running jobs may still grow comes off the
// headroom. A job admitted at the start of its run uses little yet; the next admission must not take its room.
func TestAdmissionMemoryRunningJobsGrowthComesOffTheHeadroom(t *testing.T) {
	a, _, j := setup(2)
	// 16 GB, 90 % free: 12.8 GB above the reserve. Each job peaks at 7 GB; t1 has grown 0.5 GB of it so far.
	j.learned["t1/test"] = cgroup.Learned{Demand: 1, Peak: 7 * gb, Runs: slots.LearnRuns}
	j.learned["t2/test"] = cgroup.Learned{Demand: 1, Peak: 7 * gb, Runs: slots.LearnRuns}
	j.live["t1"] = use{cores: 1, grew: gb / 2}
	held := []slots.Holder{{Owner: "t1", Kind: slots.Test, Since: now}}
	head := slots.Holder{Owner: "t2", Kind: slots.Test}
	// Each fits the headroom now (7 GB ≤ 12.8 GB) and the budget (7 + 7 ≤ 16), but t1's 6.5 GB still to come
	// leaves 6.3 GB: t2 waits on the host's memory.
	ok, why, binding := a.Admit(head, held)
	if ok || binding != slots.ResHeadroom || slots.Wire(binding) != slots.ResMem {
		t.Fatalf("admitted beside a job that will grow 6.5 GB: %v %q %q", ok, why, binding)
	}
	if a.Backfill(holderOf("t0"), head, held) {
		t.Fatal("backfilled past the running job's growth")
	}
	// Once t1 has grown to its peak, its growth is in the headroom reading, and nothing more is taken off.
	j.live["t1"] = use{cores: 1, grew: 7 * gb}
	if ok, why, _ := a.Admit(head, held); !ok {
		t.Fatalf("held back after the growth came: %q", why)
	}
}

func holderOf(owner string) slots.Holder { return slots.Holder{Owner: owner, Kind: slots.Test} }
