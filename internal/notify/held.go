package notify

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/kvstore"
)

// V0.push-summary-batch: a push that may not go out now is held, not dropped.
//
//   - Quiet hours (V0.46): a push for a thread whose project is quiet is held
//     until the window ends. Then one push says what still needs the owner
//     ("Acme: 3 done, 1 asking"), or nothing when all of it was seen or
//     settled meanwhile. The held pushes are kept in the KV table, so a
//     restart during the window loses none; each expires after HoldFor.
//   - Away: once no device has said anything for AwayAfter, done and blocked
//     are collected and sent as one push at most every BatchEvery ("3 done,
//     1 blocked"). Asking and failed still push at once. A beat from any
//     device ends the batch: what was collected before it is not sent, since
//     the owner came back and saw the inbox. The batch is in memory only: a
//     restart counts as presence (Presence.Seen), so it would be dropped
//     anyway.

// AwayAfter is how long without a beat from any device makes the owner away.
const AwayAfter = 10 * time.Minute

// BatchEvery is the most often a batch of done and blocked pushes goes out
// while the owner is away.
const BatchEvery = 5 * time.Minute

// HoldFor is how long a held quiet-hours push is kept: quiet stretches join
// at most a week ahead (decision 0014), so an older one is stale.
const HoldFor = 8 * 24 * time.Hour

// Tick is how often the front looks for held pushes that are due.
const Tick = 30 * time.Second

// Route is what becomes of a push that is due.
type Route int

const (
	Send  Route = iota
	Drop        // the thread is watched: the owner sees it
	Hold        // the project is in its quiet hours: summarised when they end
	Batch       // done or blocked while the owner is away: in the next batch
)

// RouteFor decides a due push of a thread entering state.
func RouteFor(state string, watched bool, quietUntil *time.Time, away bool, now time.Time) Route {
	switch {
	case watched:
		return Drop
	case quietUntil != nil && now.Before(*quietUntil):
		return Hold
	case away && (state == "done" || state == "blocked"):
		return Batch
	}
	return Send
}

// Held is one push held back.
type Held struct {
	Host    string    `json:"host"` // the host's name
	Thread  string    `json:"thread"`
	Project string    `json:"project,omitempty"`
	State   string    `json:"state"`
	At      time.Time `json:"at"`
	// Until is the end of the quiet hours it waits for; zero in the batch.
	Until time.Time `json:"until,omitzero"`
}

func (h Held) key() string { return h.Host + "\x00" + h.Thread }

// heldKey is the KV key of the held quiet-hours pushes.
const heldKey = "notify.held"

// Queue keeps the held pushes. The zero value keeps them in memory.
type Queue struct {
	KV kvstore.Table // nil: memory only

	mu    sync.Mutex
	quiet []Held
	batch []Held
}

// Load reads the held quiet-hours pushes the table kept, dropping those older
// than HoldFor.
func (q *Queue) Load(now time.Time) error {
	if q.KV == nil {
		return nil
	}
	raw, err := q.KV.GetKV(heldKey)
	if err != nil || raw == "" {
		return err
	}
	var hs []Held
	if err := json.Unmarshal([]byte(raw), &hs); err != nil {
		return fmt.Errorf("held pushes: %w", err)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.quiet = slices.DeleteFunc(hs, func(h Held) bool { return now.Sub(h.At) > HoldFor })
	return nil
}

// Hold keeps a push for when its quiet hours end; a newer one of the same
// thread takes its place.
func (q *Queue) Hold(h Held) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.quiet = put(q.quiet, h)
	return q.saveLocked()
}

// Batch adds a push to the away batch; a newer one of the same thread takes
// its place.
func (q *Queue) Batch(h Held) {
	q.mu.Lock()
	defer q.mu.Unlock()
	h.Until = time.Time{}
	q.batch = put(q.batch, h)
}

func put(hs []Held, h Held) []Held {
	for i := range hs {
		if hs[i].key() == h.key() {
			hs[i] = h
			return hs
		}
	}
	return append(hs, h)
}

// Due takes what is due at now. quiet is the held pushes whose window has
// ended, one group per host and project; batch is the away batch when its
// oldest push is BatchEvery old. lastSeen is the newest beat of any device:
// the batch's pushes from before it are dropped, since the owner came back.
func (q *Queue) Due(now, lastSeen time.Time) (quiet [][]Held, batch []Held, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var keep []Held
	groups := map[string]int{}
	for _, h := range q.quiet {
		switch {
		case now.Sub(h.At) > HoldFor:
		case now.Before(h.Until):
			keep = append(keep, h)
		default:
			g := h.Host + "\x00" + h.Project
			i, ok := groups[g]
			if !ok {
				i = len(quiet)
				groups[g] = i
				quiet = append(quiet, nil)
			}
			quiet[i] = append(quiet[i], h)
		}
	}
	if len(keep) != len(q.quiet) {
		q.quiet = keep
		err = q.saveLocked()
	}
	q.batch = slices.DeleteFunc(q.batch, func(h Held) bool { return !h.At.After(lastSeen) })
	if len(q.batch) > 0 {
		start := q.batch[0].At
		for _, h := range q.batch {
			if h.At.Before(start) {
				start = h.At
			}
		}
		if now.Sub(start) >= BatchEvery {
			batch, q.batch = q.batch, nil
		}
	}
	return quiet, batch, err
}

func (q *Queue) saveLocked() error {
	if q.KV == nil {
		return nil
	}
	b, err := json.Marshal(q.quiet)
	if err != nil {
		return err
	}
	if len(q.quiet) == 0 {
		b = nil
	}
	return q.KV.SetKV(heldKey, string(b))
}

// summaryOrder is the order of the states in a summary: what needs an
// answer first.
var summaryOrder = []string{"asking", "failed", "blocked", "done"}

// Counts is a summary's text: "3 done, 1 asking" in summaryOrder, "" for
// none. A state not in summaryOrder is not counted.
func Counts(states []string) string {
	n := map[string]int{}
	for _, s := range states {
		n[s]++
	}
	var parts []string
	for _, s := range summaryOrder {
		if n[s] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n[s], s))
		}
	}
	return strings.Join(parts, ", ")
}

// Summarised reports whether a thread now in state goes into a summary. The
// quiet summary counts all that needs the owner; the batch only done and
// blocked, since asking and failed pushed at once.
func Summarised(state string, quiet bool) bool {
	if quiet {
		return slices.Contains(summaryOrder, state)
	}
	return state == "done" || state == "blocked"
}
