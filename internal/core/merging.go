package core

import (
	"fmt"
	"time"
)

// V0.merge-in-flight: a merge that was started is not the owner's move any
// more. Until now the thread kept reading "Merge" in Your move from the press
// until the poller read the PR as merged, as if the press had done nothing.

// Merging is a merge of the change's PR that Fyr started and GitHub has not
// reported yet.
type Merging struct {
	// Head is the commit the merge is bound to; "" when the effect named none.
	Head string `json:"head,omitempty"`
	// At is the effect's start, then its ok result: MergingFor counts from it.
	At time.Time `json:"at"`
}

// MergingFor is how long a started merge counts without news: FyrMerge's own
// time limit. Past it the PR reads as it is (Ready, or its failure).
const MergingFor = 10 * time.Minute

// CauseMerging is a change whose merge is in flight: shown muted, never in
// Needs you.
const CauseMerging = "merging"

// MergingText is the muted line of a change whose merge is in flight.
const MergingText = "Merging"

// InMerge reports a change whose merge is in flight: the marker is set, the
// PR is still open and its head is the one the merge is bound to. The time
// limit is not read here: the clock's reduce drops the marker when it passes
// (NextCheck), so the answer does not move between two reduces.
func (c *Change) InMerge() bool {
	m := c.Merging
	return m != nil && c.OpenPR() && (m.Head == "" || SameHead(c.PR.Head, m.Head))
}

// markMerging sets the marker on the change of every merge effect this reduce
// starts. One place for the four ways a merge starts (the Merge button, the
// owner's yes to a merge ask, the owner's merge command, auto-merge): each
// ends as an EffectRun of FyrMerge with the PR in its args.
func markMerging(t *Thread, effects []Effect, now time.Time) {
	for _, e := range effects {
		if e.Kind != EffectRun || e.Request == nil || e.Request.Action != FyrMerge {
			continue
		}
		url, _ := e.Request.Args["pr"].(string)
		if c := changeByPR(t, url); c != nil && c.OpenPR() {
			head, _ := e.Request.Args["head"].(string)
			c.Merging = &Merging{Head: head, At: now}
		}
	}
}

// mergeResult takes a merge effect's result: a refused or lost merge ends the
// marker (the PR reads as it is again); a merge GitHub took keeps it until
// the poller reads the PR as merged, counting MergingFor from now.
func mergeResult(t *Thread, req *EffectReq, ok bool, now time.Time) {
	if req == nil || req.Action != FyrMerge {
		return
	}
	url, _ := req.Args["pr"].(string)
	c := changeByPR(t, url)
	if c == nil || c.Merging == nil {
		return
	}
	if !ok {
		c.Merging = nil
		return
	}
	c.Merging.At = now
}

// dropStaleMerging removes the markers that no longer hold: the PR merged or
// closed, its head moved, or MergingFor passed with no news.
func dropStaleMerging(t *Thread, now time.Time) {
	for i := range t.Changes {
		c := &t.Changes[i]
		if c.Merging != nil && (!c.InMerge() || !now.Before(c.Merging.At.Add(MergingFor))) {
			c.Merging = nil
		}
	}
}

// mergingCheck is when the clock must re-reduce the thread so a marker with
// no news ends, or nil.
func mergingCheck(t *Thread) *time.Time {
	var out *time.Time
	for i := range t.Changes {
		if c := &t.Changes[i]; c.InMerge() {
			if at := c.Merging.At.Add(MergingFor); out == nil || at.Before(*out) {
				out = &at
			}
		}
	}
	return out
}

// mergingText is the Waiting reason of a thread with a merge in flight, or "".
func mergingText(t *Thread) string {
	for i := len(t.Changes) - 1; i >= 0; i-- {
		if c := &t.Changes[i]; c.InMerge() && !c.IsDone() {
			if c.PR.Number > 0 {
				return fmt.Sprintf("Merging #%d", c.PR.Number)
			}
			return MergingText
		}
	}
	return ""
}
