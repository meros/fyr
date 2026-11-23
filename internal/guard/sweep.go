package guard

// The slots' side of the guard's tick (V0.guard-finish): the idle sweep that
// gives back admissions nothing uses, the notice that a job is heavier than
// predicted, the gate's hold while memory is short, and the budget's step.
// Each is a function of what the engine gathers; the engine applies the
// result (releases, notices, the pool's budget) and keeps no rule of its own.

import (
	"fmt"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
)

// SweepThread is what the sweep reads of a thread that holds or waits for an
// admission.
type SweepThread struct {
	// Running: its agent runs now.
	Running bool
	// Admitted: it runs admitted work that outlives the run (a background
	// task with the job, V0.201).
	Admitted bool
}

// SweepIn is the idle sweep's inputs, taken at Now.
type SweepIn struct {
	Now     time.Time
	Held    []resource.Holder
	Waiting []resource.Holder
	// Threads are the threads that exist; a holder not in it is gone.
	Threads map[string]SweepThread
	// Active is when each non-priority holder's work last showed (its
	// SlotActive, never before its grant); a holder without an answer is
	// missing.
	Active map[string]time.Time
	// Calls are the foreground heavy calls running per thread, Background the
	// threads with a job that outlives its call.
	Calls      map[string]int
	Background map[string]bool
	// Frozen is the guard's frozen work (State.Frozen); Thawed when each
	// thread's job thawed last (Decision Thawed).
	Frozen []Frozen
	Thawed map[string]time.Time
	// Idle is since when each holder without a live run has been idle.
	Idle map[string]time.Time
}

// Sweep gives back the admissions of threads that are gone or held one for
// SlotIdle without work (V0.86 (3), V0.201, V0.209): it returns the threads
// to release, in the order found, and the next Idle clocks. The input maps
// are not changed.
func Sweep(in SweepIn) (drop []string, idle map[string]time.Time) {
	idle = make(map[string]time.Time, len(in.Idle))
	for k, v := range in.Idle {
		idle[k] = v
	}
	active := make(map[string]time.Time, len(in.Active))
	for k, v := range in.Active {
		active[k] = v
	}
	for _, h := range in.Held {
		if h.Priority {
			continue // the self-deploy releases its own
		}
		// V0.sim-fixes (1): a job the guard holds frozen does not move its
		// SlotActive, so its hold read as idle: a foreground call went stale at
		// HeavyCallStale, a background job at SlotIdle, and after a restart any
		// frozen job at SlotIdle (no calls are known). The guard then thawed it
		// as "episode or hold ended" into the critical stall it was frozen for.
		// A frozen hold is not swept, and its idle time starts at its thaw.
		t, exists := in.Threads[h.Owner]
		if exists && FrozenJob(in.Frozen, h.Owner) {
			delete(idle, h.Owner)
			continue
		}
		if at, ok := in.Thawed[h.Owner]; ok {
			if a, ok := active[h.Owner]; ok && a.Before(at) {
				active[h.Owner] = at
			}
			if since, ok := idle[h.Owner]; ok && since.Before(at) {
				idle[h.Owner] = at
			}
		}
		switch {
		case !exists:
			drop = append(drop, h.Owner)
		case t.Running:
			delete(idle, h.Owner)
			// V0.209: an admission nothing uses goes back by itself.
			if at, ok := active[h.Owner]; ok {
				gone := in.Now.Sub(at)
				switch {
				case in.Calls[h.Owner] > 0 && gone < HeavyCallStale:
				case in.Background[h.Owner] && t.Admitted:
				case gone >= SlotIdle:
					drop = append(drop, h.Owner)
				}
			}
		case t.Admitted: // V0.201: the idle time starts when it is done
			delete(idle, h.Owner)
		default:
			since, ok := idle[h.Owner]
			if !ok {
				idle[h.Owner] = in.Now
			} else if in.Now.Sub(since) >= SlotIdle {
				drop = append(drop, h.Owner)
			}
		}
	}
	for _, h := range in.Waiting {
		if _, exists := in.Threads[h.Owner]; !h.Priority && !exists {
			drop = append(drop, h.Owner)
		}
	}
	return drop, idle
}

// Over is a holder's job against its prediction, as admission measures it
// (V0.163: in percent of the host).
type Over struct {
	Class           string
	Live, Predicted float64
	Over            bool
}

// OverNotice tells Thread that its job uses more than predicted.
type OverNotice struct {
	Thread string
	Over
}

// HeavierThanPredicted tells each holder once per hold that its job uses more
// of the budget than predicted (decision 0029 S3); nothing is stopped. told is
// when each holder was told (its hold's Since); measure reads one holder and
// is asked only of a holder not told for its hold yet. It returns the notices
// in the holders' order; the caller records each as told before the next.
func HeavierThanPredicted(held []resource.Holder, told map[string]time.Time, measure func(owner string) Over) []OverNotice {
	var out []OverNotice
	for _, h := range held {
		if h.Priority {
			continue
		}
		if since, done := told[h.Owner]; done && since.Equal(h.Since) {
			continue
		}
		if o := measure(h.Owner); o.Over {
			out = append(out, OverNotice{Thread: h.Owner, Over: o})
		}
	}
	return out
}

// GateHolds says whether the gate holds a newly matched heavy command while
// memory is short (V0.guard-wake, decision 0036 E1): a command that has not
// started has no timer running, so holding it costs less than freezing a job
// that runs. The pool already queues a thread that holds no admission (memory
// short holds every job); the gate holds the rest: a thread that holds a
// non-priority admission, and a learned (soft) command, which never queues.
func GateHolds(short, holds, priority, soft bool) bool {
	if !short {
		return false
	}
	return !((holds && priority) || (!holds && !soft))
}

// BudgetMove is the budget controller's decision on a tick: Set says the
// pool's budget becomes F for Why; Log, when not empty, is the line that
// tells the move (V0.load-tuning).
type BudgetMove struct {
	Set bool
	F   resource.Factors
	Why string
	Log string
}

// BudgetIn is what the budget's step reads: the pool's snapshot s, its budget
// now (old), its last admission and the signals read for this step.
type BudgetIn struct {
	Now       time.Time
	Snapshot  resource.Snapshot
	Old       resource.Factors
	LastGrant time.Time
	Signals   resource.Signals
}

// Budget steps the budget controller a (decision 0029 S4) and decides the
// pool's budget: it is set when it moved, or when its reason changed.
func Budget(a *AIMD, in BudgetIn) BudgetMove {
	s := in.Snapshot
	f, why := a.Step(in.Now, in.Signals, len(s.Waiting) > 0, in.LastGrant, s.Bound)
	m := BudgetMove{F: f, Why: why}
	if f != in.Old || (s.Budget != nil && s.BudgetWhy != why) {
		m.Set = true
		if f != in.Old {
			m.Log = fmt.Sprintf("slots: budget cpu=%.3f->%.3f mem=%.3f->%.3f why=%q waiting=%d held=%d %s", in.Old.CPU, f.CPU, in.Old.Mem, f.Mem, why, len(s.Waiting), len(s.Held), SignalsLine(in.Signals))
		}
	}
	return m
}
