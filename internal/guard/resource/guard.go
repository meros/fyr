package resource

import "time"

// Decision 0032: what the memory guard did, for the front (GET /api/load,
// `slots.guard`): a short history of its decisions and, per thread, where the
// freezes of its episode stand. The engine publishes it; the pool only copies.

// WorkUnit is one part of a thread's undeclared work that can be frozen alone
// (decision 0032 slice 2): its first process, its memory and its command.
type WorkUnit struct {
	PID     int
	RSS     uint64
	Command string
}

// GuardEvent is one decision: "short" or "recovered" (the host's memory),
// "freeze" or "thaw" of a thread's undeclared work or of a job it asked for;
// V0.rerun-classes: "kill" of a rerunnable job, and "capped" when its class
// was killed too often and is frozen instead for an hour (Class names it);
// V0.oomd-frozen-work: "ended", work the guard killed before oomd could.
type GuardEvent struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	// Work is "undeclared" or "job" (freeze and thaw only).
	Work   string `json:"work,omitempty"`
	Thread string `json:"thread,omitempty"`
	// N is the freeze's number in its episode (undeclared work).
	N int `json:"n,omitempty"`
	// Reason says why a thaw happened: "memory", "gone", "limit", "ended".
	Reason string `json:"reason,omitempty"`
	// Class is the rerunnable class of a kill or a cap.
	Class string `json:"class,omitempty"`
	// Headroom is the memory free of the agents' capacity, 0 to 1.
	Headroom *float64 `json:"headroom,omitempty"`
	Stall    string   `json:"stall,omitempty"`
}

// GuardThread is where one thread's undeclared-work freezes stand: N so far
// of Max, frozen since FrozenAt (nil while it runs), and when the next freeze
// may start (NextAt, nil when it may now or the cap is reached).
type GuardThread struct {
	Thread   string     `json:"thread"`
	N        int        `json:"n"`
	Frozen   bool       `json:"frozen"`
	FrozenAt *time.Time `json:"frozenAt,omitempty"`
	NextAt   *time.Time `json:"nextAt,omitempty"`
	Capped   bool       `json:"capped,omitempty"`
}

// Guard is the guard's published view.
type Guard struct {
	// Short: the host's memory is short now, since ShortSince.
	Short      bool       `json:"short"`
	ShortSince *time.Time `json:"shortSince,omitempty"`
	// Max is the most freezes of one episode; RefreezeSec the seconds the
	// work runs between two.
	Max         int           `json:"max"`
	RefreezeSec int           `json:"refreezeSec"`
	Events      []GuardEvent  `json:"events"`
	Threads     []GuardThread `json:"threads,omitempty"`
	// Rerun is each rerunnable class the guard killed in the last hour
	// (V0.rerun-classes): how often, and until when it is frozen instead.
	Rerun []GuardRerun `json:"rerun,omitempty"`
	// Invariants are the load invariants (V0.load-invariants): the open
	// cards and each invariant's violations.
	Invariants *Invariants `json:"invariants,omitempty"`
}

// InvariantCard is an invariant's violation as the owner's Needs you
// shows it (V0.load-invariants, V0.thread-invariants): the invariant in one
// sentence, when, the evidence, and recent log or transition lines.
type InvariantCard struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind,omitempty"` // "" or "load", or "thread"
	Text     string    `json:"text"`
	At       time.Time `json:"at"`
	Evidence string    `json:"evidence"`
	Lines    []string  `json:"lines,omitempty"`
}

// InvariantStat is one invariant's violations so far and the last one.
type InvariantStat struct {
	ID    string     `json:"id"`
	Kind  string     `json:"kind,omitempty"` // "" or "load", or "thread"
	Text  string     `json:"text"`
	Count int        `json:"count"`
	Last  *time.Time `json:"last,omitempty"`
}

// Invariants is the load invariants' view. FixDir is the Fyr checkout a
// card's Fix it starts its thread in, "" when the host has none.
type Invariants struct {
	Cards  []InvariantCard `json:"cards,omitempty"`
	Stats  []InvariantStat `json:"stats"`
	FixDir string          `json:"fixDir,omitempty"`
}

// GuardRerun is one rerunnable class's kills in the last hour.
type GuardRerun struct {
	Class       string     `json:"class"`
	Kills       int        `json:"kills"`
	CappedUntil *time.Time `json:"cappedUntil,omitempty"`
}
