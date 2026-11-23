package cgroup

// V0.176: leftover processes. A thread's cgroup (agents/<thread>/ and its
// subgroups) holds its CLI and everything the CLI starts. A process there that
// is not the CLI or a descendant of it, or any process in the cgroup of a
// thread with no live CLI, is a leftover: a Chrome a layout run left, a
// mockup server, a port-forward of a thread that settled. Fyr shows them and
// ends them by policy (SPEC V0.176):
//
//   - a settled or archived thread's: at once;
//   - a cgroup no thread of this host has: after LeftoverGrace;
//   - a live thread's, once its run has ended: after LeftoverIdle, unless the
//     agent declared it (keep_process) or it runs under `timeout` (it keeps
//     its timeout);
//   - the owner's End ends one now, the owner's Keep holds one until it expires,
//     through a settle too.
//
// Nothing outside agents/ is read, self-deploy/ is never looked at, the CLI's
// tree is never a leftover, and a pid whose start time changed is never
// signalled. On a host without the cgroup tree (macOS) the tracker remembers
// each live CLI's descendants from the process table, and a remembered
// process that outlives its place under the CLI is that thread's leftover.

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/meros/fyr/internal/kvstore"
)

const (
	// LeftoverGrace: a process is shown as a leftover (and an orphan's is
	// ended) only when it has been one this long.
	LeftoverGrace = 2 * time.Minute
	// LeftoverIdle: a live thread's leftover ends this long after its run ended.
	LeftoverIdle = 30 * time.Minute
	// MaxKeep bounds a keep: the agent's and the owner's.
	MaxKeep = 7 * 24 * time.Hour
	// LeftoverTick is how often the host looks; a run's end and a settle look at once.
	LeftoverTick = time.Minute
)

// Leftover is one leftover process and its descendants (a root).
type Leftover struct {
	// ID is "<pid>@<start ticks>": it names this process and no later one with the pid.
	ID     string `json:"id"`
	Thread string `json:"thread"` // the cgroup's name under agents/: a thread id
	// Orphan: no thread of this host has this cgroup.
	Orphan  bool      `json:"orphan,omitempty"`
	PID     int       `json:"pid"`
	Name    string    `json:"name"`    // short: "chromium", "http.server"
	Command string    `json:"command"` // the root's command line, cut
	Procs   int       `json:"procs"`   // the root and its descendants
	RSS     uint64    `json:"rss"`     // their resident memory, bytes
	Started time.Time `json:"started"`
	Seen    time.Time `json:"seen"` // first seen as a leftover
	// Until is the end its own `timeout` sets.
	Until *time.Time `json:"until,omitempty"`
	Kept  *Keep      `json:"kept,omitempty"`
	// EndsAt is when the policy ends it, as it stands now; nil while it holds.
	EndsAt *time.Time `json:"endsAt,omitempty"`

	pids   []int
	starts map[int]uint64
}

// Keep holds a leftover: the agent's declaration (keep_process) or the
// owner's Keep. PID names a process (its root or one inside), Match a text in
// the root's command line.
type Keep struct {
	Thread string    `json:"thread"`
	PID    int       `json:"pid,omitempty"`
	Match  string    `json:"match,omitempty"`
	Until  time.Time `json:"until"`
	Why    string    `json:"why,omitempty"`
	By     string    `json:"by"` // "agent" or "owner"
}

func (k Keep) holds(l Leftover) bool {
	if k.Thread != l.Thread {
		return false
	}
	if k.PID != 0 && slices.Contains(l.pids, k.PID) {
		return true
	}
	return k.Match != "" && strings.Contains(l.Command, k.Match)
}

// ThreadState is what the tracker needs to know of a thread.
type ThreadState struct {
	Known   bool // a thread of this host has this id
	Asleep  bool // settled or archived
	Running bool // a run is live
}

// pinfo is one process of the table.
type pinfo struct {
	pid, ppid int
	start     uint64 // ticks after boot (a process table from ps: unix seconds)
	at        time.Time
	rss       uint64
	argv      []string
}

// ---- the tracker ----

// Leftovers tracks the host's leftovers, applies the policy and keeps the
// keeps. Tick is called by one goroutine; the views never wait.
type Leftovers struct {
	Tree *Tree // nil: the process-tree mode
	Proc string
	// PS gives `ps -A -o pid=,ppid=,rss=,etime=,args=` on a host without /proc.
	PS       func() (string, error)
	Now      func() time.Time
	CLIs     func() map[string]int
	Thread   func(name string) ThreadState
	KV       kvstore.Table
	TermWait time.Duration // SIGTERM to SIGKILL, default 5 s
	// OnEnd hears what was ended and why, per thread, outside the lock.
	OnEnd func(thread string, ended []Leftover, why string)
	Logf  func(string, ...any)
	// Signal sends the end's SIGTERM and SIGKILL; default syscall.Kill. A test
	// records with it what the tracker itself ended (V0.fix-flakes).
	Signal func(pid int, sig syscall.Signal) error

	tick      sync.Mutex // one Tick at a time
	mu        sync.Mutex
	seen      map[string]time.Time // leftover id → first seen
	idleSince map[string]time.Time // thread → when its run was last seen ended
	keeps     []Keep
	loaded    bool
	// The process-tree mode: each remembered process (pid@start) → its thread.
	owned map[string]string
	pub   atomic.Pointer[[]Leftover]
	all   atomic.Pointer[[]Leftover]
}

