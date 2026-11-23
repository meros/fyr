package resource

import "time"

// The heavy commands and the service patterns as GET /api/load shows them
// (V0.164, V0.services-not-jobs); internal/heavy keeps them.

// Added is a heavy command beyond the built-in ones: learned by Fyr, or added
// by the owner before V0.209.
type Added struct {
	Pattern string    `json:"pattern"`
	Kind    string    `json:"kind"`
	Name    string    `json:"name"`
	At      time.Time `json:"at"`
	Refused int       `json:"refused"`
	Learned bool      `json:"learned,omitempty"`
}

// ServicePattern is one pattern as Settings shows it.
type ServicePattern struct {
	Pattern string `json:"pattern"`
	BuiltIn bool   `json:"builtIn,omitempty"`
}
