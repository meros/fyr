package engine

import (
	"fmt"
	"time"

	"github.com/meros/fyr/internal/slots"
)

// The pure part of the memory guard's tick (freezeGuard): what the signals
// say, and what becomes of each frozen job. Nothing here reads the engine, so
// each rule is tested on its own (guarddecide_test.go).

// guardMemory is what one tick's signals say about the host's memory.
type guardMemory struct {
	critical       bool // the host's or the agents' stall is critical
	agentsCritical bool
	nearOomd       bool // the unit's stall is near oomd's limit
	short          bool // memory is short: headroom under the reserve, or a critical or near-oomd stall
	calm           bool // no stall at or above elevated
}

// memoryState reads sig under policy p (V0.oomd-guard, V0.guard-agents-stall).
func memoryState(sig slots.Signals, p slots.Policy) guardMemory {
	critical, _ := sig.Critical()
	m := guardMemory{
		critical:       critical,
		agentsCritical: sig.AgentsStall == slots.StallCritical,
		nearOomd:       sig.NearOomd(),
	}
	m.short = sig.Provider != "" && ((sig.Headroom != nil && *sig.Headroom < sig.Reserve(p)) || critical || m.nearOomd)
	m.calm = !critical && !m.nearOomd && sig.Stall != slots.StallElevated && sig.AgentsStall != slots.StallElevated
	return m
}

// wakeFine says memory is fine enough for the wake clock to run: calm, and
// with a cap (V0.guard-wake) the stall alone decides; without one also the
// headroom (reserve + 5 %).
func wakeFine(m guardMemory, sig slots.Signals, p slots.Policy, capNow uint64) bool {
	return m.calm && (capNow > 0 || sig.Headroom == nil || *sig.Headroom >= sig.Reserve(p)+0.05)
}

// frozenFacts is what the tick knows about one frozen job.
type frozenFacts struct {
	holding, emptied bool // its hold or episode goes on; its cgroup has no process left
	age              time.Duration
	rerunnable       bool // a rerun entry would kill it at the ceiling instead
}

// frozenAction is what a tick does with a frozen job.
type frozenAction int

const (
	actKeep        frozenAction = iota
	actKeepPastMax              // kept past FreezeMax by the agents' critical stall: told once
	actThaw
)

// frozenWhy names why a job is kept, for the log line.
type frozenWhy int

const (
	whyNone     frozenWhy = iota
	whyEnded              // hold or episode ended, work still there, memory critical
	whyNoRoom             // past FreezeMax but what is out of RAM would not fit
	whyCritical           // past FreezeMax and the agents' stall is critical
)

type frozenVerdict struct {
	act     frozenAction
	why     frozenWhy
	room    string // whyNoRoom: the reason from the room check
	ceiling bool   // actThaw: at the ceiling while the agents' stall is critical
}

// judgeFrozen is the tick's rule for one frozen job (V0.guard-tidy,
// V0.sim-fixes (1) and (3)). room is asked only when the timed thaw is the
// question, so its cost is paid then.
func judgeFrozen(m guardMemory, f frozenFacts, room func() (bool, string)) frozenVerdict {
	overMax := f.age >= FreezeMax
	ceiling := f.age >= FreezeCeiling && !f.rerunnable
	// A hold that ended while its work is still there does not thaw into the
	// critical stall it was frozen for: it thaws below critical, or at the ceiling.
	if !f.holding && !f.emptied && (m.critical || m.agentsCritical) && !ceiling {
		return frozenVerdict{act: actKeep, why: whyEnded}
	}
	// Past FreezeMax only the agents' own critical stall holds the work.
	timed := overMax && !m.agentsCritical
	if timed && f.holding && !f.emptied && !ceiling {
		if ok, why := room(); !ok {
			return frozenVerdict{act: actKeep, why: whyNoRoom, room: why}
		}
	}
	if !f.holding || f.emptied || timed || ceiling {
		return frozenVerdict{act: actThaw, ceiling: ceiling && m.agentsCritical}
	}
	if overMax {
		return frozenVerdict{act: actKeepPastMax, why: whyCritical}
	}
	return frozenVerdict{act: actKeep}
}

