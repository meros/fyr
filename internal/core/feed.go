package core

// The agent feed (decision 0020 part 5, #47): the only way Fyr speaks to the
// thread's agent. Notice rules project each reduce (its events and the
// records before and after) into Notices; the thread keeps one queue of
// them, merged by coalesce key; the agent manager takes them at three
// points: when the agent is idle (a message that starts a turn), at a tool
// call (context) and at the run's end (the Stop hook's one reason).

import (
	"slices"
	"strings"
	"time"
)

// Urgency is when a notice reaches the agent.
type Urgency string

const (
	// Wake is delivered now: a turn starts when the agent is idle.
	Wake Urgency = "wake"
	// Next is delivered at the next tool call or turn; it never wakes an
	// idle agent (V0.95 (1), decision 0024 section 8).
	Next Urgency = "next"
	// Stop is delivered at the run's end and blocks the Stop once.
	Stop Urgency = "stop"
	// Silent is only in the log and in fyr_state: never queued.
	Silent Urgency = "silent"
)

// rank orders urgencies for a merge: the earlier delivery wins.
func (u Urgency) rank() int {
	switch u {
	case Wake:
		return 3
	case Next:
		return 2
	case Stop:
		return 1
	}
	return 0
}

// Notice is one thing the agent is told: one sentence about one record.
type Notice struct {
	// About is the record: "thread", "view", "change:<id>" or "rule:<id>".
	About   string  `json:"about"`
	What    string  `json:"what"`
	Urgency Urgency `json:"urgency"`
	Key     string  `json:"coalesceKey"`
	// Subject and States make a coalesced line: "CI on PR #12: failed →
	// passed". A notice without a subject is replaced by a newer one.
	Subject string   `json:"subject,omitempty"`
	States  []string `json:"states,omitempty"`
	// Self: the agent's own Input caused it (no loop), or it needs nothing
	// from the agent (the owner's settle, V0.89), so it never wakes the
	// agent; it goes out with the next tool call or delivery.
	Self bool `json:"self,omitempty"`
	// Budget: an observer's or a Rule's wake, which counts against the
	// thread's wake budget (V0.95 (3)).
	Budget bool      `json:"budget,omitempty"`
	At     time.Time `json:"at"`
	// said is the dedupe key (change, field, head) of a fact notice, and
	// state what it says (V0.95 (2)); set by the rule, never stored.
	said, state string
}

func (n *Notice) render() {
	if n.Subject != "" {
		n.What = n.Subject + ": " + strings.Join(n.States, " → ") + "."
	}
}

// Feed is the thread's queue of notices not delivered yet.
type Feed struct {
	Queue []Notice `json:"queue,omitempty"`
	// Told is the last delivery per coalesce key: the interval of the
	// mid-turn reminders.
	Told map[string]time.Time `json:"told,omitempty"`
	// Said is the last state said per (change, field, head) (V0.95 (2)): a
	// run does not clear it; it keeps the newest maxSaid keys.
	Said map[string]Said `json:"said,omitempty"`
	// Woke are the times an observer's or a Rule's notice woke the agent
	// within the last wakeWindow (V0.95 (3)).
	Woke []time.Time `json:"woke,omitempty"`
}

// Said is one fact said to the agent: its state and when.
type Said struct {
	State string    `json:"state"`
	At    time.Time `json:"at"`
}

const (
	maxQueue = 50
	maxSaid  = 200
	// The wake budget (V0.95 (3)): at most wakeBudget wakes from observers
	// and Rules per wakeWindow.
	wakeBudget = 5
	wakeWindow = 10 * time.Minute
)

// said reports a fact notice said before on the same head with the same
// state, and otherwise records it.
func (f *Feed) said(n Notice, now time.Time) bool {
	if n.said == "" {
		return false
	}
	if s, ok := f.Said[n.said]; ok && s.State == n.state {
		return true
	}
	if f.Said == nil {
		f.Said = map[string]Said{}
	}
	f.Said[n.said] = Said{State: n.state, At: now}
	for len(f.Said) > maxSaid {
		old := ""
		for k, s := range f.Said {
			if old == "" || s.At.Before(f.Said[old].At) || (s.At.Equal(f.Said[old].At) && k < old) {
				old = k
			}
		}
		delete(f.Said, old)
	}
	return false
}

