// Package discovery names where candidates for other installs come from
// (decision 0030 3.6). A source yields (id?, address) candidates and never
// trust: the link's handshake must prove the id that was meant. The tailnet
// and mDNS sources of the later slices implement Source; the test harness's
// in-memory bus (internal/testkit/authtest) implements it too, so a test can add,
// spoof and withdraw answers.
package discovery

import "context"

// Candidate is an address that may reach an install.
type Candidate struct {
	// ID is the install id the source claims, "" when it does not say. An
	// mDNS answer carries one in its TXT record; it is a claim, not proof.
	ID string
	// Addr is host:port of the peer port.
	Addr string
	// Name is the instance name it announced ("fyr-<short id>" from an install
	// of Fyr), "" when the source gives none. A claim like ID.
	Name string
	// Source names where it came from: "tailnet", "mdns", "hand".
	Source string
}

// Source lists the candidates it knows now.
type Source interface {
	Candidates(ctx context.Context) ([]Candidate, error)
}
