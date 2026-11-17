package authtest

import (
	"context"
	"sync"

	"github.com/meros/fyr/internal/discovery"
)

// LAN is an in-memory mDNS bus behind discovery.Source (decision 0030 4.1):
// a test adds, spoofs and withdraws answers. The installs themselves use real
// loopback TCP; only the multicast is replaced.
type LAN struct {
	mu      sync.Mutex
	answers []discovery.Candidate
}

// Advertise adds an answer: install id at addr.
func (l *LAN) Advertise(id, addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.answers = append(l.answers, discovery.Candidate{ID: id, Addr: addr, Source: "mdns"})
}

// Spoof adds an answer that claims id at an address that is not the install's.
// It is the same on the bus; the name says what the test means.
func (l *LAN) Spoof(id, attackerAddr string) { l.Advertise(id, attackerAddr) }

// Withdraw removes every answer at addr.
func (l *LAN) Withdraw(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	keep := l.answers[:0]
	for _, a := range l.answers {
		if a.Addr != addr {
			keep = append(keep, a)
		}
	}
	l.answers = keep
}

// Candidates implements discovery.Source.
func (l *LAN) Candidates(ctx context.Context) ([]discovery.Candidate, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]discovery.Candidate(nil), l.answers...), nil
}

var _ discovery.Source = (*LAN)(nil)
