package slots

import "time"

// Decision 0032: what the memory guard did, for the front (GET /api/load,
// `slots.guard`): a short history of its decisions and, per thread, where the
// freezes of its episode stand. The engine publishes it; the pool only copies.

// GuardEvent is one decision: "short" or "recovered" (the host's memory),
// "freeze" or "thaw" of a thread's undeclared work or of a job it asked for.
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
}
