// Package slots runs at most N heavy jobs (test suites, builds, e2e runs)
// at once across a host's threads (SPEC 0.5 V0.86). The others wait in a
// queue: first come, first served, a priority waiter (the self-deploy's
// tests) ahead of the rest. Nobody takes a slot from its holder.
package slots

import (
	"context"
	"slices"
	"sync"
	"time"
)

// The kinds of heavy job; they all take from the same pool.
const (
	Test  = "test"
	Build = "build"
	E2E   = "e2e"
)

// Kinds are the kinds an agent may name, in the order the tool lists them.
func Kinds() []string { return []string{Test, Build, E2E} }

// Default is N when the host sets none.
const Default = 2

// Holder is one owner (a thread id, or the self-deploy) holding or waiting.
type Holder struct {
	Owner    string    `json:"owner"`
	Kind     string    `json:"kind"`
	Since    time.Time `json:"since"`
	Priority bool      `json:"priority,omitempty"`

	ready chan struct{} // a Wait's: closed on the grant
}

// Snapshot is the pool as GET /api/load shows it.
type Snapshot struct {
	N       int      `json:"n"`
	Held    []Holder `json:"held"`
	Waiting []Holder `json:"waiting"`
}

// Pool is one host's slots.
type Pool struct {
	// OnGrant hears a queued Acquire's owner get its slot later, outside
	// the lock; a Wait's grant is its return instead.
	OnGrant func(owner, kind string)
	Now     func() time.Time

	mu   sync.Mutex
	n    int
	held []Holder
	wait []Holder
}

// New is a pool of n slots; below 1, Default.
func New(n int) *Pool {
	if n < 1 {
		n = Default
	}
	return &Pool{n: n}
}

func (p *Pool) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func index(hs []Holder, owner string) int {
	return slices.IndexFunc(hs, func(h Holder) bool { return h.Owner == owner })
}

// Acquire takes a slot for owner at once, or queues it. held: owner holds
// a slot now; else ahead is how many wait before it. Asking again answers
// the same and does not queue twice.
func (p *Pool) Acquire(owner, kind string) (held bool, ahead int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index(p.held, owner) >= 0 {
		return true, 0
	}
	if i := index(p.wait, owner); i >= 0 {
		return false, i
	}
	h := Holder{Owner: owner, Kind: kind, Since: p.now()}
	if len(p.wait) == 0 && len(p.held) < p.n {
		p.held = append(p.held, h)
		return true, 0
	}
	p.wait = append(p.wait, h)
	return false, len(p.wait) - 1
}

// Wait takes a slot for owner with priority, blocking until it holds one
// or ctx ends; release gives it back.
func (p *Pool) Wait(ctx context.Context, owner, kind string) (release func(), err error) {
	release = func() { p.Release(owner) }
	p.mu.Lock()
	if len(p.held) < p.n && !slices.ContainsFunc(p.wait, func(h Holder) bool { return h.Priority }) {
		p.held = append(p.held, Holder{Owner: owner, Kind: kind, Since: p.now(), Priority: true})
		p.mu.Unlock()
		return release, nil
	}
	h := Holder{Owner: owner, Kind: kind, Since: p.now(), Priority: true, ready: make(chan struct{})}
	at := slices.IndexFunc(p.wait, func(w Holder) bool { return !w.Priority })
	if at < 0 {
		at = len(p.wait)
	}
	p.wait = slices.Insert(p.wait, at, h)
	p.mu.Unlock()
	select {
	case <-h.ready:
		return release, nil
	case <-ctx.Done():
		p.mu.Lock()
		if i := index(p.wait, owner); i >= 0 && p.wait[i].ready == h.ready {
			p.wait = slices.Delete(p.wait, i, i+1)
			p.mu.Unlock()
			return nil, ctx.Err()
		}
		p.mu.Unlock()
		// Granted as ctx ended: give it back.
		p.Release(owner)
		return nil, ctx.Err()
	}
}

// Release gives owner's slot back, or takes it out of the queue; false
// when it had neither. The next waiters get the free slots.
func (p *Pool) Release(owner string) bool {
	p.mu.Lock()
	found := false
	if i := index(p.held, owner); i >= 0 {
		p.held = slices.Delete(p.held, i, i+1)
		found = true
	} else if i := index(p.wait, owner); i >= 0 {
		p.wait = slices.Delete(p.wait, i, i+1)
		found = true
	}
	var told []Holder
	for len(p.held) < p.n && len(p.wait) > 0 {
		h := p.wait[0]
		p.wait = p.wait[1:]
		h.Since = p.now()
		if h.ready != nil {
			close(h.ready)
			h.ready = nil
		} else {
			told = append(told, h)
		}
		p.held = append(p.held, h)
	}
	on := p.OnGrant
	p.mu.Unlock()
	if on != nil {
		for _, h := range told {
			on(h.Owner, h.Kind)
		}
	}
	return found
}

// Holding reports owner's slot.
func (p *Pool) Holding(owner string) (Holder, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if i := index(p.held, owner); i >= 0 {
		return p.held[i], true
	}
	return Holder{}, false
}

// Snapshot copies the pool.
func (p *Pool) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := Snapshot{N: p.n, Held: make([]Holder, len(p.held)), Waiting: make([]Holder, len(p.wait))}
	copy(s.Held, p.held)
	copy(s.Waiting, p.wait)
	for i := range s.Waiting {
		s.Waiting[i].ready = nil
	}
	return s
}
