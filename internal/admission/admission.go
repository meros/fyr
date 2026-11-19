// Package admission is a host's slot admission policy (decision 0029 S2–S4,
// V0.163, V0.164): what a heavy job costs, whether the queue's head may start
// beside the jobs that hold slots, whether a later waiter may pass it (EASY
// backfill), and whether a holder is heavier than predicted. The rules
// themselves are slots' (DecideWith, NeedOf, EASY); this package feeds them
// the host's measures and the job tracker's learned classes. It was fyrd's
// main.go until V0.182.
//
// Locks: Cost, Admit and Backfill run under the pool's lock (slots.Pool
// calls them so); they call Host and Jobs, which take their own locks and
// never the pool's or the engine's. Nothing here takes a lock of its own.
package admission

import (
	"time"

	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/slots"
)

// Host is the host's measures: *cgroup.Sampler.
type Host interface {
	Signals() slots.Signals
	// Undeclared lists the threads that run heavy work Fyr did not admit
	// (V0.164), from a published view.
	Undeclared() []slots.Undeclared
}

// Jobs is the job tracker: *cgroup.Jobs.
type Jobs interface {
	Learned(class string) (cgroup.Learned, bool)
	ClassOf(thread, kind string) string
	LiveUse(thread string) (cores float64, grew uint64, ok bool)
	Active(thread string) (time.Time, bool)
}

// Admission is the policy of one pool. N is the host's `slots` setting (the
// prior is 1/N of the host); Now is time.Now when nil.
type Admission struct {
	Host   Host
	Jobs   Jobs
	Policy slots.Policy
	N      int
	Pool   *slots.Pool
	Now    func() time.Time
}

// New is the policy for pool, with n the host's `slots` setting as
// configured and the host's thresholds over the defaults (a value of 0 keeps
// the default).
func New(pool *slots.Pool, n int, host Host, jobs Jobs, slowdownMax, memoryReserve float64) *Admission {
	p := slots.DefaultPolicy
	if slowdownMax > 0 {
		p.SlowdownMax = slowdownMax
	}
	if memoryReserve > 0 {
		p.MemoryReserve = memoryReserve
	}
	return &Admission{Host: host, Jobs: jobs, Policy: p, N: n, Pool: pool}
}

// Install makes a the pool's policy: its measures, costs, admission and
// backfill.
func (a *Admission) Install() {
	a.Pool.Host = a.Host.Signals
	a.Pool.Cost = a.Cost
	a.Pool.Admit = a.Admit
	a.Pool.Backfill = a.Backfill
}

func (a *Admission) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// Predict is a job's need and its class's p90 wall time in seconds
// (decision 0029 S3, V0.163): CPU and memory apart, the class's learned
// demand (cores) and peak (bytes); the prior (1/N of the host) and no wall
// time until the class is learned.
func (a *Admission) Predict(h slots.Holder) (need slots.Need, wall float64) {
	sig := a.Host.Signals()
	l, ok := a.Jobs.Learned(a.Jobs.ClassOf(h.Owner, h.Kind))
	if !ok {
		return slots.Prior(sig, a.N), 0
	}
	return slots.NeedOf(l.Demand, l.Peak, l.Runs, sig, a.N), l.WallP90()
}

// Live is what a holder uses now: its cores and how much its memory grew
// since the grant; false without a reading.
func (a *Admission) Live(h slots.Holder) (slots.Need, bool) {
	cores, grew, ok := a.Jobs.LiveUse(h.Owner)
	return slots.Need{CPU: cores, Mem: grew}, ok
}

// Cost is a holder's need for the budget: the prior for a priority hold (the
// deploy's tests); for an idle hold (no heavy command for slots.IdleAfter) a
// little CPU and the memory it holds; else the prediction, at least what it
// uses now.
func (a *Admission) Cost(h slots.Holder) slots.Need {
	need, _ := a.cost(h)
	return need
}

