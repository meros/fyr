// Package slots runs at most N heavy jobs (test suites, builds, e2e runs)
// at once across a host's threads (SPEC 0.5 V0.86). The others wait in a
// queue: first come, first served, a priority waiter (the self-deploy's
// tests) ahead of the rest. Nobody takes a slot from its holder. N 0 is
// off: every acquire holds at once and nothing is kept. The holders and
// the queue live in the store's KV table, so they survive a restart.
package slots

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"slices"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/lockwatch"
)

// The kinds of heavy job; they all take from the same pool.
const (
	Test  = "test"
	Build = "build"
	E2E   = "e2e"
)

// Kinds are the kinds an agent may name, in the order the tool lists them.
func Kinds() []string { return []string{Test, Build, E2E} }

// Default is N when the host sets none, or a negative one.
const Default = 2

// key is where the pool is kept in the store's KV table.
const key = "slots"

// KV is the store's key-value table (*store.Store).
type KV interface {
	GetKV(k string) (string, error)
	SetKV(k, v string) error
}

// Holder is one owner (a thread id, or the self-deploy) holding or waiting.
type Holder struct {
	Owner    string    `json:"owner"`
	Kind     string    `json:"kind"`
	Since    time.Time `json:"since"`
	Priority bool      `json:"priority,omitempty"`
	// Cost is a holder's share of the budget in a snapshot (decision 0029
	// S2); 0 without Pool.Cost.
	Cost float64 `json:"cost,omitempty"`

	ready chan struct{} // a Wait's: closed on the grant
}

// Snapshot is the pool as GET /api/load shows it.
type Snapshot struct {
	N       int      `json:"n"`
	Held    []Holder `json:"held"`
	Waiting []Holder `json:"waiting"`
	// Decision 0029 S2: Used is the holders' costs summed (nil without
	// Pool.Cost); Why is why the head waits ("" when nothing holds it).
	Used *float64 `json:"used,omitempty"`
	Why  string   `json:"why,omitempty"`
	// Decision 0029 S4: the budget now (nil while it is N) and why.
	Budget    *float64 `json:"budget,omitempty"`
	BudgetWhy string   `json:"budgetWhy,omitempty"`
}

// Pool is one host's slots.
type Pool struct {
	// OnGrant hears a queued Acquire's owner get its slot later, outside
	// the lock; a Wait's grant is its return instead.
	OnGrant func(owner, kind string)
	Now     func() time.Time
	// Admit decides whether head may start beside held (decision 0029 S2),
	// and why not; nil is the count rule. It runs under the pool's lock and
	// must not call the pool.
	Admit func(head Holder, held []Holder) (bool, string)
	// Cost is a holder's share of the budget for the snapshot; optional.
	Cost func(h Holder) float64
	// Backfill decides whether c, behind a head that does not fit, may
	// start now without delaying it (decision 0029 S3, EASY); nil never.
	// Under the pool's lock, like Admit.
	Backfill func(head, c Holder, held []Holder) bool

	mu   lockwatch.Mutex
	pub  atomic.Pointer[Snapshot] // the last snapshot, for View
	n    int
	kv   KV
	held []Holder
	wait []Holder
	why  string
	// Decision 0029 S4: the budget (0 is N), why, and the last grant.
	budget    float64
	budgetWhy string
	lastGrant time.Time
	// budgetBits mirrors budget for Budget, which Admit calls under p.mu.
	budgetBits atomic.Uint64
	// passes: the head (owner and since) and how often backfill passed it.
	passHead  string
	passSince time.Time
	passes    int
}

// New is a pool of n slots kept in memory only: 0 is off, below 0 Default.
func New(n int) *Pool {
	if n < 0 {
		n = Default
	}
	return &Pool{n: n}
}

// Open is New with the holders and the queue loaded from kv and written
// back on every change. A priority wait is never kept: it ends with its
// daemon.
func Open(n int, kv KV) *Pool {
	p := New(n)
	if p.n == 0 || kv == nil {
		return p
	}
	p.kv = kv
	if raw, _ := kv.GetKV(key); raw != "" {
		var s Snapshot
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			log.Printf("slots: stored pool unreadable, starting empty: %v", err)
		} else {
			p.held, p.wait = s.Held, s.Waiting
		}
	}
	return p
}

// Off reports a pool of 0 slots: no limit.
func (p *Pool) Off() bool { return p.n == 0 }

