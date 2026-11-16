package core

// Done (SPEC V0.98, decision 0024 (3), slice 4): a finished change leaves
// the board. Ended (abandoned, reverted) keeps its meaning for rules and
// settle; done is when the work needs nobody any more. Fyr marks it at the
// reduce that finishes it, so the thread's other changes (a stacked PR's
// base) decide, and the client only reads it. A deploy watch no longer holds
// a merged change (V0.105 (1)); it only adds live as a fact.

import (
	"encoding/json"
	"slices"
	"strings"
	"time"
)

// DoneMark is when a change was done, who finished it (the phase's writer,
// or the owner's Done), and why: the phase, "merged", DoneByOwner or
// DoneBeforeV2.
type DoneMark struct {
	At     time.Time `json:"at"`
	By     string    `json:"by"`
	Reason string    `json:"reason"`
}

const (
	// DoneByOwner is the reason of the owner's Done (V0.98 (4)).
	DoneByOwner = "done"
	// DoneBeforeV2 is the reason the load pass gives a change that landed
	// before V0.98 (7).
	DoneBeforeV2 = "landed before v2"
	// DoneDefaultBranch is the reason of a record on the repo's default
	// branch (V0.105 (2)).
	DoneDefaultBranch = "default branch"
	// KindChangeDone is the owner's Done on a change of the thread; the host
	// sends it to a host record as KindRecordDone.
	KindChangeDone = "change.done"
	// KindRecordDone is the owner's Done on a host record: the one owner
	// Input a record takes.
	KindRecordDone = "record.done"
)

// IsDone reports a finished change: it is off the board and the thread's
// open list.
func (c *Change) IsDone() bool { return c.Done != nil }

// MarkedDone reports the owner's Done ("marked done by you"): it holds
// against later facts, and the change no longer blocks.
func (c *Change) MarkedDone() bool { return c.Done != nil && c.Done.Reason == DoneByOwner }

// markDone keeps touchedAt and the done marks of the thread's changes after
// a reduce: a change whose facts moved since before is touched now; a
// change that finished is marked; one that went back in flight (a reopened
// PR) is open again unless the owner marked it.
func markDone(before, t *Thread, now time.Time) {
	prev := map[string]*Change{}
	if before != nil {
		for i := range before.Changes {
			prev[before.Changes[i].ID] = &before.Changes[i]
		}
	}
	for i := range t.Changes {
		c := &t.Changes[i]
		if p := prev[c.ID]; p == nil || moved(p, c) {
			c.TouchedAt = &now
		}
	}
	// Twice: a change done in the first pass can finish a stacked PR on it.
	for range 2 {
		for i := range t.Changes {
			c := &t.Changes[i]
			if c.Done != nil {
				if !c.MarkedDone() && c.Phase.InFlight() {
					c.Done = nil
				}
				continue
			}
			if reason, ok := doneNow(t, c); ok {
				c.Done = &DoneMark{At: now, By: c.PhaseBy, Reason: reason}
			}
		}
	}
}

// moved reports a change whose facts differ: its time stamps, its done mark
// and when its PR was read do not count.
func moved(a, b *Change) bool {
	strip := func(c *Change) []byte {
		x := *c
		x.UpdatedAt, x.TouchedAt, x.Done = time.Time{}, nil, nil
		if x.PR != nil {
			p := *x.PR
			p.CheckedAt = nil
			x.PR = &p
		}
		out, _ := json.Marshal(x)
		return out
	}
	return string(strip(a)) != string(strip(b))
}

// doneNow is why c is done now, if it is (V0.98 (1)).
func doneNow(t *Thread, c *Change) (string, bool) {
	switch {
	case c.Phase.Ended():
		return string(c.Phase), true
	case !c.Phase.Reached(PhaseLanded):
		return "", false
	case c.Phase == PhaseLanded && stackedWaits(t, c):
		return "", false
	}
	if c.Phase == PhaseLanded && c.PR != nil && c.PR.State == "MERGED" {
		return "merged", true
	}
	return string(c.Phase), true
}

// stackedWaits reports a PR merged into a branch that is not the default
// one, whose merge commit is not in the default branch yet. When the poller
// could not tell, the change of its base branch decides.
func stackedWaits(t *Thread, c *Change) bool {
	p := c.PR
	if p == nil || p.State != "MERGED" || !p.Stacked {
		return false
	}
	if p.InDefault != nil {
		return !*p.InDefault
	}
	return !slices.ContainsFunc(t.Changes, func(x Change) bool {
		return x.ID != c.ID && x.Branch == p.Base && x.IsDone() && sameRepo(x.Targets, repoTargets(prTargetRepo(p.Repo)))
	})
}

// stackedOpen reports a merged stacked PR that is not done: the poller keeps
// reading it until its merge commit is in the default branch.
func stackedOpen(c *Change) bool {
	return !c.IsDone() && c.Phase == PhaseLanded && c.PR != nil && c.PR.State == "MERGED" && c.PR.Stacked
}

