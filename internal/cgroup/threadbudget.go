package cgroup

// Decision 0032 slice 3, as V0.share-shadow leaves it: a share of the agents'
// budget per thread is only measured, never enforced. The per-job limits
// (V0.job-limits) bound a job; a thread's own memory.high is not written. The
// log says what a share of shadowPercent would have met (`guard: budget over
// ... mode=shadow`, `guard: budget summary ...`): the tuning data decision 0032
// asks for.

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// holdMargin is how far above its learned peak a job may go unthrottled
	// (V0.job-limits).
	holdMargin = 1.25
	// shadowPercent is the share of the agents' budget the log measures against.
	shadowPercent = 12
	// budgetLogEvery is how often a thread over the share is logged, and the summary.
	budgetLogEvery = time.Minute
)

// budgetSeen is what the log remembers of a thread, under Tree.mu. The counters
// are from the start of the time the line will cover.
type budgetSeen struct {
	logged, since  time.Time
	events, faults uint64
}

func (t *Tree) budgetClock() time.Time {
	if t.budgetNow != nil {
		return t.budgetNow()
	}
	return time.Now()
}

func inMB(n uint64) string { return fmt.Sprintf("%dMB", n>>20) }

// ShadowThreadBudget logs which threads' working memory is over the shadow
// share. It writes nothing (V0.share-shadow). A thread holding a slot (held,
// by thread ID) runs admitted work and is not called over.
func (t *Tree) ShadowThreadBudget(held map[string]bool) {
	if t == nil {
		return
	}
	now := t.budgetClock()
	agents := t.agentsDir()
	budget := readBytes(filepath.Join(agents, "memory.high"))
	if budget == 0 {
		budget = readBytes(filepath.Join(agents, "memory.max"))
	}
	reference := budget * shadowPercent / 100
	t.mu.Lock()
	if t.budgetSeen == nil {
		t.budgetSeen = map[string]budgetSeen{}
	}
	t.mu.Unlock()
	byName := make(map[string]bool, len(held))
	for id := range held {
		byName[unsafe.ReplaceAllString(id, "_")] = true
	}
	live := map[string]bool{}
	threads, over := 0, 0
	for _, c := range t.threadCgroups() {
		name := c.Name
		// A thread with two cgroups (a handover, or one from before the
		// switch to scopes) is measured where its newest process runs.
		if name == DeployDir || live[name] || c.Dir != t.threadDir(name) {
			continue
		}
		live[name] = true
		threads++
		ref := reference
		if byName[name] {
			ref = 0
		}
		if t.observeThread(now, name, c.Dir, ref) {
			over++
		}
	}
	t.mu.Lock()
	for name := range t.budgetSeen {
		if !live[name] {
			delete(t.budgetSeen, name)
		}
	}
	summary := over > 0 && now.Sub(t.budgetSummary) >= budgetLogEvery
	if summary {
		t.budgetSummary = now
	}
	t.mu.Unlock()
	if summary {
		t.logf("guard: budget summary mode=shadow share=%s budget=%s threads=%d over=%d agents_mem=%s agents_swap=%s/%s",
			inMB(reference), inMB(budget), threads, over, inMB(readBytes(filepath.Join(agents, "memory.current"))),
			inMB(readBytes(filepath.Join(agents, "memory.swap.current"))), inMB(readBytes(filepath.Join(agents, "memory.swap.max"))))
	}
}

// observeThread logs, at most once per budgetLogEvery, that the thread's
// working memory is over ref, and reports whether it is.
func (t *Tree) observeThread(now time.Time, name, d string, ref uint64) bool {
	current := readBytes(filepath.Join(d, "memory.current"))
	working := current
	if in, ok := readKey(filepath.Join(d, "memory.stat"), "inactive_file"); ok && in <= current {
		working -= in
	}
	events, _ := readKey(filepath.Join(d, "memory.events"), "high")
	faults, _ := readKey(filepath.Join(d, "memory.stat"), "pgmajfault")
	isOver := ref > 0 && working > ref

	t.mu.Lock()
	s, known := t.budgetSeen[name]
	if !known {
		s.since, s.events, s.faults = now, events, faults
	}
	logIt := isOver && now.Sub(s.logged) >= budgetLogEvery
	prev := s
	switch {
	case logIt:
		s.logged, s.since, s.events, s.faults = now, now, events, faults
	case !isOver:
		s.since, s.events, s.faults = now, events, faults
	}
	t.budgetSeen[name] = s
	t.mu.Unlock()

	if logIt {
		rate := uint64(0)
		if secs := now.Sub(prev.since).Seconds(); secs >= 1 && faults >= prev.faults {
			rate = uint64(float64(faults-prev.faults) * float64(os.Getpagesize()) / secs)
		}
		p, _ := readPSI(filepath.Join(d, "memory.pressure"))
		t.logf("guard: budget over thread=%s mode=shadow working=%s mem=%s limit=%s swap=%s read_back=%s/s high_events=+%d psi_some=%.0f psi_full=%.0f",
			name, inMB(working), inMB(current), inMB(ref), inMB(readBytes(filepath.Join(d, "memory.swap.current"))), inMB(rate), events-min(events, prev.events), p.some, p.full)
	}
	return isOver
}
