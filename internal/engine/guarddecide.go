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