// thawing is a frozen job a tick thaws, and why.
type thawing struct {
	f                         frozenJob
	holding, emptied, ceiling bool
	woke                      string // the wake rule's reason, "" for the others
}

// thawWords gives the long reason for the log and the short one for the
// guard's history.
func thawWords(t thawing, now time.Time) (reason, why string) {
	reason, why = "memory fine for 30 s", "memory"
	switch {
	case t.emptied:
		reason, why = "its processes are gone", "gone"
	case !t.holding:
		reason, why = "episode or hold ended", "ended"
	case t.woke != "":
		reason = t.woke
	case t.ceiling:
		reason, why = fmt.Sprintf("%s ceiling, the agents' memory still stalls critically", frozenWords(FreezeCeiling)), "ceiling"
	case now.Sub(t.f.at) >= FreezeMax:
		reason, why = "10 min limit", "limit"
		if t.f.pastMax {
			reason = "10 min limit, stall no longer critical"
		}
	}
	return reason, why
}

// stillHolding says whether frozen job f's hold or episode goes on: an
// undeclared freeze while its thread is still flagged with the same start
// (admitted, ended or released, it is not), a job while the slot holder it
// was frozen under is still held.
func stillHolding(f frozenJob, flagged []slots.Undeclared, held []slots.Holder) bool {
	if f.undeclared {
		for _, u := range flagged {
			if u.Thread == f.owner && u.Since.Equal(f.episode) {
				return true
			}
		}
	}
	for _, h := range held {
		if h.Owner == f.owner && h.Since.Format(time.RFC3339Nano) == f.since {
			return true
		}
	}
	return false
}

// wakesAlone is V0.sim-fixes (3): the frozen work left is all the heavy work
// (nothing runs that it could take memory from), memory does not stall
// critically, and WakeEvery has passed since the last wake: the oldest freeze
// wakes at once. heavyRunning is asked last, so its cost is paid only then.
func wakesAlone(kept, thawing int, m guardMemory, sinceWake time.Duration, heavyRunning func() int) bool {
	return kept > 0 && thawing == 0 && !m.critical && !m.nearOomd && heavyRunning() == 0 && sinceWake >= WakeEvery
}

// wakeDue is V0.guard-wake (E3): the wake clock has run ThawAfterOK, and
// WakeEvery has passed since the last wake, with a freeze kept and no thaw
// this tick.
func wakeDue(kept, thawing int, okSince, lastWake, now time.Time) bool {
	return kept > 0 && thawing == 0 && !okSince.IsZero() && now.Sub(okSince) >= ThawAfterOK && now.Sub(lastWake) >= WakeEvery
}

// freezeGateWhy is what stands between a tick and a new freeze.
type freezeGateWhy int

const (
	gateOpen    freezeGateWhy = iota // memory is short and stalls: go on
	gateQuiet                        // not short, or a thaw or a kill this tick: nothing to say
	gateNoStall                      // short on headroom alone: say it once
)

// freezeGate decides whether a tick may freeze more work.
func freezeGate(m guardMemory, stalling bool, thawedOrKilled int) freezeGateWhy {
	switch {
	case !m.short || thawedOrKilled > 0:
		return gateQuiet
	case !m.critical && !m.nearOomd && !stalling:
		return gateNoStall
	}
	return gateOpen
}

// freezeRoom says whether one more freeze must wait: block when n freezes (at
// most max(maxFrozen, 1)) are held or the last one is younger than
// AddFreezeAfter (not near oomd's limit, where the next does not wait: oomd
// kills after its duration, sooner than that). say is true when the limit
// itself, not the wait, is why (so it is logged once).
func freezeRoom(kept, maxFrozen int, lastFrozen, now time.Time, nearOomd bool) (block, say bool) {
	wait := kept > 0 && now.Sub(lastFrozen) < AddFreezeAfter && !nearOomd
	if kept >= max(maxFrozen, 1) || wait {
		return true, kept >= maxFrozen && maxFrozen > 0
	}
	return false, false
}
