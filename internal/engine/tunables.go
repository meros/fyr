package engine

import (
	"time"

	"github.com/meros/fyr/internal/guard/resource"
)

// The engine's slot and memory-guard tunables, in one place. They stay here
// and are not resource.Policy fields: they are the engine's own clocks (the
// slots package has no frozen work or heavy calls), and moving them would
// export new Policy fields that the config and the fronts would then carry.

// SlotIdle is how long an admission may carry no work before it goes back
// (V0.86 (3), V0.209): no live run, or a live run with no heavy command going
// and the thread's CPU at its idle level. Going back costs nothing: the next
// heavy command is admitted again at the gate.
const SlotIdle = 2 * time.Minute

// HeavyCallStale is how long a heavy command may look dead (no result, no CPU)
// before its admission stops counting on it: a result can get lost.
const HeavyCallStale = 15 * time.Minute

// Decision 0029 S5: how long memory must be fine before a frozen job thaws,
// and the longest a job stays frozen while memory does not stall critically
// (V0.oomd-guard: on 18 and 19 Nov each of six thaws at the limit let a job
// back into a critical stall, and oomd killed under fyr.service).
const (
	ThawAfterOK = 30 * time.Second
	FreezeMax   = 10 * time.Minute
	// FreezeCeiling ends a hold past FreezeMax (V0.guard-tidy): work frozen
	// this long thaws even while the agents' memory stalls critically, unless
	// its class is rerunnable (V0.rerun-classes kills it instead).
	FreezeCeiling = 30 * time.Minute
)

// Decision 0032 slice 1: one long episode of undeclared work can be frozen
// again after a thaw (the freeze is a time slice, not a once-only verdict),
// once the work has run RefreezeAfter, and at most MaxFreezes times, so a
// job that never settles is let run in the end instead of flapping.
const (
	RefreezeAfter = 60 * time.Second
	MaxFreezes    = 8
)

// WorkMemShare is the least share of a flagged thread's memory its work's
// processes must hold to be frozen or take turns (V0.guard-tidy). On 19 Nov
// at 16:02 the guard took turns among 3 parts that each read 0.0 GB while the
// thread's 3.3 GB stayed charged to its cli/.
const WorkMemShare = 0.25

// V0.multi-freeze: while memory stays short AddFreezeAfter after the newest
// freeze, the guard freezes one more thread; when it is fine again it thaws
// them one at a time, so the thawed work does not page back in all at once.
// V0.guard-wake (decision 0036 E3, E4): the oldest freeze wakes first, at
// most one each WakeEvery, and up to the running threads less one are frozen
// at once (maxFrozen; it was 3).
const (
	AddFreezeAfter = 60 * time.Second
	WakeEvery      = 10 * time.Second
)

// TriggerFresh is how long after the PSI trigger fired the agents' stall is
// critical and the guard may freeze work that grows without waiting the
// detector's window (V0.freeze-fast-class-name): two ticks.
const TriggerFresh = resource.TriggerFresh

// guardKeep is how many decisions the front's history holds.
const guardKeep = 60
