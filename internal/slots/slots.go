// Package slots runs at most N heavy jobs (test suites, builds, e2e runs)
// at once across a host's threads (SPEC 0.5 V0.86). The others wait in a
// queue: first come, first served within a project priority (a new waiter
// goes ahead of the lower ones, V0.project-priority), a priority waiter (the
// self-deploy's tests) ahead of the rest. Nobody takes a slot from its holder. N 0 is
// off: every acquire holds at once and nothing is kept. The holders and
// the queue live in the store's KV table, so they survive a restart.
package slots

import (
	"context"
	"encoding/json"
	"github.com/meros/fyr/internal/kvstore"
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

// Holder is one owner (a thread id, or the self-deploy) holding or waiting.
type Holder struct {
	Owner    string    `json:"owner"`
	Kind     string    `json:"kind"`
	Since    time.Time `json:"since"`
	Priority bool      `json:"priority,omitempty"`
	// V0.163, in a snapshot: the job's need (learned or live: cores and
	// bytes) as percentages of the host, CPU and memory, and Pct the larger
	// of the two; for a waiter, what it will need. Need is nil without
	// Pool.Cost. Cost is the old one-number form (Pct of N), kept one
	// release for older fronts.
	Need   *Need   `json:"need,omitempty"`
	CPUPct float64 `json:"cpuPct,omitempty"`
	MemPct float64 `json:"memPct,omitempty"`
	Pct    float64 `json:"pct,omitempty"`
	Cost   float64 `json:"cost,omitempty"`

	ready chan struct{} // a Wait's: closed on the grant
}

// Undeclared is a thread that runs heavy work Fyr did not admit (V0.164): its
// use above its idle level, counted in the budget beside the admitted jobs.
// The host's detector makes the list (Pool.Undeclared); the pool adds the
// shares of the host.
type Undeclared struct {
	Thread  string    `json:"thread"`
	Name    string    `json:"name,omitempty"` // the thread's project, for the wait reason
	CPU     float64   `json:"cpu"`            // cores
	Mem     uint64    `json:"mem"`            // bytes
	CPUPct  float64   `json:"cpuPct"`
	MemPct  float64   `json:"memPct"`
	Since   time.Time `json:"since"`
	Command string    `json:"command,omitempty"`
	Paused  bool      `json:"paused"`
	Queued  bool      `json:"queued,omitempty"`
	// OverCPU and OverMem: the measure is at least a tenth of its
	// threshold, so the row names it.
	OverCPU bool `json:"overCpu"`
	OverMem bool `json:"overMem"`
	// MemOver: the memory itself is over its threshold (the freeze takes these).
	MemOver bool `json:"-"`
	Matched bool `json:"matched,omitempty"`
	// Kind and Window are for the agent's message, not the front.
	Kind   string        `json:"-"`
	Window time.Duration `json:"-"`
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
	// V0.163, in percent of what the host gives its agents, per resource:
	// the jobs in use, the budget (AIMD moves it), and what is free in it.
	// UsedPct is the tighter resource's use of its budget (the rail's bar)
	// and Tight names it; Binding names what holds the head back; NeedPct
	// is the head's larger need. Without measures (no Pool.Host) only
	// UsedPct, a share of the fixed count, is set.
	CPUPct       *float64 `json:"cpuPct,omitempty"`
	MemPct       *float64 `json:"memPct,omitempty"`
	CPUBudgetPct *float64 `json:"cpuBudgetPct,omitempty"`
	MemBudgetPct *float64 `json:"memBudgetPct,omitempty"`
	CPUFreePct   *float64 `json:"cpuFreePct,omitempty"`
	MemFreePct   *float64 `json:"memFreePct,omitempty"`
	UsedPct      *float64 `json:"usedPct,omitempty"`
	Tight        string   `json:"tight,omitempty"`
	Binding      string   `json:"binding,omitempty"`
	NeedPct      *float64 `json:"needPct,omitempty"`
	// V0.164: the threads that run heavy work Fyr did not admit, and their
	// share of the host; the used percentages above include them.
	Undeclared       []Undeclared `json:"undeclared,omitempty"`
	UndeclaredCPUPct *float64     `json:"undeclaredCpuPct,omitempty"`
	UndeclaredMemPct *float64     `json:"undeclaredMemPct,omitempty"`
	// Decision 0032: what the memory guard did lately.
	Guard *Guard `json:"guard,omitempty"`
	// bound is Binding with ResHeadroom told apart (V0.admission-memory).
	bound string
}

// Bound is the resource that holds the head back as the AIMD controller
// reads it: Binding, with ResHeadroom where the host's real headroom binds
// and not the memory budget (V0.admission-memory).
func (s Snapshot) Bound() string { return s.bound }

// Pool is one host's slots.
type Pool struct {
	// OnGrant hears a queued Acquire's owner get its slot later, outside
	// the lock; a Wait's grant is its return instead.
	OnGrant func(owner, kind string)
	Now     func() time.Time
	// Admit decides whether head may start beside held (decision 0029 S2),
	// and why not; nil is the count rule. It runs under the pool's lock and
	// must not call the pool.
	// It also names the resource that binds (V0.163: ResCPU or ResMem;
	// V0.admission-memory: ResHeadroom).
	Admit func(head Holder, held []Holder) (bool, string, string)
	// Cost is a holder's need (learned, or live while it runs) for the
	// snapshot; optional. Host is the host's measures and capacity (V0.163:
	// the percentages); optional. Both run under the pool's lock.
	Cost func(h Holder) Need
	Host func() Signals
	// Undeclared lists the threads that run heavy work without an admission
	// (V0.164), for the snapshot; optional, under the pool's lock, so it reads
	// a published view and waits for nothing.
	Undeclared func() []Undeclared
	// Guard is the memory guard's published view (decision 0032), for the
	// snapshot; optional, under the pool's lock, so it reads a published copy.
	Guard func() *Guard
	// Backfill decides whether c, behind a head that does not fit, may
	// start now without delaying it (decision 0029 S3, EASY); nil never.
	// Under the pool's lock, like Admit.
	Backfill func(head, c Holder, held []Holder) bool
	// Rank is an owner's project priority (V0.project-priority): 1 high, 0
	// normal, -1 low; nil ranks all alike. It places a new waiter, under the
	// pool's lock, so it must read a published value and not call the pool.
	Rank func(owner string) int

	mu   lockwatch.Mutex
	pub  atomic.Pointer[Snapshot] // the last snapshot, for View
	n    int
	kv   kvstore.Table
	held []Holder
	wait []Holder
	why  string
	// binding is the resource that holds the head back (V0.163).
	binding string
	// Decision 0029 S4, V0.163 per resource: the budget factors (zero bits
	// is Full), why, and the last grant. The bits are atomic, since Admit
	// reads Budget under p.mu.
	budgetWhy string
	lastGrant time.Time
	cpuBits   atomic.Uint64
	memBits   atomic.Uint64
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
func Open(n int, kv kvstore.Table) *Pool {
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
	asked := len(p.wait) == 0 // h is the head and was just refused
	p.wait = slices.Insert(p.wait, p.placeLocked(owner), h)
	// Decision 0029 S3: it may start at once behind a head that does not fit.
	// V0.admission-tidy: the head is asked first unless it was just refused, so
	// a new job never backfills past a head that fits now (and uses one of its
	// passes).
	var granted []Holder
	if asked {
		granted = p.backfillLocked(nil)
	} else {
		granted = p.promoteLocked()
	}
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

// RankAfter is how long a waiter waits before a later one of a higher
// project priority may no longer go ahead of it (V0.project-priority): a low
// project's job is late, never starved.
const RankAfter = 20 * time.Minute

// placeLocked is where a new waiter of owner goes in the queue
// (V0.project-priority): behind the priority waits, every waiter of its own
// project priority or higher, and every waiter that has waited RankAfter;
// ahead of the rest. Without Rank, or with every waiter ranked alike, that
// is the end. The waiters already queued keep their order, so the owner's
// order (V0.137) stands. Call it with p.mu held.
func (p *Pool) placeLocked(owner string) int {
	if p.Rank == nil {
		return len(p.wait)
	}
	mine, now := p.Rank(owner), p.now()
	at := 0
	for i, w := range p.wait {
		if w.Priority || p.Rank(w.Owner) >= mine || now.Sub(w.Since) >= RankAfter {
			at = i + 1
		}
	}
	return at
}

// TryAcquire takes a slot for owner only if one is free for it now, and never
// queues it: a learned command (V0.209) must not wait. Whoever waits goes first.
func (p *Pool) TryAcquire(owner, kind string) bool {
	if p.Off() {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if index(p.held, owner) >= 0 {
		return true
	}
	h := Holder{Owner: owner, Kind: kind, Since: p.now()}
	if len(p.wait) > 0 || !p.admitLocked(h) {
		return false
	}
	defer p.save()
	p.held = append(p.held, h)
	p.lastGrant = h.Since
	return true
}

// Grow admits owner, which holds a slot, again for a heavier job of kind
// (V0.admission-tidy): a thread that holds one for `go test ./...` and starts
// `yarn e2e` was let through unchecked. Its need is decided beside the other
// holders (its own hold left out), which admits the difference. When it does
// not fit, owner waits in the queue as a new job does and keeps its hold
// meanwhile; the grant replaces the hold. held: it fits now; else ahead is how
// many wait before it.
func (p *Pool) Grow(owner, kind string) (held bool, ahead int) {
	if p.Off() {
		return true, 0
	}
	p.mu.Lock()
	if index(p.held, owner) < 0 {
		p.mu.Unlock()
		return false, -1
	}
	if i := index(p.wait, owner); i >= 0 {
		p.mu.Unlock()
		return false, i
	}
	h := Holder{Owner: owner, Kind: kind, Since: p.now()}
	if len(p.wait) == 0 && p.admitLocked(h) {
		p.mu.Unlock()
		return true, 0
	}
	asked := len(p.wait) == 0
	p.wait = slices.Insert(p.wait, p.placeLocked(owner), h)
	// A queue whose holders all wait to grow may now admit its head (the
	// progress rule in admitLocked).
	var told []Holder
	if !asked {
		for _, g := range p.promoteLocked() {
			if g.Owner == owner {
				held = true
			} else {
				told = append(told, g)
			}
		}
	}
	p.save()
	ahead = index(p.wait, owner)
	on := p.OnGrant
	p.mu.Unlock()
	if on != nil {
		for _, g := range told {
			on(g.Owner, g.Kind)
		}
	}
	if held {
		return true, 0
	}
	return false, ahead
}

// Take gives owner's held slot back without handing it on and calls nobody,
// so it is safe under a caller's lock; the caller then calls Fill.
func (p *Pool) Take(owner string) bool {
	if p.Off() {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	i := index(p.held, owner)
	if i < 0 {
		return false
	}
	defer p.save()
	p.held = slices.Delete(p.held, i, i+1)
	return true
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
	}
	// V0.admission-tidy: a holder may also wait to Grow.
	if i := index(p.wait, owner); i >= 0 {
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
	if j := index(p.held, h.Owner); j >= 0 {
		p.held[j] = h // V0.admission-tidy: a Grow's grant replaces the hold
		return told
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
		if p.Backfill(head, p.wait[i], p.othersLocked(p.wait[i].Owner)) {
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
	others := p.othersLocked(head.Owner)
	// V0.admission-tidy: when the head holds a slot and waits to grow, and every
	// other holder waits to grow too, no holder runs ungrown: each one's decision
	// would count the others' holds, and none would ever be admitted. The head is
	// then decided as with nothing held (one job always runs), the memory rules
	// still applying.
	if index(p.held, head.Owner) >= 0 && !slices.ContainsFunc(others, func(h Holder) bool { return index(p.wait, h.Owner) < 0 }) {
		others = nil
	}
	if p.Admit == nil {
		ok := len(others) < p.n
		p.why = ""
		return ok
	}
	ok, why, binding := p.Admit(head, others)
	p.why, p.binding = why, binding
	if ok {
		p.why, p.binding = "", ""
	}
	return ok
}

// othersLocked is a copy of the holders without owner's own hold: a holder
// that waits to Grow is decided beside the others (V0.admission-tidy). Call it
// with p.mu held.
func (p *Pool) othersLocked(owner string) []Holder {
	return slices.DeleteFunc(slices.Clone(p.held), func(h Holder) bool { return h.Owner == owner })
}

// Budget is the budget per resource as a share of the host: Full, or what
// S4's controller set (V0.163). It takes no lock: Admit calls it under p.mu.
func (p *Pool) Budget() Factors {
	f := Full
	if b := p.cpuBits.Load(); b != 0 {
		f.CPU = math.Float64frombits(b)
	}
	if b := p.memBits.Load(); b != 0 {
		f.Mem = math.Float64frombits(b)
	}
	return f
}

// N is the host's `slots` setting: the fixed count without measures, and
// what the prior and the AIMD floor are made of.
func (p *Pool) N() int { return p.n }

// SetBudget sets the budget and why (decision 0029 S4), and grants the
// queue what now fits.
func (p *Pool) SetBudget(f Factors, why string) {
	if p.Off() {
		return
	}
	p.mu.Lock()
	p.budgetWhy = why
	p.cpuBits.Store(math.Float64bits(f.CPU))
	p.memBits.Store(math.Float64bits(f.Mem))
	p.fillLocked()
}

// Binding is the resource that holds the queue's head back, "" when none,
// as the snapshot sends it.
func (p *Pool) Binding() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Wire(p.binding)
}

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
	if p.Guard != nil {
		s.Guard = p.Guard()
	}
	if len(s.Waiting) > 0 {
		s.Why = p.why
	}
	if len(s.Waiting) > 0 {
		s.Binding, s.bound = Wire(p.binding), p.binding
	}
	n := float64(p.n)
	f := p.Budget()
	if f != Full {
		b := max(f.CPU, f.Mem) * n // the old one-number budget, for older fronts
		s.Budget, s.BudgetWhy = &b, p.budgetWhy
	}
	pct := func(v float64) *float64 { return &v }
	var host Signals
	if p.Host != nil {
		host = p.Host()
	}
	measured := p.Cost != nil && host.CPU > 0 && host.Memory > 0
	if !measured {
		// Without measures: the fixed count, each job 1/N.
		for i := range s.Held {
			s.Held[i].Pct = Pct(1 / n)
		}
		for i := range s.Waiting {
			s.Waiting[i].Pct = Pct(1 / n)
		}
		s.UsedPct = pct(Pct(float64(len(s.Held)) / n))
		if len(s.Waiting) > 0 {
			s.NeedPct = pct(s.Waiting[0].Pct)
		}
		pub := s
		pub.Held, pub.Waiting = slices.Clone(s.Held), slices.Clone(s.Waiting)
		p.pub.Store(&pub)
		return s
	}
	fill := func(h *Holder) Need {
		need := p.Cost(*h)
		c, m := need.Shares(host)
		h.Need, h.CPUPct, h.MemPct = &need, Pct(c), Pct(m)
		h.Pct = max(h.CPUPct, h.MemPct)
		h.Cost = float64(int(max(c, m)*n*100+0.5)) / 100
		return need
	}
	var used Need
	old := 0.0
	for i := range s.Held {
		used = used.Plus(fill(&s.Held[i]))
		old += s.Held[i].Cost
	}
	if p.Undeclared != nil {
		var ux Need
		for _, u := range p.Undeclared() {
			c, m := Need{CPU: u.CPU, Mem: u.Mem}.Shares(host)
			u.CPUPct, u.MemPct = Pct(c), Pct(m)
			s.Undeclared = append(s.Undeclared, u)
			ux = ux.Plus(Need{CPU: u.CPU, Mem: u.Mem})
		}
		if len(s.Undeclared) > 0 {
			c, m := ux.Shares(host)
			s.UndeclaredCPUPct, s.UndeclaredMemPct = pct(Pct(c)), pct(Pct(m))
			used = used.Plus(ux)
		}
	}
	for i := range s.Waiting {
		fill(&s.Waiting[i])
	}
	s.Used = pct(float64(int(old*100+0.5)) / 100)
	uc, um := used.Shares(host)
	s.CPUPct, s.MemPct = pct(Pct(uc)), pct(Pct(um))
	s.CPUBudgetPct, s.MemBudgetPct = pct(Pct(f.CPU)), pct(Pct(f.Mem))
	s.CPUFreePct, s.MemFreePct = pct(max(0, Pct(f.CPU-uc))), pct(max(0, Pct(f.Mem-um)))
	rc, rm := uc/max(f.CPU, 1e-9), um/max(f.Mem, 1e-9)
	if rm > rc {
		s.UsedPct, s.Tight = pct(Pct(rm)), ResMem
	} else {
		s.UsedPct, s.Tight = pct(Pct(rc)), ResCPU
	}
	if len(s.Waiting) > 0 {
		s.NeedPct = pct(s.Waiting[0].Pct)
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
