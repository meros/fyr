package engine

import (
	"time"
)

// The engine's slot tunables: how long an admission may sit idle. The memory
// guard's clocks are internal/guard's (tunables.go there).

// SlotIdle is how long an admission may carry no work before it goes back
// (V0.86 (3), V0.209): no live run, or a live run with no heavy command going
// and the thread's CPU at its idle level. Going back costs nothing: the next
// heavy command is admitted again at the gate.
const SlotIdle = 2 * time.Minute

// HeavyCallStale is how long a heavy command may look dead (no result, no CPU)
// before its admission stops counting on it: a result can get lost.
const HeavyCallStale = 15 * time.Minute

// guardKeep is how many decisions the front's history holds.
const guardKeep = 60
