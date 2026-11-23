package guard

import "time"

// The memory guard's clocks and limits, in one place (they were the engine's
// until V0.guard-tick). They are not resource.Policy fields: moving them there
// would export new Policy fields that the config and the fronts would carry.

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

// Decision 0032 slice 2: when the undeclared work the guard freezes has several
// heavy parts, they take turns instead of all stopping: one runs, the others
// are frozen, and the turn moves after a slice long enough to pay for bringing
// the next part's memory back from swap.
const (
	// TurnMin is the shortest turn, TurnMax the longest. The parts belong to
	// one thread, so a long turn costs nobody else anything; decision 0032 has
	// the measurements (1 s never finished, and longer turns were faster).
	TurnMin = 30 * time.Second
	TurnMax = 2 * time.Minute
	// turnFactor is how many times the swap-in time a turn lasts.
	turnFactor = 3
	// TurnMinRSS: a part that holds less gets no turn while a heavier part
	// waits (V0.multi-freeze). On 19 Nov the turns of a frozen dev stack
	// went to a crash handler, a curl and a tail, 30 s each, among 44 parts.
	TurnMinRSS = 128 << 20
)

// V0.rerun-classes (decision 0036 (F)): a job of a class the owner marked
// rerunnable is killed instead of frozen when it was frozen and could not wake
// within RerunAfter, or when it would be frozen and its learned wall time is
// under RerunShort. Its thread is told to run it again, the gate holds that
// rerun until memory is fine and it fits, and its learned peak rises by
// RerunRaise. A class killed RerunCap times in RerunCapFor is frozen instead
// for the rest of that hour, and the guard's history says so to the owner.
const (
	RerunAfter  = 2 * time.Minute
	RerunShort  = 5 * time.Minute
	RerunCap    = 3
	RerunCapFor = time.Hour
)
