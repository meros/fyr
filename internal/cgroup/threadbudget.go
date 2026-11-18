package cgroup

// Decision 0032 slice 3: each thread's memory.high is a share of the agents'
// budget, so one thread's undeclared work slows itself first. A slot raises the
// thread's limit by what its job is known to need; a job of unknown size runs
// without one. The kernel reclaims down to a lowered limit at once, and with
// swap that is a slowdown. Without swap room it is a livelock, so the limits are
// lifted while the agents' swap is nearly full.
//
// Every decision is logged for tuning (`guard: budget ...`). With the share off
// the same lines say what a share of shadowPercent would have met (mode=shadow),
// and nothing is written.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// holdMargin is how far above its learned peak a job may go unthrottled.
	holdMargin = 1.25
	// swapNearlyFull is the part of agents/ memory.swap.max at which the
	// limits are lifted.
	swapNearlyFull = 0.9
	// shadowPercent is the share the log measures against while the share is off.
	shadowPercent = 12
	// budgetLogEvery is how often a thread over its limit is logged, and the summary.
	budgetLogEvery = time.Minute
)

// ThreadHold is a thread's slot: the memory its job is known to need above
// the thread's own, when Known.
type ThreadHold struct {
	Peak  uint64
	Known bool
}

// budgetSeen is what the log remembers of a thread, under Tree.mu. The counters
// are from the start of the time the line will cover.
type budgetSeen struct {
	high, why      string
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

// ApplyThreadBudget writes each thread's memory.high for the slots held now
// and logs what changed and who is over. With the share off every thread is
// set back to no limit of its own, so turning it off restarts no agent.
func (t *Tree) ApplyThreadBudget(holds map[string]ThreadHold) {
	if t == nil {
		return
	}
	now := t.budgetClock()
	agents := filepath.Join(t.root, "agents")
	budget := readBytes(filepath.Join(agents, "memory.high"))
	if budget == 0 {
		budget = readBytes(filepath.Join(agents, "memory.max"))
	}
	share := budget * uint64(t.threadPercent) / 100
	mode, reference := "on", share
	if t.threadPercent == 0 {
		mode, reference = "shadow", budget*shadowPercent/100
	}
	swapUsed, swapLimit := readBytes(filepath.Join(agents, "memory.swap.current")), readBytes(filepath.Join(agents, "memory.swap.max"))
	lifted := share > 0 && swapLimit > 0 && float64(swapUsed) >= swapNearlyFull*float64(swapLimit)
	if lifted {
		share = 0
	}
	t.mu.Lock()
	changed := lifted != t.budgetLifted
	t.budgetLifted = lifted
	if t.budgetSeen == nil {
		t.budgetSeen = map[string]budgetSeen{}
	}
	t.mu.Unlock()
	if changed {
		state := "back"
		if lifted {
			state = "lifted"
		}
		t.logf("guard: budget limits %s: agents swap=%s of %s", state, inMB(swapUsed), inMB(swapLimit))
	}
	byName := make(map[string]ThreadHold, len(holds))
	for id, h := range holds {
		byName[unsafe.ReplaceAllString(id, "_")] = h
	}
	es, _ := os.ReadDir(agents)
	live := map[string]bool{}
	threads, limited, over := 0, 0, 0
	for _, e := range es {
		if !e.IsDir() || e.Name() == "probe" || e.Name() == DeployDir {
			continue
		}
		name := e.Name()
		live[name] = true
		threads++
		h, held := byName[name]
		want, why := threadHigh(share, budget, h, held, t.threadPercent > 0, lifted)
		if want != "max" {
			limited++
		}
		d := filepath.Join(agents, name)
		b, err := os.ReadFile(filepath.Join(d, "memory.high"))
		if strings.TrimSpace(string(b)) != want && (err == nil || want != "max") {
			if err := put(d, "memory.high", want); err != nil {
				t.logf("guard: budget could not limit thread=%s: %v", name, err)
			}
		}
		// Measured against the limit in force, or the shadow share while off.
		// A slot or full swap that lifted the limit leaves nothing to be over.
		ref := reference
		if want != "max" {
			ref, _ = strconv.ParseUint(want, 10, 64)
		} else if mode == "on" {
			ref = 0
		} else if held {
			ref = 0
		}
		if t.observeThread(now, name, d, want, why, mode, ref, h, held) {
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
		t.logf("guard: budget summary mode=%s share=%s budget=%s threads=%d limited=%d over=%d agents_mem=%s agents_swap=%s/%s",
			mode, inMB(reference), inMB(budget), threads, limited, over, inMB(readBytes(filepath.Join(agents, "memory.current"))), inMB(swapUsed), inMB(swapLimit))
	}
}

// observeThread logs a change of the thread's limit and, at most once per
// budgetLogEvery, that its working memory is over ref; it reports the latter.
func (t *Tree) observeThread(now time.Time, name, d, high, why, mode string, ref uint64, h ThreadHold, held bool) bool {
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
	moved := known && (s.high != high || s.why != why)
	first := !known && high != "max"
	was := s.high
	if !known {
		s.since, s.events, s.faults = now, events, faults
	}
	s.high, s.why = high, why
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

	if moved || first {
		if was == "" {
			was = "none"
		}
		t.logf("guard: budget limit thread=%s high=%s was=%s why=%s job_peak=%s mem=%s", name, high, was, why, inMB(h.Peak), inMB(current))
	}
	if logIt {
		rate := uint64(0)
		if secs := now.Sub(prev.since).Seconds(); secs >= 1 && faults >= prev.faults {
			rate = uint64(float64(faults-prev.faults) * float64(os.Getpagesize()) / secs)
		}
		p, _ := readPSI(filepath.Join(d, "memory.pressure"))
		slot := "none"
		if held && h.Known {
			slot = "known"
		} else if held {
			slot = "unknown"
		}
		t.logf("guard: budget over thread=%s mode=%s working=%s mem=%s limit=%s swap=%s read_back=%s/s high_events=+%d psi_some=%.0f psi_full=%.0f slot=%s",
			name, mode, inMB(working), inMB(current), inMB(ref), inMB(readBytes(filepath.Join(d, "memory.swap.current"))), inMB(rate), events-min(events, prev.events), p.some, p.full, slot)
	}
	return isOver
}

// threadHigh is the value for one thread, in whole pages as the kernel keeps
// it, and why.
func threadHigh(share, budget uint64, h ThreadHold, held, on, lifted bool) (string, string) {
	switch {
	case !on:
		return "max", "off"
	case lifted:
		return "max", "swap-full"
	case share == 0:
		return "max", "no-budget"
	case held && !h.Known:
		return "max", "slot-unknown-size"
	}
	v, why := share, "share"
	if held {
		v, why = v+uint64(holdMargin*float64(h.Peak)), "slot"
	}
	if v >= budget {
		return "max", "slot-fills-budget"
	}
	page := uint64(os.Getpagesize())
	return strconv.FormatUint(v/page*page, 10), why
}
