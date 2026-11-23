package core

// The load passes (V0.structure-cost (2)): every repair of a record stored by an
// older build lives here. They run once per store (V0.versioned-store): the
// store's migration at Open calls Migrate on every thread and MigrateRecord
// on every host record, and stores what they changed in the transaction that
// records the store's schema version. The repairs of shapes stored before
// V0.100 (#31, #32, #33, #35, #46, #48, V0.70) are gone: Open refuses such a
// store.

import (
	"slices"
	"time"
)

// Migrate repairs a thread stored by an older build, once per store: a
// change that is not code ends (V0.changes-code-only (4)), what landed or
// ended before V0.98 is done, and a Rule on a retired Condition kind is
// brought up to date. It reports whether t changed.
func Migrate(t *Thread, now time.Time) bool {
	changed := migrateCodeOnly(t, now)
	changed = migrateDone(t, now) || changed
	return migrateRetiredWhens(t) || changed
}

// MigrateRecord is Migrate on a host record. It reports whether c changed.
func MigrateRecord(c *Change, now time.Time) bool {
	ended := migrateRecordCodeOnly(c, now)
	return migrateRecordDone(c, now) || ended
}

// retiredNoDecisions is the Condition kind the "settle when done" Rule read
// before V0.203 (all(runEnded, noDecisions, idle)); no build arms it now.
const retiredNoDecisions = "noDecisions"

// migrateRetiredWhens gives a stored "settle when done" Rule that reads
// noDecisions the Condition it has now (workDone), and drops any other Rule
// that reads it: it could never hold again (V0.structure-cost (2)).
func migrateRetiredWhens(t *Thread) bool {
	changed := false
	for i := range t.Rules {
		if r := &t.Rules[i]; r.ID == settleDoneRule && r.When.has(retiredNoDecisions) {
			r.When, changed = settleRule(settleDoneRule, "", When{Kind: WhenWorkDone}, r.CreatedBy).When, true
		}
	}
	n := len(t.Rules)
	DropRules(t, func(r Rule) bool { return r.When.has(retiredNoDecisions) })
	return changed || len(t.Rules) != n
}

// migrateCodeOnly is the load pass of V0.changes-code-only (4) on one
// thread: a change that is not code and has not finished (not ended, not
// landed or live) ends as abandoned with CodeOnlyReason. It does not move
// the thread, so nothing about it reaches Your move; migrateDone then files
// it under Done here. An open ask on such a change is closed, and the agent
// hears so once, at its next tool call or turn. It reports whether the
// thread changed.
func migrateCodeOnly(t *Thread, now time.Time) bool {
	changed := false
	for i := range t.Changes {
		c := &t.Changes[i]
		if !endCodeOnly(c, now) {
			continue
		}
		changed = true
		for _, d := range t.Decisions {
			if d.Effect == nil {
				continue
			}
			if id, _ := d.Effect.Args["change"].(string); id != c.ID {
				continue
			}
			DecisionClosed(t, d.ID)
			t.Feed.Enqueue(Notice{About: "change:" + c.ID, What: codeOnlyNotice(d.Effect.Action, c), Urgency: Next, Key: "codeonly:" + d.ID, At: now})
		}
	}
	return changed
}

// migrateRecordCodeOnly is the same pass on a host record.
func migrateRecordCodeOnly(c *Change, now time.Time) bool { return endCodeOnly(c, now) }

func endCodeOnly(c *Change, now time.Time) bool {
	if c.Kind == "" || c.Kind == KindCode || c.Phase.Ended() || c.Phase.Reached(PhaseLanded) {
		return false
	}
	setPhase(nil, c, PhaseAbandoned, ByObserved, now)
	c.Label, c.Offers, c.Annotations = CodeOnlyReason, nil, nil
	return true
}

// migrateDone is the load pass of V0.98 (7) on one thread: a change Fyr
// made for a PR folds into the open change on its repo and branch; every
// change at or past landed is done ("landed before v2"; V0.105: also with
// a deploy watch armed), every ended one too; an open change without touchedAt is
// touched when it last moved. It reports whether the thread changed.
func migrateDone(t *Thread, now time.Time) bool {
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

// migrateRecordDone is the load pass on a host record: one that landed or
// ended is done. It reports whether the record changed.
func migrateRecordDone(c *Change, now time.Time) bool {
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
