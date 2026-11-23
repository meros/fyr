package guard

import "time"

// Frozen is a job or undeclared work the guard holds frozen (decision 0029
// S5), as State keeps it.
type Frozen struct {
	Owner, Since string // the hold: owner and its grant
	At           time.Time
	// V0.164: undeclared work frozen instead of a hold; Episode is the
	// flag's since.
	Undeclared bool
	Episode    time.Time
	// Decision 0032 slice 2: the part of the work whose turn it is (0: the
	// work is frozen whole), since when, for how long, and the thread's
	// read-back counter when the turn began.
	Active int
	TurnAt time.Time
	Turn   time.Duration
	Faults uint64
	// Parts is each frozen part's memory when it was last frozen, by pid
	// (V0.sim-fixes (4)): what comes back if the turns end in a wake.
	Parts map[int]uint64
	// V0.oomd-guard (decision 0036 E2): past FreezeMax while memory stalls
	// critically, so it stays frozen; the thread was told once.
	PastMax bool
	// V0.guard-wake (decision 0036 E3, E6): the thread's memory at the
	// freeze, and its cgroup's frozen_usec then (HasUsec: the kernel has it).
	Mem     uint64
	Usec    uint64
	HasUsec bool
	// V0.rerun-classes: the work's heavy class (a job's, from the gate) and
	// command (undeclared work's), which the owner's rerunnable entries match.
	Class, Cmd string
}

// UndFreeze is what the guard froze of one thread's episode of undeclared
// work: the episode (the flag's since), how many freezes so far, and when the
// last one thawed.
type UndFreeze struct {
	Episode  time.Time
	N        int
	ThawedAt time.Time
}

// FrozenJob says fs holds thread's job (not undeclared work) frozen.
func FrozenJob(fs []Frozen, thread string) bool {
	for _, f := range fs {
		if f.Owner == thread && !f.Undeclared {
			return true
		}
	}
	return false
}

// IsFrozen says fs holds thread frozen.
func IsFrozen(fs []Frozen, thread string) bool {
	for _, f := range fs {
		if f.Owner == thread {
			return true
		}
	}
	return false
}

// FrozenUndeclared is thread's freeze of undeclared work in fs.
func FrozenUndeclared(fs []Frozen, thread string) (Frozen, bool) {
	for _, f := range fs {
		if f.Undeclared && f.Owner == thread {
			return f, true
		}
	}
	return Frozen{}, false
}