// prTargetRepo is a PR's owner/name in the git observer's form.
func prTargetRepo(repo string) string {
	if repo != "" && strings.Count(repo, "/") == 1 {
		return "github.com/" + repo
	}
	return repo
}

// changeDone is the owner's change.done {change} (V0.98 (4)).
func changeDone(t *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
	c := FindChange(t, strings.TrimSpace(a.Str("change")))
	if c == nil {
		return nil, NoChange(a.Str("change"))
	}
	if c.IsDone() {
		return nil, reject(RejectUnavailable, "%q is already done", c.Title)
	}
	c.Done = &DoneMark{At: now, By: ByOwner, Reason: DoneByOwner}
	c.UpdatedAt = now
	v.SeenAt = now
	return nil, nil
}

func init() {
	register(&Action{Name: KindChangeDone, Label: "Done",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			for i := range t.Changes {
				if !t.Changes[i].IsDone() {
					return ""
				}
			}
			return "No open change"
		},
		Apply: changeDone})
}

// recordDone is the owner's Done on a host record (V0.98 (4)).
func recordDone(rs []*Change, in Input, now time.Time) (before, after *Change, err error) {
	id := strings.TrimSpace(mapArgs(in).Str("change"))
	i := slices.IndexFunc(rs, func(c *Change) bool { return c.ID == id })
	if i < 0 {
		return nil, nil, NoChange(id)
	}
	if rs[i].IsDone() {
		return nil, nil, reject(RejectUnavailable, "%q is already done", rs[i].Title)
	}
	before = rs[i]
	c := cloneChange(before)
	c.Done = &DoneMark{At: now, By: ByOwner, Reason: DoneByOwner}
	c.UpdatedAt, c.TouchedAt = now, &now
	return before, c, nil
}

// joinInto folds the change from into the change keep (V0.87 (2)) and
// returns keep.
func joinInto(t *Thread, keep, from string, now time.Time) *Change {
	i := slices.IndexFunc(t.Changes, func(x Change) bool { return x.ID == from })
	if i < 0 || keep == from {
		return findByID(t, keep)
	}
	x := t.Changes[i]
	t.Changes = slices.Delete(t.Changes, i, i+1)
	c := findByID(t, keep)
	foldChange(t, c, &x, now)
	return c
}

// MigrateDone is the load pass of V0.98 (7) on one thread: a change Fyr
// made for a PR folds into the open change on its repo and branch; every
// change at or past landed is done ("landed before v2"; V0.105: also with
// a deploy watch armed), every ended one too; an open change without touchedAt is
// touched when it last moved. It reports whether the thread changed.
func MigrateDone(t *Thread, now time.Time) bool {
	changed := false
	for {
		i := slices.IndexFunc(t.Changes, func(c Change) bool {
			return madeForPR(&c) && c.Branch != "" && changeOnPRBranch(t, c.Branch, c.PR.Repo, c.ID) != nil
		})
		if i < 0 {
			break
		}
		c := t.Changes[i]
		joinInto(t, changeOnPRBranch(t, c.Branch, c.PR.Repo, c.ID).ID, c.ID, now)
		changed = true
	}
	for i := range t.Changes {
		c := &t.Changes[i]
		if c.TouchedAt == nil && !c.UpdatedAt.IsZero() {
			at := c.UpdatedAt
			c.TouchedAt, changed = &at, true
		}
		if c.Done != nil {
			continue
		}
		reason, ok := doneNow(t, c)
		if !ok {
			continue
		}
		if c.Phase.Reached(PhaseLanded) {
			reason = DoneBeforeV2
		}
		c.Done, changed = &DoneMark{At: now, By: c.PhaseBy, Reason: reason}, true
	}
	return changed
}

// MigrateRecordDone is the load pass on a host record: one that landed or
// ended is done. It reports whether the record changed.
func MigrateRecordDone(c *Change, now time.Time) bool {
	changed := false
	if c.TouchedAt == nil && !c.UpdatedAt.IsZero() {
		at := c.UpdatedAt
		c.TouchedAt, changed = &at, true
	}
	if c.Done != nil {
		return changed
	}
	if c.Phase.InFlight() {
		// V0.105 (2): a record on the repo's default branch is done.
		if !onDefaultBranch(c) {
			return changed
		}
		c.Done = &DoneMark{At: now, By: ByObserved, Reason: DoneDefaultBranch}
		return true
	}
	reason := string(c.Phase)
	if c.Phase.Reached(PhaseLanded) {
		reason = DoneBeforeV2
	}
	c.Done = &DoneMark{At: now, By: c.PhaseBy, Reason: reason}
	return true
}

// onDefaultBranch reports a record whose branch is its repo's default one,
// by its workspace base, else main or master (V0.105 (2)).
func onDefaultBranch(c *Change) bool {
	g := GitFacts{Branch: c.Branch}
	if c.Workspace != nil {
		g.Base = c.Workspace.Base
	}
	return c.Branch != "" && defaultBranch(g)
}