const (
	keepsKey  = "leftover-keeps"
	timersKey = "leftover-timers"
	// leftoverTimerTTL drops persisted seen/idleSince stamps older than a day.
	leftoverTimerTTL = 24 * time.Hour
)

type savedLeftoverTimers struct {
	Seen      map[string]time.Time `json:"seen,omitempty"`
	IdleSince map[string]time.Time `json:"idleSince,omitempty"`
}

func (l *Leftovers) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *Leftovers) logf(f string, a ...any) {
	if l.Logf != nil {
		l.Logf(f, a...)
	}
}

func (l *Leftovers) loadLocked() {
	if l.loaded {
		return
	}
	l.loaded = true
	if l.KV == nil {
		return
	}
	if raw, _ := l.KV.GetKV(keepsKey); raw != "" {
		json.Unmarshal([]byte(raw), &l.keeps)
	}
	if raw, _ := l.KV.GetKV(timersKey); raw != "" {
		var t savedLeftoverTimers
		if json.Unmarshal([]byte(raw), &t) == nil {
			cut := l.now().Add(-leftoverTimerTTL)
			if l.seen == nil {
				l.seen = map[string]time.Time{}
			}
			for id, at := range t.Seen {
				if at.After(cut) {
					l.seen[id] = at
				}
			}
			if l.idleSince == nil {
				l.idleSince = map[string]time.Time{}
			}
			for th, at := range t.IdleSince {
				if at.After(cut) {
					l.idleSince[th] = at
				}
			}
		}
	}
}

func (l *Leftovers) saveLocked() {
	if l.KV == nil {
		return
	}
	b, _ := json.Marshal(l.keeps)
	l.KV.SetKV(keepsKey, string(b))
}

func (l *Leftovers) saveTimersLocked() {
	if l.KV == nil {
		return
	}
	if len(l.seen) == 0 && len(l.idleSince) == 0 {
		l.KV.SetKV(timersKey, "")
		return
	}
	if b, err := json.Marshal(savedLeftoverTimers{Seen: l.seen, IdleSince: l.idleSince}); err == nil {
		l.KV.SetKV(timersKey, string(b))
	}
}

// View is the leftovers past the grace, the oldest first.
func (l *Leftovers) View() []Leftover {
	if p := l.pub.Load(); p != nil {
		return slices.Clone(*p)
	}
	return nil
}

// Pending is every leftover of the last Tick, in its grace too.
func (l *Leftovers) Pending() []Leftover {
	if p := l.all.Load(); p != nil {
		return slices.Clone(*p)
	}
	return nil
}

// Declare records the agent's (or the owner's) keep of a process of thread:
// by pid, or by a text of its command line, for d (at most MaxKeep).
func (l *Leftovers) Declare(thread string, pid int, match string, d time.Duration, why, by string) error {
	match = strings.TrimSpace(match)
	if pid <= 0 && len(match) < 3 {
		return errors.New("name the process: its pid, or at least 3 characters of its command line")
	}
	if d <= 0 {
		return errors.New("a keep needs a duration")
	}
	d = min(d, MaxKeep)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.loadLocked()
	l.keeps = append(l.keeps, Keep{Thread: thread, PID: pid, Match: match, Until: l.now().Add(d), Why: why, By: by})
	l.saveLocked()
	return nil
}

func (l *Leftovers) find(id string) (Leftover, bool) {
	for _, x := range l.Pending() {
		if x.ID == id {
			return x, true
		}
	}
	return Leftover{}, false
}

// Keep is the owner's Keep of a shown leftover, for d.
func (l *Leftovers) Keep(id string, d time.Duration) error {
	x, ok := l.find(id)
	if !ok {
		return errors.New("no such leftover process")
	}
	if err := l.Declare(x.Thread, x.PID, "", d, "kept by the owner", "owner"); err != nil {
		return err
	}
	l.republish()
	return nil
}

// End is the owner's End: it ends the leftover now and leaves a line.
func (l *Leftovers) End(id string) error {
	x, ok := l.find(id)
	if !ok {
		return errors.New("no such leftover process")
	}
	l.end(map[string][]Leftover{x.Thread: {x}}, "ended by the owner")
	l.Tick()
	return nil
}

// Forget drops the keeps of a thread's processes that ended, and expired keeps.
func (l *Leftovers) pruneLocked(now time.Time) {
	n := 0
	for _, k := range l.keeps {
		if k.Until.After(now) {
			l.keeps[n] = k
			n++
		}
	}
	if n != len(l.keeps) {
		l.keeps = l.keeps[:n]
		l.saveLocked()
	}
}

