// Package lockwatch finds fyrd's stalls (SPEC 0.5 V0.147): mutexes that
// record who holds them and since when, operations (an HTTP request, an
// engine step) that record when they started, and a watchdog that writes
// every goroutine's stack to a file when one of them takes too long.
package lockwatch

import (
	"bytes"
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"weak"
)

// Mutex is a sync.Mutex that records its holder: the goroutine, the
// function that locked it and when. Its zero value is an unlocked mutex;
// its name is the type of the first method that locks it, such as
// "engine.(*Engine)", unless Name sets one.
type Mutex struct {
	mu   sync.Mutex
	held atomic.Pointer[Holder]
	reg  atomic.Bool
	// Critical marks the core lock: held past the watchdog's Exit limit,
	// fyrd writes the dump and exits so the launcher restarts it.
	Critical bool
	Name     string
}

// Holder is who holds a mutex, or runs an operation.
type Holder struct {
	Lock      string    // the mutex's name, or the operation's
	Goroutine uint64    // 0 when unknown
	Where     string    // the locking function, file:line
	Since     time.Time // when it was taken
	Critical  bool
	// Limit is an operation's own stall limit; 0 is the watchdog's.
	Limit time.Duration
	op    bool
}

// Held is how long h has been held at now.
func (h Holder) Held(now time.Time) time.Duration { return now.Sub(h.Since) }

func (h Holder) String() string {
	g := ""
	if h.Goroutine != 0 {
		g = fmt.Sprintf(" by goroutine %d", h.Goroutine)
	}
	kind := "lock"
	if h.op {
		kind = "operation"
	}
	return fmt.Sprintf("%s %s held %s%s at %s", kind, h.Lock, time.Since(h.Since).Round(time.Millisecond), g, h.Where)
}

var registry struct {
	mu    sync.Mutex
	locks []weak.Pointer[Mutex]
	ops   map[uint64]*Holder
	next  uint64
}

// Lock locks m and records the caller as its holder.
func (m *Mutex) Lock() {
	m.mu.Lock()
	m.taken(2)
}

// TryLock locks m if it is free.
func (m *Mutex) TryLock() bool {
	if !m.mu.TryLock() {
		return false
	}
	m.taken(2)
	return true
}

// Unlock unlocks m.
func (m *Mutex) Unlock() {
	m.held.Store(nil)
	m.mu.Unlock()
}

func (m *Mutex) taken(skip int) {
	pc, file, line, _ := runtime.Caller(skip)
	fn := ""
	if f := runtime.FuncForPC(pc); f != nil {
		fn = f.Name()
	}
	if !m.reg.Load() && m.reg.CompareAndSwap(false, true) {
		if m.Name == "" {
			m.Name = typeOf(fn)
		}
		registry.mu.Lock()
		registry.locks = slices.DeleteFunc(registry.locks, func(w weak.Pointer[Mutex]) bool { return w.Value() == nil })
		registry.locks = append(registry.locks, weak.Make(m))
		registry.mu.Unlock()
	}
	m.held.Store(&Holder{Lock: m.Name, Goroutine: goid(), Where: fmt.Sprintf("%s (%s:%d)", shortFn(fn), short(file), line), Since: time.Now(), Critical: m.Critical})
}

// Holder is m's holder now; false while m is free.
func (m *Mutex) Holder() (Holder, bool) {
	if h := m.held.Load(); h != nil {
		return *h, true
	}
	return Holder{}, false
}

// Start records an operation, such as "GET /api/load", until its done is
// called; the watchdog reports it when it runs longer than limit (0: the
// watchdog's Stall).
func Start(name string, limit time.Duration) (done func()) {
	h := &Holder{Lock: name, Goroutine: goid(), Since: time.Now(), Limit: limit, op: true}
	if _, file, line, ok := runtime.Caller(1); ok {
		h.Where = fmt.Sprintf("%s:%d", short(file), line)
	}
	registry.mu.Lock()
	if registry.ops == nil {
		registry.ops = map[uint64]*Holder{}
	}
	registry.next++
	id := registry.next
	registry.ops[id] = h
	registry.mu.Unlock()
	return func() {
		registry.mu.Lock()
		delete(registry.ops, id)
		registry.mu.Unlock()
	}
}

// Holders are every held watched mutex and every running operation, the
// longest first.
func Holders() []Holder {
	registry.mu.Lock()
	var out []Holder
	for _, w := range registry.locks {
		if m := w.Value(); m != nil {
			if h, ok := m.Holder(); ok {
				out = append(out, h)
			}
		}
	}
	for _, h := range registry.ops {
		out = append(out, *h)
	}
	registry.mu.Unlock()
	slices.SortFunc(out, func(a, b Holder) int { return a.Since.Compare(b.Since) })
	return out
}

// Stacks are every goroutine's stack.
func Stacks() []byte {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return buf[:n]
		}
		if len(buf) >= 64<<20 {
			return buf
		}
		buf = make([]byte, 2*len(buf))
	}
}

// goid is the calling goroutine's id, read from its stack's first line.
func goid() uint64 {
	var b [64]byte
	s := b[:runtime.Stack(b[:], false)]
	s, _ = bytes.CutPrefix(s, []byte("goroutine "))
	if i := bytes.IndexByte(s, ' '); i > 0 {
		id, _ := strconv.ParseUint(string(s[:i]), 10, 64)
		return id
	}
	return 0
}

// typeOf is "engine.(*Engine)" for "github.com/x/internal/engine.(*Engine).apply".
func typeOf(fn string) string {
	fn = shortFn(fn)
	if i := strings.Index(fn, ")."); i > 0 {
		return fn[:i+1]
	}
	if i := strings.LastIndexByte(fn, '.'); i > 0 {
		return fn[:i]
	}
	return fn
}

// shortFn drops a function's package path: "engine.(*Engine).apply".
func shortFn(fn string) string {
	if i := strings.LastIndexByte(fn, '/'); i >= 0 {
		return fn[i+1:]
	}
	return fn
}

// short drops the module path: "internal/engine/slots.go" → "engine/slots.go".
func short(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		if j := strings.LastIndexByte(s[:i], '/'); j >= 0 {
			return s[j+1:]
		}
	}
	return s
}
