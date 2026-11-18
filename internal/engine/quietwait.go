package engine

import (
	"log"
	"time"

	"github.com/meros/fyr/internal/core"
)

// SPEC 0.5 V0.195: a turn ended on a background command, and the thread's
// cgroup, which holds the command and anything it or the agent detached, has
// gone quiet. On 18 Nov a hand-written wait outlived the experiment it waited
// for and the thread read as waiting for 17 min.
const (
	// QuietAfter is how long the thread must stay quiet.
	QuietAfter = 5 * time.Minute
	// QuietWindow is one measure: the average over it, so that a burst
	// between two looks counts.
	QuietWindow = time.Minute
	// QuietCores is the line. Idle threads with their CLI measured 0.004 to
	// 0.03 cores; a poll every 15 s adds little.
	QuietCores = 0.05
)

// quietWait is one thread's CPU watch.
type quietWait struct {
	at    time.Time     // the last reading
	usage time.Duration // its CPU time
	since time.Time     // quiet since
}

// sweepQuietWaits runs on the tick. It reads the cgroups outside e.mu.
func (e *Engine) sweepQuietWaits() {
	if e.ThreadCPUTime == nil {
		return
	}
	e.mu.Lock()
	now := e.Now()
	if e.quietWaits == nil {
		e.quietWaits = map[string]*quietWait{}
	}
	for id := range e.quietWaits {
		if t := e.threads[id]; t == nil || len(core.QuietCandidates(t)) == 0 {
			delete(e.quietWaits, id)
		}
	}
	var look []string
	for id, t := range e.threads {
		if len(core.QuietCandidates(t)) == 0 {
			continue
		}
		if w := e.quietWaits[id]; w == nil || now.Sub(w.at) >= QuietWindow {
			look = append(look, id)
		}
	}
	e.mu.Unlock()
	for _, id := range look {
		usage, ok := e.ThreadCPUTime(id)
		frozen := e.ThreadFrozen != nil && e.ThreadFrozen(id)
		if !ok {
			continue
		}
		e.mu.Lock()
		args := e.quietStep(id, usage, frozen, now)
		e.mu.Unlock()
		if args == nil {
			continue
		}
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindTaskQuiet, Thread: id, Args: *args}); err != nil {
			log.Printf("thread %s: quiet wait: %v", id, err)
		}
	}
}

// quietStep takes one reading under e.mu and returns what to record, or nil.
func (e *Engine) quietStep(id string, usage time.Duration, frozen bool, now time.Time) *core.TaskQuietArgs {
	t := e.threads[id]
	if t == nil {
		return nil
	}
	tasks := core.QuietCandidates(t)
	if len(tasks) == 0 {
		return nil
	}
	w := e.quietWaits[id]
	// A long gap (a suspended host, a stalled tick) measures nothing.
	if w == nil || now.Sub(w.at) > 3*QuietWindow {
		e.quietWaits[id] = &quietWait{at: now, usage: usage, since: now}
		return nil
	}
	gap := now.Sub(w.at)
	cores := float64(usage-w.usage) / float64(gap)
	w.at, w.usage = now, usage
	if frozen { // frozen work uses no CPU and is not stuck
		w.since = now
		return nil
	}
	if cores >= QuietCores {
		w.since = now
		for _, k := range tasks {
			if k.QuietSince != nil {
				return &core.TaskQuietArgs{}
			}
		}
		return nil
	}
	since := w.since
	if t.RunEndedAt != nil && t.RunEndedAt.After(since) {
		since = *t.RunEndedAt
	}
	if now.Sub(since) < QuietAfter {
		return nil
	}
	var ids []string
	for _, k := range tasks {
		if (k.QuietSince == nil || !k.QuietTold) && k.StartedAt != nil && !k.StartedAt.After(now.Add(-QuietAfter)) {
			ids = append(ids, k.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return &core.TaskQuietArgs{IDs: ids, Since: &since}
}