func (p *Pool) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// save writes the holders and the queue without priority waits; call it
// with p.mu held.
func (p *Pool) save() {
	if p.kv == nil {
		return
	}
	keep := func(hs []Holder) []Holder {
		out := []Holder{}
		for _, h := range hs {
			if !h.Priority {
				h.ready = nil
				out = append(out, h)
			}
		}
		return out
	}
	b, _ := json.Marshal(Snapshot{N: p.n, Held: keep(p.held), Waiting: keep(p.wait)})
	if err := p.kv.SetKV(key, string(b)); err != nil {
		log.Printf("slots: store: %v", err)
	}
}

func index(hs []Holder, owner string) int {
	return slices.IndexFunc(hs, func(h Holder) bool { return h.Owner == owner })
}

// Acquire takes a slot for owner at once, or queues it. held: owner holds
// a slot now; else ahead is how many wait before it. Asking again answers
// the same and does not queue twice. Off: always held, nothing kept.
func (p *Pool) Acquire(owner, kind string) (held bool, ahead int) {
	if p.Off() {
		return true, 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if index(p.held, owner) >= 0 {
		return true, 0
	}
	if i := index(p.wait, owner); i >= 0 {
		return false, i
	}
	h := Holder{Owner: owner, Kind: kind, Since: p.now()}
	defer p.save()
	if len(p.wait) == 0 && p.admitLocked(h) {
		p.held = append(p.held, h)
		p.lastGrant = h.Since
		return true, 0
	}
	p.wait = append(p.wait, h)
	// Decision 0029 S3: it may start at once behind a head that does not fit
	// (the head was just asked; only backfill runs here, Admit once per call).
	granted := p.backfillLocked(nil)
	mine := false
	var others []Holder
	for _, g := range granted {
		if g.Owner == owner {
			mine = true
		} else {
			others = append(others, g)
		}
	}
	if len(others) > 0 && p.OnGrant != nil {
		on := p.OnGrant
		defer func() {
			for _, g := range others {
				on(g.Owner, g.Kind)
			}
		}()
	}
	if mine {
		return true, 0
	}
	return false, index(p.wait, owner)
}

// Wait takes a slot for owner with priority, blocking until it holds one
// or ctx ends; release gives it back. Off: at once.
func (p *Pool) Wait(ctx context.Context, owner, kind string) (release func(), err error) {
	if p.Off() {
		return func() {}, nil
	}
	release = func() { p.Release(owner) }
	p.mu.Lock()
	if !slices.ContainsFunc(p.wait, func(h Holder) bool { return h.Priority }) && p.admitLocked(Holder{Owner: owner, Kind: kind, Priority: true}) {
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
	if p.Off() {
		return false
	}
	p.mu.Lock()
	found := false
	if i := index(p.held, owner); i >= 0 {
		p.held = slices.Delete(p.held, i, i+1)
		found = true
	} else if i := index(p.wait, owner); i >= 0 {
		p.wait = slices.Delete(p.wait, i, i+1)
		found = true
	}
	p.fillLocked()
	return found
}

// Fill hands the free slots to the next waiters; after a load with a
// larger N, the owner calls it once OnGrant is set.
func (p *Pool) Fill() {
	if p.Off() {
		return
	}
	p.mu.Lock()
	p.fillLocked()
}

// fillLocked promotes waiters, saves, unlocks p.mu and tells the agents.
func (p *Pool) fillLocked() {
	told := p.promoteLocked()
	p.save()
	on := p.OnGrant
	p.mu.Unlock()
	if on != nil {
		for _, h := range told {
			on(h.Owner, h.Kind)
		}
	}
}

// promoteLocked grants the queue's head while it fits, then (decision 0029
// S3) backfills later jobs that Backfill allows, at most BackfillPasses
// times per head and not once the head waited BackfillFor. It returns the
// grants to tell (a priority Wait's grant closes its channel instead). Call
// it with p.mu held.
func (p *Pool) promoteLocked() []Holder {
	var told []Holder
	for len(p.wait) > 0 && p.admitLocked(p.wait[0]) {
		told = p.grantLocked(0, told)
	}
	return p.backfillLocked(told)
}

// grantLocked moves wait[i] to held and returns told with it, unless it is a
// priority Wait (its channel is closed instead). Call it with p.mu held.
func (p *Pool) grantLocked(i int, told []Holder) []Holder {
	h := p.wait[i]
	p.wait = slices.Delete(p.wait, i, i+1)
	h.Since = p.now()
	p.lastGrant = h.Since
	if h.ready != nil {
		close(h.ready)
		h.ready = nil
	} else {
		told = append(told, h)
	}
	p.held = append(p.held, h)
	return told
}

// backfillLocked is promoteLocked's second half: later jobs Backfill lets
// pass a head that does not fit (EASY, capped). Call it with p.mu held.
func (p *Pool) backfillLocked(told []Holder) []Holder {
	grant := func(i int) { told = p.grantLocked(i, told) }
	if len(p.wait) < 2 || p.Backfill == nil {
		return told
	}
	head := p.wait[0]
	if p.passHead != head.Owner || !p.passSince.Equal(head.Since) {
		p.passHead, p.passSince, p.passes = head.Owner, head.Since, 0
	}
	for i := 1; i < len(p.wait); {
		if p.passes >= BackfillPasses || p.now().Sub(head.Since) >= BackfillFor {
			break
		}
		if p.Backfill(head, p.wait[i], slices.Clone(p.held)) {
			grant(i)
			p.passes++
			continue
		}
		i++
	}
	return told
}

// admitLocked asks Admit (else the count rule) about head and keeps why it
// waits; call it with p.mu held.
func (p *Pool) admitLocked(head Holder) bool {
	if p.Admit == nil {
		ok := len(p.held) < p.n
		p.why = ""
		return ok
	}
	ok, why := p.Admit(head, slices.Clone(p.held))
	p.why = why
	if ok {
		p.why = ""
	}
	return ok
}

// Budget is the pool's capacity in cost units: N, or what S4's controller
// set. Admit calls it under the pool's lock, so it reads a value the
// controller stores atomically enough: a float set under p.mu.
func (p *Pool) Budget() float64 {
	if b := p.budgetLoad(); b > 0 {
		return b
	}
	return float64(p.n)
}

// SetBudget sets the budget and why (decision 0029 S4), and grants the
// queue what now fits.
func (p *Pool) SetBudget(b float64, why string) {
	if p.Off() {
		return
	}
	p.mu.Lock()
	p.budget, p.budgetWhy = b, why
	p.budgetBits.Store(math.Float64bits(b))
	p.fillLocked()
}

func (p *Pool) budgetLoad() float64 { return math.Float64frombits(p.budgetBits.Load()) }

// LastGrant is when the pool last admitted a job.
func (p *Pool) LastGrant() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastGrant
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
	return p.snapshotLocked()
}

// View is the pool for a reader that must not wait (V0.147: GET
// /api/load): a fresh copy when the lock is free, else the last copy any
// Snapshot or View took.
func (p *Pool) View() Snapshot {
	if p.mu.TryLock() {
		defer p.mu.Unlock()
		return p.snapshotLocked()
	}
	if v := p.pub.Load(); v != nil {
		return *v
	}
	return p.Snapshot()
}

func (p *Pool) snapshotLocked() Snapshot {
	s := Snapshot{N: p.n, Held: make([]Holder, len(p.held)), Waiting: make([]Holder, len(p.wait))}
	copy(s.Held, p.held)
	copy(s.Waiting, p.wait)
	for i := range s.Waiting {
		s.Waiting[i].ready = nil
	}
	if len(s.Waiting) > 0 {
		s.Why = p.why
	}
	if p.budget > 0 && p.budget != float64(p.n) {
		b := p.budget
		s.Budget, s.BudgetWhy = &b, p.budgetWhy
	}
	if p.Cost != nil {
		used := 0.0
		for i := range s.Held {
			s.Held[i].Cost = p.Cost(s.Held[i])
			used += s.Held[i].Cost
		}
		used = float64(int(used*100+0.5)) / 100
		s.Used = &used
	}
	pub := s
	pub.Held, pub.Waiting = slices.Clone(s.Held), slices.Clone(s.Waiting)
	p.pub.Store(&pub)
	return s
}

// Reorder is the owner's order of the queue (SPEC V0.137): the listed
// owners that wait without priority go first, in the given order, and the
// other waiters follow in their own order; priority waiters stay ahead of
// all of them and cannot be moved. An owner that no longer waits is
// ignored, so a page that read the queue a moment ago cannot drop anyone.
// It returns the pool as it is now.
func (p *Pool) Reorder(owners []string) Snapshot {
	if p.Off() {
		return p.Snapshot()
	}
	p.mu.Lock()
	var pri, rest []Holder
	for _, h := range p.wait {
		if h.Priority {
			pri = append(pri, h)
		} else {
			rest = append(rest, h)
		}
	}
	next := pri
	for _, o := range owners {
		if i := index(rest, o); i >= 0 {
			next = append(next, rest[i])
			rest = slices.Delete(rest, i, i+1)
		}
	}
	p.wait = append(next, rest...)
	p.save()
	p.mu.Unlock()
	return p.Snapshot()
}
