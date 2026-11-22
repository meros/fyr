package authtest

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	"github.com/meros/fyr/internal/discovery"
	"github.com/meros/fyr/internal/fsutil"
)

// LAN is an in-memory mDNS bus behind discovery.Source (decision 0030 4.1):
// a test adds, spoofs and withdraws answers. The installs themselves use real
// loopback TCP; only the multicast is replaced.
type LAN struct {
	mu      sync.Mutex
	answers []discovery.Candidate
	file    string
}

// File makes the bus readable by real fyrds: every change is written to path as
// JSON, and an install started with config lanBusFile reads it as its mDNS source.
func (l *LAN) File(path string) *LAN {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.file = path
	l.flush()
	return l
}

// flush writes the answers; the caller holds l.mu.
func (l *LAN) flush() {
	if l.file == "" {
		return
	}
	b, _ := json.Marshal(l.answers)
	if err := fsutil.WriteAtomic(l.file, b, 0o600); err != nil {
		log.Printf("authtest: cannot write %s: %v", l.file, err)
	}
}

// Advertise adds an answer: install id at addr.
func (l *LAN) Advertise(id, addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.answers = append(l.answers, discovery.Candidate{ID: id, Addr: addr, Source: "mdns"})
	l.flush()
}

// Announce makes addr the one answer for id: what an install does when it moves.
func (l *LAN) Announce(id, addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	keep := l.answers[:0]
	for _, a := range l.answers {
		if a.ID != id {
			keep = append(keep, a)
		}
	}
	l.answers = append(keep, discovery.Candidate{ID: id, Addr: addr, Source: "mdns"})
	l.flush()
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
	l.flush()
}

// Candidates implements discovery.Source.
func (l *LAN) Candidates(ctx context.Context) ([]discovery.Candidate, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]discovery.Candidate(nil), l.answers...), nil
}

var _ discovery.Source = (*LAN)(nil)