// cost is Cost and what the holder may still grow: its need's memory less
// what it grew since the grant (V0.admission-memory); all of it without a
// reading yet, nothing for an idle hold (its need is what it holds).
func (a *Admission) cost(h slots.Holder) (need slots.Need, grow uint64) {
	sig := a.Host.Signals()
	l, isLive := a.Live(h)
	switch {
	case h.Priority:
		need = slots.Prior(sig, a.N)
	case a.idle(h):
		return slots.Need{CPU: slots.IdleShare * sig.CPU, Mem: l.Mem}, 0
	default:
		need, _ = a.Predict(h)
		if isLive {
			need = need.Max(l)
		}
	}
	if need.Mem > l.Mem {
		grow = need.Mem - l.Mem
	}
	return need, grow
}

func (a *Admission) idle(h slots.Holder) bool {
	at, ok := a.Jobs.Active(h.Owner)
	return ok && a.now().Sub(at) >= slots.IdleAfter
}

// heldNeeds is the holders' needs, and sig with their growth still to come
// (V0.admission-memory: decision 0029 section 3 rule 3 takes it off the
// headroom; V0.163 had dropped it). One pass over the holders, under the
// pool's lock like Cost.
func (a *Admission) heldNeeds(held []slots.Holder) ([]slots.Need, slots.Signals) {
	sig := a.Host.Signals()
	out := make([]slots.Need, len(held))
	for i, h := range held {
		var g uint64
		out[i], g = a.cost(h)
		sig.Growth += g
	}
	return out, sig
}

// extras is the use of the threads that run heavy work Fyr did not admit
// (V0.164); it counts beside the admitted jobs, except the head's own (its
// request stands in for it, which DecideWith handles).
func (a *Admission) extras() []slots.Extra {
	var out []slots.Extra
	for _, u := range a.Host.Undeclared() {
		out = append(out, slots.Extra{Owner: u.Thread, Name: u.Name, Need: slots.Need{CPU: u.CPU, Mem: u.Mem}})
	}
	// V0.services-not-jobs: a running service's memory is reserved from its
	// learned peak while it runs.
	if sv, ok := a.Host.(interface{ ServiceExtras() []slots.Extra }); ok {
		out = append(out, sv.ServiceExtras()...)
	}
	return out
}

// Admit decides whether head may start beside held (decision 0029 S2): a
// budget of costs, gated by the slowdown and memory; one job always runs.
// It returns why not and the binding resource.
func (a *Admission) Admit(head slots.Holder, held []slots.Holder) (bool, string, string) {
	need, _ := a.Predict(head)
	needs, sig := a.heldNeeds(held)
	return slots.DecideWith(a.Policy, sig, a.Pool.Budget(), a.N, needs, a.extras(), head.Owner, need)
}

// Backfill is EASY backfill: c may pass the head when it fits now and is
// cheap, or ends (its class's p90) before the earliest running job is
// expected to.
func (a *Admission) Backfill(head, c slots.Holder, held []slots.Holder) bool {
	need, wall := a.Predict(c)
	needs, sig := a.heldNeeds(held)
	if ok, _, _ := slots.DecideWith(a.Policy, sig, a.Pool.Budget(), a.N, needs, a.extras(), c.Owner, need); !ok {
		return false
	}
	ends := make([]time.Time, len(held))
	for i, h := range held {
		if _, w := a.Predict(h); w > 0 {
			ends[i] = h.Since.Add(time.Duration(w * float64(time.Second)))
		}
	}
	return slots.EASY(a.now(), slots.Cheap(need, sig), wall, ends)
}

// Over says whether owner's job is heavier than predicted, in either
// resource, by more than an eighth of the host (V0.163), with its class and
// its live and predicted use in percent of the host.
func (a *Admission) Over(owner string) (class string, live, predicted float64, over bool) {
	h, ok := a.Pool.Holding(owner)
	if !ok {
		return "", 0, 0, false
	}
	sig := a.Host.Signals()
	p, _ := a.Predict(h)
	l, ok := a.Live(h)
	pc, pm := p.Shares(sig)
	lc, lm := l.Shares(sig)
	over = ok && (lc > pc+slots.CheapShare || lm > pm+slots.CheapShare)
	return a.Jobs.ClassOf(owner, h.Kind), slots.Pct(max(lc, lm)), slots.Pct(max(pc, pm)), over
}

// Thrashing: the host thrashes, so agents wait (V0.153).
func (a *Admission) Thrashing() (bool, string) {
	return slots.Thrashing(a.Host.Signals(), a.Policy)
}
