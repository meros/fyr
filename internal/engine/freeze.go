package engine

import (
	"time"
)

// frozenJob is a job or undeclared work the memory guard froze (decision
// 0029 S5).
type frozenJob struct {
	owner, since string // the hold: owner and its grant
	at           time.Time
	// V0.164: undeclared work frozen instead of a hold; episode is the
	// flag's since.
	undeclared bool
	episode    time.Time
	// Decision 0032 slice 2: the part of the work whose turn it is (0: the
	// work is frozen whole), since when, for how long, and the thread's
	// read-back counter when the turn began.
	active int
	turnAt time.Time
	turn   time.Duration
	faults uint64
	// parts is each frozen part's memory when it was last frozen, by pid
	// (V0.sim-fixes (4)): what comes back if the turns end in a wake.
	parts map[int]uint64
	// V0.oomd-guard (decision 0036 E2): past FreezeMax while memory stalls
	// critically, so it stays frozen; the thread was told once.
	pastMax bool
	// V0.guard-wake (decision 0036 E3, E6): the thread's memory at the
	// freeze, and its cgroup's frozen_usec then (hasUsec: the kernel has it).
	mem     uint64
	usec    uint64
	hasUsec bool
	// V0.rerun-classes: the work's heavy class (a job's, from the gate) and
	// command (undeclared work's), which the owner's rerunnable entries match.
	class, cmd string
}

// frozenJobOf says the guard holds thread's job (not undeclared work) frozen
// now; under mu.
func (e *Engine) frozenJobOf(thread string) bool {
	for _, f := range e.frozen {
		if f.owner == thread && !f.undeclared {
			return true
		}
	}
	return false
}

// isFrozen says the guard holds thread frozen now; under mu.
func (e *Engine) isFrozen(thread string) bool {
	for _, f := range e.frozen {
		if f.owner == thread {
			return true
		}
	}
	return false
}