// woke are the budgeted wakes within the window before now.
func (f *Feed) woke(now time.Time) []time.Time {
	return slices.DeleteFunc(slices.Clone(f.Woke), func(at time.Time) bool { return now.Sub(at) >= wakeWindow })
}

// budgetLeft reports whether an observer's or a Rule's notice may wake the
// agent now.
func (f *Feed) budgetLeft(now time.Time) bool { return len(f.woke(now)) < wakeBudget }

// waking reports a notice that may start a turn now.
func (f *Feed) waking(n Notice, now time.Time) bool {
	return n.Urgency == Wake && !n.Self && (!n.Budget || f.budgetLeft(now))
}

// FreeAt is when the wake budget frees for a wake it holds back, or nil
// (V0.95 (3)): the thread's NextCheck, so the clock delivers it then.
func (f *Feed) FreeAt(now time.Time) *time.Time {
	held := slices.ContainsFunc(f.Queue, func(n Notice) bool { return n.Urgency == Wake && !n.Self && n.Budget })
	w := f.woke(now)
	if !held || len(w) < wakeBudget {
		return nil
	}
	slices.SortFunc(w, func(a, b time.Time) int { return a.Compare(b) })
	at := w[len(w)-wakeBudget].Add(wakeWindow)
	return &at
}

// Enqueue adds a notice, merging it into a pending one with its key.
func (f *Feed) Enqueue(n Notice) {
	if n.Urgency == Silent || n.Urgency == "" {
		return
	}
	n.render()
	for i := range f.Queue {
		q := &f.Queue[i]
		if q.Key != n.Key {
			continue
		}
		switch {
		case n.Urgency.rank() > q.Urgency.rank():
			q.Urgency, q.Budget = n.Urgency, n.Budget
		case n.Urgency == q.Urgency:
			q.Budget = q.Budget && n.Budget
		}
		q.Self = q.Self && n.Self
		q.At = n.At
		if q.Subject != "" && n.Subject == q.Subject {
			for _, s := range n.States {
				if len(q.States) == 0 || q.States[len(q.States)-1] != s {
					q.States = append(q.States, s)
				}
			}
			q.render()
		} else {
			q.What, q.Subject, q.States, q.About = n.What, n.Subject, n.States, n.About
		}
		return
	}
	f.Queue = keepLast(append(f.Queue, n), maxQueue)
}

// wakes reports a pending notice that may start a turn: only a wake, and
// an observer's or a Rule's only within the wake budget (V0.95 (1), (3)).
func (f *Feed) wakes(now time.Time) bool {
	return slices.ContainsFunc(f.Queue, func(n Notice) bool { return f.waking(n, now) })
}

// take removes and returns the queued notices that match.
func (f *Feed) take(match func(Notice) bool) []Notice {
	var out, keep []Notice
	for _, n := range f.Queue {
		if match(n) {
			out = append(out, n)
		} else {
			keep = append(keep, n)
		}
	}
	f.Queue = keep
	return out
}

// remember records the delivery time of each key during a run: the
// interval of the reminders. Older entries go.
func (f *Feed) remember(ns []Notice, run *time.Time, now time.Time) {
	if f.Told == nil {
		f.Told = map[string]time.Time{}
	}
	for _, n := range ns {
		f.Told[n.Key] = now
	}
	for k, at := range f.Told {
		if run == nil || at.Before(*run) {
			delete(f.Told, k)
		}
	}
	if len(f.Told) == 0 {
		f.Told = nil
	}
}

// Asleep reports a thread whose agent is not woken: settled or archived.
// Its notices wait until it is revived.
func (t *Thread) Asleep() bool { return t.Settle.At != nil || t.ArchivedAt != nil }

// wakeable reports a thread whose agent a notice may wake now: not asleep,
// and not moving to another profile (the handover's end wakes it).
func (t *Thread) wakeable() bool { return !t.Asleep() && t.Handover == nil }

// NoticeText is how notices read to the agent: one line each.
func NoticeText(ns []Notice) string {
	lines := make([]string, len(ns))
	for i, n := range ns {
		lines[i] = "[Fyr] " + n.What
	}
	return strings.Join(lines, "\n")
}