func (l *Leftovers) keepOf(x Leftover, now time.Time, owner bool) *Keep {
	for _, k := range l.keeps {
		if k.Until.After(now) && (k.By == "owner") == owner && k.holds(x) {
			k := k
			return &k
		}
	}
	return nil
}

// Tick scans, decides, ends and publishes. A run's end and a settle call it too.
func (l *Leftovers) Tick() {
	l.tick.Lock()
	defer l.tick.Unlock()
	now := l.now()
	clis := map[string]int{}
	if l.CLIs != nil {
		clis = l.CLIs()
	}
	var found []Leftover
	if l.Tree != nil {
		found = ScanLeftovers(l.Tree, l.Proc, clis)
	} else {
		found = l.scanTrees(clis, now)
	}
	state := func(name string) ThreadState {
		if l.Thread == nil {
			return ThreadState{Known: true}
		}
		return l.Thread(name)
	}
	states := map[string]ThreadState{}
	for _, x := range found {
		if _, ok := states[x.Thread]; !ok {
			states[x.Thread] = state(x.Thread)
		}
	}

	l.mu.Lock()
	l.loadLocked()
	l.pruneLocked(now)
	if l.seen == nil {
		l.seen = map[string]time.Time{}
	}
	if l.idleSince == nil {
		l.idleSince = map[string]time.Time{}
	}
	timersDirty := false
	ids := map[string]bool{}
	for i := range found {
		x := &found[i]
		ids[x.ID] = true
		if _, ok := l.seen[x.ID]; !ok {
			l.seen[x.ID] = now
			timersDirty = true
		}
		x.Seen = l.seen[x.ID]
	}
	for id := range l.seen {
		if !ids[id] {
			delete(l.seen, id)
			timersDirty = true
		}
	}
	for th, st := range states {
		switch {
		case st.Running:
			if _, had := l.idleSince[th]; had {
				delete(l.idleSince, th)
				timersDirty = true
			}
		case l.idleSince[th].IsZero():
			l.idleSince[th] = now
			timersDirty = true
		}
	}
	for th := range l.idleSince {
		if _, ok := states[th]; !ok {
			delete(l.idleSince, th)
			timersDirty = true
		}
	}
	if timersDirty {
		l.saveTimersLocked()
	}
	end := map[string][]Leftover{}
	why := map[string]string{}
	for i := range found {
		x := &found[i]
		st := states[x.Thread]
		x.Orphan = !st.Known
		if k := l.keepOf(*x, now, true); k != nil {
			x.Kept = k
			continue
		}
		var at time.Time
		reason := ""
		switch {
		case x.Orphan:
			at, reason = x.Seen.Add(LeftoverGrace), "in the cgroup of no thread"
		case st.Asleep:
			at, reason = x.Seen, "the thread settled"
		default:
			if k := l.keepOf(*x, now, false); k != nil {
				x.Kept = k
				continue
			}
			if st.Running || (x.Until != nil && x.Until.After(now)) {
				continue
			}
			from := l.idleSince[x.Thread]
			if x.Seen.After(from) {
				from = x.Seen
			}
			at, reason = from.Add(LeftoverIdle), fmt.Sprintf("idle %d min after the run ended", int(LeftoverIdle/time.Minute))
		}
		at2 := at
		x.EndsAt = &at2
		if !now.Before(at) {
			end[x.Thread] = append(end[x.Thread], *x)
			why[x.Thread] = reason
		}
	}
	l.mu.Unlock()

	for th, xs := range end {
		l.end(map[string][]Leftover{th: xs}, why[th])
	}
	ended := map[string]bool{}
	for _, xs := range end {
		for _, x := range xs {
			ended[x.ID] = true
		}
	}
	var all, shown []Leftover
	for _, x := range found {
		if ended[x.ID] {
			continue
		}
		all = append(all, x)
		if !now.Before(x.Seen.Add(LeftoverGrace)) {
			shown = append(shown, x)
		}
	}
	sort.SliceStable(shown, func(i, j int) bool { return shown[i].Seen.Before(shown[j].Seen) })
	l.all.Store(&all)
	l.pub.Store(&shown)
}

// republish refreshes the Kept marks of the published views after a keep.
func (l *Leftovers) republish() {
	now := l.now()
	l.mu.Lock()
	fix := func(xs []Leftover) []Leftover {
		for i := range xs {
			if k := l.keepOf(xs[i], now, true); k != nil {
				xs[i].Kept, xs[i].EndsAt = k, nil
			} else if k := l.keepOf(xs[i], now, false); k != nil && xs[i].Kept == nil {
				xs[i].Kept = k
			}
		}
		return xs
	}
	all, shown := fix(l.Pending()), fix(l.View())
	l.mu.Unlock()
	l.all.Store(&all)
	l.pub.Store(&shown)
}
