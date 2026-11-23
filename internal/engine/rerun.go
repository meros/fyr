package engine

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
)

const rerunKillsKey = "engine.rerunKills"

func (e *Engine) loadRerunKillsLocked(now time.Time) {
	if e.st == nil {
		return
	}
	raw, _ := e.st.GetKV(rerunKillsKey)
	if raw == "" {
		return
	}
	var m map[string][]time.Time
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	for entry, ks := range m {
		if ks = guard.Recent(ks, now); len(ks) > 0 {
			if e.gs.RerunKills == nil {
				e.gs.RerunKills = map[string][]time.Time{}
			}
			e.gs.RerunKills[entry] = ks
		}
	}
}

// V0.rerun-classes (decision 0036 (F)): a job of a class the owner marked
// rerunnable is killed instead of frozen (internal/guard decides when; its
// RerunAfter, RerunShort, RerunCap). Its thread is told to run it again, the
// gate holds that rerun until memory is fine and it fits, and its learned
// peak rises.

// RerunDeps are the rerunnable classes' measures and controls. All optional:
// without RerunMatch and JobKill nothing is killed.
type RerunDeps struct {
	// RerunMatch is the owner's entry that a job's heavy class or command
	// matches, "" for none.
	RerunMatch func(class, command string) string
	// RerunWall is the learned wall time (p90) of thread's admitted job's
	// class; false while unknown.
	RerunWall func(thread string) (time.Duration, bool)
	// JobKill kills thread's job (undeclared: its undeclared work) and
	// returns how many processes it killed.
	JobKill func(thread string, undeclared bool) int
	// RerunRaise raises the learned peak of thread's admitted job's class
	// (×1.2), before its admission goes back.
	RerunRaise func(thread string)
	// RerunSet is the owner's add or remove (host.rerunClasses).
	RerunSet func(args map[string]any) error
}

// RerunWaiting says thread's job was killed for a rerun that has not been
// admitted yet; fyrd's gate then names an undeclared command's class by its
// entry, so the rerun is admitted like a heavy job.
func (e *Engine) RerunWaiting(thread string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.rerunWait[thread]
	return ok
}

// rerunView is the classes killed in the last hour, for the front.
func (e *Engine) rerunView(now time.Time) []resource.GuardRerun {
	var out []resource.GuardRerun
	for entry, ks := range e.gs.RerunKills {
		ks = guard.Recent(ks, now)
		if len(ks) == 0 {
			continue
		}
		r := resource.GuardRerun{Class: entry, Kills: len(ks)}
		if len(ks) >= guard.RerunCap {
			t := ks[len(ks)-guard.RerunCap].Add(guard.RerunCapFor)
			r.CappedUntil = &t
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b resource.GuardRerun) int {
		if a.Class < b.Class {
			return -1
		}
		return 1
	})
	return out
}
