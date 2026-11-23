package resource

// V0.job-limits, V0.guard-finish: what a job's limits are decided from, and
// the decision. The policy (internal/guard's JobLimit) decides the values;
// the mechanism reads the inputs and writes the memory.high and cpu.max.

// HoldMargin is how far above its learned peak a job may go unthrottled
// (V0.job-limits); a job that hit its limit or stalled is raised to its use
// times this.
const HoldMargin = 1.25

// UnknownShare caps an unknown class's memory.high at this share of the
// agents' memory budget; GOMEMLIMIT takes the same share (V0.job-limits).
const UnknownShare = 0.5

// JobSize is what a job's class has learned; Known is false before its first
// run (or with no class). Runs is how many runs it is learned from.
type JobSize struct {
	Class  string
	Peak   uint64  // bytes
	Demand float64 // cores
	Known  bool
	Runs   int
}

// Limit is a job cgroup's memory.high and cpu.max as written ("max", bytes,
// or "quota period"), and why.
type Limit struct {
	High, CPU, Why string
}

// LimitAsk is one job's sizing on a tick, as the mechanism reads it.
type LimitAsk struct {
	Size JobSize
	// N is the host's `slots` setting (0 sizes a class as learned).
	N int
	// Budget and Used are the agents' memory budget and use now, Cores the
	// agents' CPU; Lifted: the agents' swap is nearly full (the valve).
	Budget, Used uint64
	Cores        float64
	Lifted       bool
	// Undeclared: the guard moved the job (undeclared work).
	Undeclared bool
	// First: the job was not sized before. Kept is what an earlier ask
	// decided to keep for a class not learned yet, nil for none.
	First bool
	Kept  *Limit
	// Raised is the least memory.high the job's hits and stalls earned.
	Raised uint64
	// Stalled: the job's own memory full stall is at the critical level now
	// (the mechanism's reading); Current is its memory now.
	Stalled bool
	Current uint64
	// Turns: its parts take turns (V0.guard-tidy).
	Turns bool
	// Page is the host's page size; a memory.high is whole pages.
	Page uint64
}

// LimitAnswer is the job's limits, the limits to keep for a class not learned
// yet (Keep, on the first ask only), and its Raised after this ask.
type LimitAnswer struct {
	Limit
	Keep   *Limit
	Raised uint64
}
