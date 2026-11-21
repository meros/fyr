package core

// V0.thread-invariants: pure state and transition invariants of a thread,
// checked after every Reduce / Engine.apply, on Engine.Load, on Engine.Tick,
// and in randomized state-machine tests. Each invariant has a stable kebab-case
// id, a one-sentence description, and returns structured Violation evidence
// when broken.

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Pure thread state & transition invariant IDs (stable: logs, cards, fix threads).
const (
	// State invariants (CheckThread).
	InvThreadArchivedLiveWork    = "thread-archived-live-work"
	InvThreadSettledContradict   = "thread-settled-contradiction"
	InvThreadRunStateContradict  = "thread-run-state-contradiction"
	InvThreadPinContradict       = "thread-pin-contradiction"
	InvThreadRuleInvalid         = "thread-rule-invalid"
	InvThreadDecisionInvalid     = "thread-decision-invalid"
	InvThreadChangeInvalid       = "thread-change-invalid"
	InvThreadOutboxInvalid       = "thread-outbox-invalid"
	InvThreadFeedInvalid         = "thread-feed-invalid"
	InvThreadAttentionPlacement  = "thread-attention-placement"
	InvThreadAvailabilityMismatch = "thread-availability-mismatch"

	// Transition invariants (CheckTransition).
	InvThreadArchivedMutated    = "thread-archived-mutated"
	InvThreadSettledRunStarted  = "thread-settled-run-started"
	InvThreadReviveIncomplete   = "thread-revive-incomplete"
	InvThreadRightsViolated     = "thread-rights-violated"
	InvThreadCountersRegressed  = "thread-counters-regressed"
	InvThreadPollStoleSeen      = "thread-poll-stole-seen"
	InvThreadRuleLoop           = "thread-rule-loop"
)

// CoreThreadInvariantTexts describes each pure thread invariant in one sentence.
var CoreThreadInvariantTexts = map[string]string{
	InvThreadArchivedLiveWork:     "An archived thread still has live work, open decisions, a pin, a snooze, or armed rules.",
	InvThreadSettledContradict:    "A settled thread still has a pin, a settle offer, a settle rule, a snooze, or an armed watch.",
	InvThreadRunStateContradict:   "A thread's live run fields disagree (Running, Starting, RunStartedAt, Activity, Failure, or Stopped).",
	InvThreadPinContradict:        "A pinned thread is also archived, settled, actively snoozed, or has an unsuspended auto-settle rule.",
	InvThreadRuleInvalid:          "A thread or view rule has inconsistent suspend state, duplicate id, orphan change, unwatchable PR condition, or a stuck one-shot watch.",
	InvThreadDecisionInvalid:      "A thread has a duplicate, void, malformed, or mis-gated open decision or held merge ask.",
	InvThreadChangeInvalid:        "A thread's change record has an invalid phase, PR mapping, done mark, stale park, or dropped PR conditions.",
	InvThreadOutboxInvalid:        "A thread's outbox contains an empty, duplicate, or terminal message state.",
	InvThreadFeedInvalid:          "A thread's agent feed exceeds its limits or holds a silent or duplicate-key notice.",
	InvThreadAttentionPlacement:   "A thread's computed attention state, section, group, or reason violates the placement rules.",
	InvThreadAvailabilityMismatch: "A command's availability disagrees with the thread's state.",
	InvThreadArchivedMutated:      "An archived thread started a run, opened a decision, or queued a message without being unarchived.",
	InvThreadSettledRunStarted:    "A run or start began on a settled thread without reviving it first.",
	InvThreadReviveIncomplete:     "A send or retry left a failure, settle, settle offer, or snooze behind, or an agent send moved the owner's read watermark.",
	InvThreadRightsViolated:       "An input overwrote a field protected by field rights (owner title, owner-ended change, or owner-done change).",
	InvThreadCountersRegressed:    "A thread's monotonic fields regressed across a transition (id, createdAt, runEnds, or runEnded count).",
	InvThreadPollStoleSeen:        "A PR poll with no state change or new blocker moved a Seen thread back to Done.",
	InvThreadRuleLoop:             "A thread's rules hit the firing limit (rule.loop) during one transition.",
}

// CoreThreadInvariantOrder is the display order of the pure thread invariants.
var CoreThreadInvariantOrder = []string{
	InvThreadArchivedLiveWork,
	InvThreadSettledContradict,
	InvThreadRunStateContradict,
	InvThreadPinContradict,
	InvThreadRuleInvalid,
	InvThreadDecisionInvalid,
	InvThreadChangeInvalid,
	InvThreadOutboxInvalid,
	InvThreadFeedInvalid,
	InvThreadAttentionPlacement,
	InvThreadAvailabilityMismatch,
	InvThreadArchivedMutated,
	InvThreadSettledRunStarted,
	InvThreadReviveIncomplete,
	InvThreadRightsViolated,
	InvThreadCountersRegressed,
	InvThreadPollStoleSeen,
	InvThreadRuleLoop,
}

// Violation is one broken invariant and its structured evidence.
type Violation struct {
	ID       string
	Evidence string
}

// CheckThread checks every pure structural, lifecycle, rule, decision, change,
// outbox, feed, attention-placement, and availability invariant on (t, v) at now.
// on is the auto-merge repo predicate (same as ComputeWith; nil means none).
func CheckThread(t *Thread, v *View, now time.Time, on func(repo string) bool) []Violation {
	if t == nil {
		return nil
	}
	if v == nil {
		v = &View{User: Owner, Entity: t.ID}
	}
	var out []Violation
	add := func(id, detail string) {
		out = append(out, Violation{ID: id, Evidence: fmt.Sprintf("thread=%s %s", t.ID, detail)})
	}

	if d := checkArchivedLiveWork(t, v); d != "" {
		add(InvThreadArchivedLiveWork, d)
	}
	if d := checkSettledContradiction(t, v); d != "" {
		add(InvThreadSettledContradict, d)
	}
	if d := checkRunStateContradiction(t); d != "" {
		add(InvThreadRunStateContradict, d)
	}
	if d := checkPinContradiction(t, v, now); d != "" {
		add(InvThreadPinContradict, d)
	}
	if d := checkRulesInvalid(t, v); d != "" {
		add(InvThreadRuleInvalid, d)
	}
	if d := checkDecisionsInvalid(t); d != "" {
		add(InvThreadDecisionInvalid, d)
	}
	if d := checkChangesInvalid(t); d != "" {
		add(InvThreadChangeInvalid, d)
	}
	if d := checkOutboxInvalid(t); d != "" {
		add(InvThreadOutboxInvalid, d)
	}
	if d := checkFeedInvalid(t); d != "" {
		add(InvThreadFeedInvalid, d)
	}
	if d := checkAttentionPlacement(t, v, now, on); d != "" {
		add(InvThreadAttentionPlacement, d)
	}
	if d := checkAvailabilityMismatch(t, v, now); d != "" {
		add(InvThreadAvailabilityMismatch, d)
	}
	return out
}

// CheckTransition checks every pure transition invariant across one Reduce step.
func CheckTransition(beforeT *Thread, beforeV *View, afterT *Thread, afterV *View, in Input, res Result, now time.Time, on func(repo string) bool) []Violation {
	if afterT == nil {
		return nil
	}
	if beforeT == nil {
		beforeT = &Thread{}
	}
	if beforeV == nil {
		beforeV = &View{User: Owner, Entity: beforeT.ID}
	}
	if afterV == nil {
		afterV = &View{User: Owner, Entity: afterT.ID}
	}
	var out []Violation
	add := func(id, detail string) {
		out = append(out, Violation{ID: id, Evidence: fmt.Sprintf("thread=%s input=%s source=%s %s", afterT.ID, in.Kind, in.Source, detail)})
	}

	// 1. Archived thread must not acquire new live work without being unarchived.
	if beforeT.ArchivedAt != nil && afterT.ArchivedAt != nil {
		var probs []string
		if !beforeT.Running && afterT.Running {
			probs = append(probs, "started running")
		}
		if beforeT.Starting == nil && afterT.Starting != nil {
			probs = append(probs, "started starting")
		}
		if len(afterT.Decisions) > len(beforeT.Decisions) {
			probs = append(probs, fmt.Sprintf("decisions grew %d->%d", len(beforeT.Decisions), len(afterT.Decisions)))
		}
		if len(afterT.Outbox) > len(beforeT.Outbox) {
			probs = append(probs, fmt.Sprintf("outbox grew %d->%d", len(beforeT.Outbox), len(afterT.Outbox)))
		}
		if len(probs) > 0 {
			add(InvThreadArchivedMutated, strings.Join(probs, "; "))
		}
	}

	// 2. Settled thread must not begin a run or start without reviving first.
	if afterT.Settle.At != nil {
		var probs []string
		if !beforeT.Running && afterT.Running {
			probs = append(probs, "run started while settled")
		}
		if beforeT.Starting == nil && afterT.Starting != nil {
			probs = append(probs, "starting set while settled")
		}
		if len(probs) > 0 {
			add(InvThreadSettledRunStarted, strings.Join(probs, "; "))
		}
	}

	// 3. Send and retry must cleanly revive the thread and respect view.seen rights.
	if in.Kind == "thread.send" || in.Kind == "thread.retry" {
		var probs []string
		if afterT.Settle.At != nil {
			probs = append(probs, "settle.at still set")
		}
		if afterT.Failure != nil {
			probs = append(probs, "failure still set")
		}
		if afterT.SettleAsk != nil {
			probs = append(probs, "settleAsk still set")
		}
		if u, _ := SnoozeOf(afterV); u != nil {
			probs = append(probs, "snooze still armed")
		}
		if in.Source == FromOwner && afterV.SeenAt.Before(now) {
			probs = append(probs, fmt.Sprintf("owner send did not advance seenAt (%s < %s)", afterV.SeenAt.Format(time.RFC3339), now.Format(time.RFC3339)))
		}
		if in.Source == FromAgent && !afterV.SeenAt.Equal(beforeV.SeenAt) {
			probs = append(probs, fmt.Sprintf("agent send changed owner seenAt (%s -> %s)", beforeV.SeenAt.Format(time.RFC3339), afterV.SeenAt.Format(time.RFC3339)))
		}
		if len(probs) > 0 {
			add(InvThreadReviveIncomplete, strings.Join(probs, "; "))
		}
	}

	// 4. Field rights: owner title, owner-ended change, owner-marked-done change.
	{
		var probs []string
		if beforeT.TitleBy == ByOwner && in.Source == FromAgent && (afterT.Title != beforeT.Title || afterT.TitleBy != ByOwner) {
			probs = append(probs, fmt.Sprintf("owner title %q overwritten by agent to %q (by=%s)", beforeT.Title, afterT.Title, afterT.TitleBy))
		}
		if in.Source != FromOwner {
			for i := range beforeT.Changes {
				bc := &beforeT.Changes[i]
				ac := FindChange(afterT, bc.ID)
				if ac == nil {
					continue
				}
				if bc.PhaseBy == ByOwner && bc.Phase.Ended() && ac.Phase != bc.Phase {
					probs = append(probs, fmt.Sprintf("owner-ended change %s phase %s->%s", bc.ID, bc.Phase, ac.Phase))
				}
				if bc.MarkedDone() && (!ac.MarkedDone() || (in.Source == FromAgent && ac.Phase != bc.Phase)) {
					probs = append(probs, fmt.Sprintf("owner-done change %s mutated (done=%v phase=%s->%s)", bc.ID, ac.MarkedDone(), bc.Phase, ac.Phase))
				}
			}
		}
		if len(probs) > 0 {
			add(InvThreadRightsViolated, strings.Join(probs, "; "))
		}
	}

	// 5. Monotonic counters & immutable identity.
	{
		var probs []string
		if beforeT.ID != "" && afterT.ID != beforeT.ID {
			probs = append(probs, fmt.Sprintf("id changed %q->%q", beforeT.ID, afterT.ID))
		}
		if !beforeT.CreatedAt.IsZero() && !afterT.CreatedAt.Equal(beforeT.CreatedAt) {
			probs = append(probs, fmt.Sprintf("createdAt changed %s->%s", beforeT.CreatedAt.Format(time.RFC3339), afterT.CreatedAt.Format(time.RFC3339)))
		}
		if afterT.RunEnds < beforeT.RunEnds {
			probs = append(probs, fmt.Sprintf("runEnds regressed %d->%d", beforeT.RunEnds, afterT.RunEnds))
		}
		if in.Kind == KindRunEnded && beforeT.Running {
			if afterT.Running {
				probs = append(probs, "still running after run.ended")
			}
			if afterT.RunEnds != beforeT.RunEnds+1 {
				probs = append(probs, fmt.Sprintf("runEnds %d->%d on run.ended (want +1)", beforeT.RunEnds, afterT.RunEnds))
			}
		}
		if len(probs) > 0 {
			add(InvThreadCountersRegressed, strings.Join(probs, "; "))
		}
	}

	// 6. Routine PR polling of an already-open PR with no new blocker must never flip Seen -> Done.
	if in.Kind == KindPRObserved {
		if p, ok := in.Args.(PR); ok && p.State == "OPEN" {
			beforeAtt := ComputeWith(beforeT, beforeV, now, on)
			afterAtt := ComputeWith(afterT, afterV, now, on)
			if beforeAtt.State == Seen && afterAtt.State == Done {
				existedOpen := false
				for i := range beforeT.Changes {
					bc := &beforeT.Changes[i]
					if bc.PR != nil && bc.PR.State == "OPEN" && bc.Phase == PhaseReview && (p.URL == "" || bc.PR.URL == p.URL) {
						existedOpen = true
						break
					}
				}
				newlyBlocking := false
				for i := range afterT.Changes {
					ac := &afterT.Changes[i]
					ab := ac.Blocking()
					if ab == "" {
						continue
					}
					bc := FindChange(beforeT, ac.ID)
					if bc == nil || bc.Blocking() != ab {
						newlyBlocking = true
						break
					}
				}
				if existedOpen && !newlyBlocking {
					add(InvThreadPollStoleSeen, fmt.Sprintf("pr=%s checks=%s moved Seen->Done without new blocker", p.URL, p.Checks))
				}
			}
		}
	}

	// 7. Rule firing loop.
	for _, ev := range res.Events {
		if ev.Type == "rule.loop" {
			add(InvThreadRuleLoop, fmt.Sprintf("rule.loop data=%v", ev.Data))
			break
		}
	}

	return out
}

// --- pure state check helpers ---

func checkArchivedLiveWork(t *Thread, v *View) string {
	if t.ArchivedAt == nil {
		return ""
	}
	var probs []string
	if t.Running {
		probs = append(probs, "running=true")
	}
	if t.Starting != nil {
		probs = append(probs, "starting!=nil")
	}
	if t.Handover != nil {
		probs = append(probs, "handover!=nil")
	}
	if n := t.RunningTasks(); n > 0 {
		probs = append(probs, fmt.Sprintf("runningTasks=%d", n))
	}
	if len(t.Decisions) > 0 {
		probs = append(probs, fmt.Sprintf("decisions=%d", len(t.Decisions)))
	}
	if len(t.HeldAsks) > 0 {
		probs = append(probs, fmt.Sprintf("heldAsks=%d", len(t.HeldAsks)))
	}
	if t.PinnedAt != nil {
		probs = append(probs, "pinned=true")
	}
	if u, _ := SnoozeOf(v); u != nil {
		probs = append(probs, "snooze armed")
	}
	for _, r := range t.Rules {
		if r.Status == RuleArmed {
			probs = append(probs, fmt.Sprintf("rule %s (%s) still armed", r.ID, r.Purpose))
			break
		}
	}
	return strings.Join(probs, "; ")
}

func checkSettledContradiction(t *Thread, v *View) string {
	if t.Settle.At == nil {
		return ""
	}
	var probs []string
	if t.Starting != nil {
		probs = append(probs, "starting!=nil")
	}
	if t.PinnedAt != nil {
		probs = append(probs, "pinned=true")
	}
	if t.SettleAsk != nil {
		probs = append(probs, "settleAsk!=nil")
	}
	if u, _ := SnoozeOf(v); u != nil {
		probs = append(probs, "snooze armed")
	}
	for _, r := range t.Rules {
		if r.Purpose == PurposeSettle {
			probs = append(probs, fmt.Sprintf("settle rule %s still present", r.ID))
		}
		if r.Purpose == PurposeWatch && r.Status == RuleArmed {
			probs = append(probs, fmt.Sprintf("watch rule %s still armed", r.ID))
		}
	}
	return strings.Join(probs, "; ")
}

func checkRunStateContradiction(t *Thread) string {
	var probs []string
	if t.Running && t.RunStartedAt == nil {
		probs = append(probs, "running=true with runStartedAt=nil")
	}
	if !t.Running && t.RunStartedAt != nil {
		probs = append(probs, "running=false with runStartedAt!=nil")
	}
	if !t.Running && t.Activity != "" {
		probs = append(probs, fmt.Sprintf("running=false with activity=%q", t.Activity))
	}
	if t.Running && t.Starting != nil {
		probs = append(probs, "running=true and starting!=nil")
	}
	if t.Running && t.Failure != nil {
		probs = append(probs, fmt.Sprintf("running=true with failure=%s", t.Failure.Class))
	}
	if t.Starting != nil && t.Failure != nil {
		probs = append(probs, fmt.Sprintf("starting!=nil with failure=%s", t.Failure.Class))
	}
	if (t.Running || t.RunningTasks() > 0) && t.Stopped {
		probs = append(probs, fmt.Sprintf("stopped=true while running=%t tasks=%d", t.Running, t.RunningTasks()))
	}
	return strings.Join(probs, "; ")
}

func checkPinContradiction(t *Thread, v *View, now time.Time) string {
	if t.PinnedAt == nil {
		return ""
	}
	var probs []string
	if t.ArchivedAt != nil {
		probs = append(probs, "pinned and archived")
	}
	if t.Settle.At != nil {
		probs = append(probs, "pinned and settled")
	}
	if u, _ := SnoozeOf(v); u != nil && u.After(now) {
		probs = append(probs, "pinned and snoozed")
	}
	for _, r := range t.Rules {
		if autoSettle(r) && !slices.Contains(r.SuspendedBy, SuspendPin) {
			probs = append(probs, fmt.Sprintf("auto-settle rule %s not suspended by pin", r.ID))
		}
	}
	return strings.Join(probs, "; ")
}

func checkRulesInvalid(t *Thread, v *View) string {
	var probs []string
	seenThread := map[string]bool{}
	for _, r := range t.Rules {
		if r.ID == "" || r.Purpose == "" || r.Then.Kind == "" {
			probs = append(probs, fmt.Sprintf("malformed thread rule id=%q purpose=%q then=%q", r.ID, r.Purpose, r.Then.Kind))
		}
		if r.ID != "" && seenThread[r.ID] {
			probs = append(probs, fmt.Sprintf("duplicate thread rule %q", r.ID))
		}
		seenThread[r.ID] = true
		if r.Status != RuleArmed && r.Status != RuleSuspended {
			probs = append(probs, fmt.Sprintf("rule %s invalid status %q", r.ID, r.Status))
		}
		if (r.Status == RuleSuspended) != (len(r.SuspendedBy) > 0) {
			probs = append(probs, fmt.Sprintf("rule %s status=%s suspendedBy=%v", r.ID, r.Status, r.SuspendedBy))
		}
		wantSettle := t.Settle.At != nil && slices.Contains(suspendOn(r.Purpose), SuspendSettle)
		if slices.Contains(r.SuspendedBy, SuspendSettle) != wantSettle {
			probs = append(probs, fmt.Sprintf("rule %s suspendSettle=%t want=%t", r.ID, slices.Contains(r.SuspendedBy, SuspendSettle), wantSettle))
		}
		wantArchive := t.ArchivedAt != nil && slices.Contains(suspendOn(r.Purpose), SuspendArchive)
		if slices.Contains(r.SuspendedBy, SuspendArchive) != wantArchive {
			probs = append(probs, fmt.Sprintf("rule %s suspendArchive=%t want=%t", r.ID, slices.Contains(r.SuspendedBy, SuspendArchive), wantArchive))
		}
		wantPin := t.PinnedAt != nil && autoSettle(r)
		if slices.Contains(r.SuspendedBy, SuspendPin) != wantPin {
			probs = append(probs, fmt.Sprintf("rule %s suspendPin=%t want=%t", r.ID, slices.Contains(r.SuspendedBy, SuspendPin), wantPin))
		}
		if r.About != "" && FindChange(t, r.About) == nil {
			probs = append(probs, fmt.Sprintf("rule %s references missing change %q", r.ID, r.About))
		}
		if unwatchable(t, r) {
			probs = append(probs, fmt.Sprintf("rule %s watches unwatchable PR condition %s on %s", r.ID, r.When.Type, r.When.Change))
		}
		if oneShotWatch(r) && r.Status == RuleArmed && r.LastValue {
			probs = append(probs, fmt.Sprintf("one-shot watch %s armed with lastValue=true", r.ID))
		}
	}
	seenView := map[string]bool{}
	snoozes := 0
	for _, r := range v.Rules {
		if r.ID == "" || r.Purpose == "" || r.Then.Kind == "" {
			probs = append(probs, fmt.Sprintf("malformed view rule id=%q purpose=%q then=%q", r.ID, r.Purpose, r.Then.Kind))
		}
		if r.ID != "" && seenView[r.ID] {
			probs = append(probs, fmt.Sprintf("duplicate view rule %q", r.ID))
		}
		seenView[r.ID] = true
		if (r.Status == RuleSuspended) != (len(r.SuspendedBy) > 0) {
			probs = append(probs, fmt.Sprintf("view rule %s status=%s suspendedBy=%v", r.ID, r.Status, r.SuspendedBy))
		}
		if r.Purpose == PurposeSnooze {
			snoozes++
		}
	}
	if snoozes > 1 {
		probs = append(probs, fmt.Sprintf("view has %d snooze rules", snoozes))
	}
	return strings.Join(probs, "; ")
}

func checkDecisionsInvalid(t *Thread) string {
	var probs []string
	seen := map[string]bool{}
	for _, d := range t.Decisions {
		if d.ID == "" {
			probs = append(probs, "decision with empty id")
		} else if seen[d.ID] {
			probs = append(probs, fmt.Sprintf("duplicate decision %s", d.ID))
		}
		seen[d.ID] = true
		if d.Kind == DecisionEffect && (d.Effect == nil || d.Effect.Action == "") {
			probs = append(probs, fmt.Sprintf("effect decision %s missing effect action", d.ID))
			continue
		}
		if d.Kind == DecisionAsk && d.Async && t.ArchivedAt != nil {
			probs = append(probs, fmt.Sprintf("async ask %s open on archived thread", d.ID))
		}
		if why := askVoid(t, d); why != "" {
			probs = append(probs, fmt.Sprintf("void decision %s still open (%s)", d.ID, why))
		}
		if isMergeAsk(d) {
			if g, why, _ := mergeGate(t, d.Effect); g != gateOpen {
				probs = append(probs, fmt.Sprintf("open merge ask %s gate=%d (%s)", d.ID, g, why))
			}
		}
	}
	for _, d := range t.HeldAsks {
		if d.ID == "" {
			probs = append(probs, "held ask with empty id")
		} else if seen[d.ID] {
			probs = append(probs, fmt.Sprintf("duplicate held/open decision %s", d.ID))
		}
		seen[d.ID] = true
		if !isMergeAsk(d) {
			probs = append(probs, fmt.Sprintf("non-merge ask %s in heldAsks", d.ID))
			continue
		}
		if why := askVoid(t, d); why != "" {
			probs = append(probs, fmt.Sprintf("void held ask %s still held (%s)", d.ID, why))
		}
		if g, why, _ := mergeGate(t, d.Effect); g != gateWait {
			probs = append(probs, fmt.Sprintf("held merge ask %s gate=%d (%s)", d.ID, g, why))
		}
	}
	return strings.Join(probs, "; ")
}

func checkChangesInvalid(t *Thread) string {
	var probs []string
	seenID := map[string]bool{}
	seenPR := map[string]bool{}
	for i := range t.Changes {
		c := &t.Changes[i]
		if c.ID == "" {
			probs = append(probs, "change with empty id")
		} else if seenID[c.ID] {
			probs = append(probs, fmt.Sprintf("duplicate change id %s", c.ID))
		}
		seenID[c.ID] = true
		if !c.Phase.Valid() {
			probs = append(probs, fmt.Sprintf("change %s invalid phase %q", c.ID, c.Phase))
		}
		if u := c.PRURL(); u != "" {
			if seenPR[u] {
				probs = append(probs, fmt.Sprintf("duplicate PR URL %s across changes", u))
			}
			seenPR[u] = true
		}
		if c.PR != nil && c.PhaseBy == ByObserved {
			switch c.PR.State {
			case "OPEN":
				if c.Phase != PhaseReview {
					probs = append(probs, fmt.Sprintf("change %s OPEN PR has observed phase %s", c.ID, c.Phase))
				}
			case "MERGED":
				if c.Phase != PhaseLanded && c.Phase != PhaseLive {
					probs = append(probs, fmt.Sprintf("change %s MERGED PR has observed phase %s", c.ID, c.Phase))
				}
			case "CLOSED", PRMissing:
				if c.Phase != PhaseAbandoned {
					probs = append(probs, fmt.Sprintf("change %s %s PR has observed phase %s", c.ID, c.PR.State, c.Phase))
				}
			}
			if c.PR.State != "OPEN" {
				for _, k := range c.Conditions {
					if k.Type == CondChecks || k.Type == CondReview || k.Type == CondConflict || k.Type == CondMerge {
						probs = append(probs, fmt.Sprintf("change %s non-OPEN PR (%s) still has %s condition", c.ID, c.PR.State, k.Type))
						break
					}
				}
			}
		}
		if c.Phase.Reached(PhaseLanded) && c.LandedAt == nil {
			probs = append(probs, fmt.Sprintf("change %s phase %s missing landedAt", c.ID, c.Phase))
		}
		if _, shouldDone := doneNow(t, c); shouldDone && !c.IsDone() {
			probs = append(probs, fmt.Sprintf("change %s phase %s should be done", c.ID, c.Phase))
		}
		if c.Phase.InFlight() && !c.MarkedDone() && c.IsDone() {
			probs = append(probs, fmt.Sprintf("change %s in-flight (%s) has non-owner done mark", c.ID, c.Phase))
		}
		if c.Parked != nil && !c.ParkHolds() {
			probs = append(probs, fmt.Sprintf("change %s has stale park", c.ID))
		}
	}
	if t.MainChange != "" {
		c := FindChange(t, t.MainChange)
		if c == nil || c.PR == nil || c.Dismissed {
			probs = append(probs, fmt.Sprintf("mainChange %q does not point to an active PR change", t.MainChange))
		}
	}
	return strings.Join(probs, "; ")
}

func checkOutboxInvalid(t *Thread) string {
	var probs []string
	seen := map[string]bool{}
	for _, m := range t.Outbox {
		if m.ID == "" {
			probs = append(probs, "outbox entry with empty id")
			continue
		}
		if seen[m.ID] {
			probs = append(probs, fmt.Sprintf("duplicate outbox id %s", m.ID))
		}
		seen[m.ID] = true
		if m.State != "sent" && m.State != "queued" {
			probs = append(probs, fmt.Sprintf("outbox %s has terminal/invalid state %q", m.ID, m.State))
		}
	}
	return strings.Join(probs, "; ")
}

func checkFeedInvalid(t *Thread) string {
	var probs []string
	if len(t.Feed.Queue) > maxQueue {
		probs = append(probs, fmt.Sprintf("feed queue=%d > %d", len(t.Feed.Queue), maxQueue))
	}
	if len(t.Feed.Said) > maxSaid {
		probs = append(probs, fmt.Sprintf("feed said=%d > %d", len(t.Feed.Said), maxSaid))
	}
	seen := map[string]bool{}
	for _, n := range t.Feed.Queue {
		if n.Urgency != Wake && n.Urgency != Next && n.Urgency != Stop {
			probs = append(probs, fmt.Sprintf("feed notice %q invalid urgency %q", n.Key, n.Urgency))
		}
		if n.Key == "" {
			probs = append(probs, "feed notice with empty coalesceKey")
		} else if seen[n.Key] {
			probs = append(probs, fmt.Sprintf("duplicate feed key %q", n.Key))
		}
		seen[n.Key] = true
	}
	return strings.Join(probs, "; ")
}

// expectedSection maps every State to its unique Section (SPEC 6.1, 6.2).
func expectedSection(s State) Section {
	switch s {
	case Archived:
		return ArchivedSec
	case Settled:
		return SettledSec
	case Snoozed:
		return SnoozedSec
	case Asking, Failed, Blocked:
		return NeedsYou
	case Working, Waiting:
		return WorkingSec
	case Done, Seen:
		return SeenSec
	}
	return ""
}

func checkAttentionPlacement(t *Thread, v *View, now time.Time, on func(repo string) bool) string {
	a := ComputeWith(t, v, now, on)
	var probs []string

	// 1. State <-> Section bijection.
	if want := expectedSection(a.State); want == "" || a.Section != want {
		probs = append(probs, fmt.Sprintf("state=%q in section=%q (want %q)", a.State, a.Section, want))
	}

	// 2. Group validity (V0.row-group-code).
	validGroup := a.Group == GroupAnswer || a.Group == GroupApprove || a.Group == GroupUnblock
	if a.Section == NeedsYou && !validGroup {
		probs = append(probs, fmt.Sprintf("NeedsYou state=%s has invalid group=%q", a.State, a.Group))
	}
	if a.Section != NeedsYou && a.Group != "" {
		probs = append(probs, fmt.Sprintf("non-NeedsYou section=%s has group=%q", a.Section, a.Group))
	}
	if a.State == Failed && a.Group != GroupUnblock {
		probs = append(probs, fmt.Sprintf("Failed has group=%q want %q", a.Group, GroupUnblock))
	}

	// 3. Reason required for Waiting, Blocked, Snoozed.
	if (a.State == Waiting || a.State == Blocked || a.State == Snoozed) && strings.TrimSpace(a.Reason) == "" {
		probs = append(probs, fmt.Sprintf("state=%s has empty reason", a.State))
	}

	// 4. Lifecycle & snooze precedence guarantees.
	if t.ArchivedAt != nil {
		if a.State != Archived {
			probs = append(probs, fmt.Sprintf("archived thread computed as %s", a.State))
		}
		return strings.Join(probs, "; ")
	}
	if t.Settle.At != nil {
		if a.State != Settled {
			probs = append(probs, fmt.Sprintf("settled thread computed as %s", a.State))
		}
		return strings.Join(probs, "; ")
	}

	asks := ownerAsks(t, on)
	quotaWait := QuotaWaitText(t, now)
	if until, since := SnoozeOf(v); until != nil && until.After(now) {
		if SnoozedWhenDone(v) && WorkOverAt(t, now) && a.State == Snoozed {
			probs = append(probs, "snooze-when-done still Snoozed after workOver")
		}
		for _, d := range asks {
			if d.CreatedAt.After(*since) && a.State == Snoozed {
				probs = append(probs, fmt.Sprintf("new ask %s hidden by snooze", d.ID))
			}
		}
		// V0.quota-wait-midturn: a quota wait is not a failure until its reset passes.
		if t.Failure != nil && quotaWait == "" && t.Failure.At.After(*since) && a.State == Snoozed {
			probs = append(probs, "new failure hidden by snooze")
		}
		if a.State == Snoozed {
			return strings.Join(probs, "; ")
		}
	}

	// 5. Active thread placement guarantees.
	switch {
	case len(asks) > 0:
		if a.State != Asking || a.Section != NeedsYou {
			probs = append(probs, fmt.Sprintf("%d open owner ask(s) placed in %s/%s", len(asks), a.State, a.Section))
		}
		if want := askGroup(asks); a.Group != want {
			probs = append(probs, fmt.Sprintf("Asking group=%q want %q", a.Group, want))
		}
	case quotaWait != "":
		if a.State != Waiting || a.Section != WorkingSec {
			probs = append(probs, fmt.Sprintf("quota wait placed in %s/%s", a.State, a.Section))
		}
	case t.Failure != nil:
		if a.State != Failed || a.Section != NeedsYou {
			probs = append(probs, fmt.Sprintf("failure (%s) hidden as %s/%s", t.Failure.Class, a.State, a.Section))
		}
	case t.Unresponsive != nil:
		if a.State != Blocked || a.Section != NeedsYou {
			probs = append(probs, fmt.Sprintf("unresponsive agent placed in %s/%s", a.State, a.Section))
		}
	case t.Running:
		if a.State != Working || a.Section != WorkingSec {
			probs = append(probs, fmt.Sprintf("running thread placed in %s/%s", a.State, a.Section))
		}
	case t.MachineWait == nil && t.Starting != nil:
		if a.State != Working || a.Section != WorkingSec {
			probs = append(probs, fmt.Sprintf("starting thread placed in %s/%s", a.State, a.Section))
		}
	case t.MachineWait != nil:
		if a.State != Waiting || a.Section != WorkingSec {
			probs = append(probs, fmt.Sprintf("machineWait thread placed in %s/%s", a.State, a.Section))
		}
	case t.RunningTasks() > 0:
		if a.State != Waiting || a.Section != WorkingSec {
			probs = append(probs, fmt.Sprintf("%d running task(s) placed in %s/%s", t.RunningTasks(), a.State, a.Section))
		}
	case func() bool { _, n := threadNeed(t, on); return n != nil }():
		if a.State != Blocked || a.Section != NeedsYou {
			probs = append(probs, fmt.Sprintf("threadNeed placed in %s/%s", a.State, a.Section))
		}
	case PendingRequest(t) != nil:
		if a.State != Blocked || a.Section != NeedsYou {
			probs = append(probs, fmt.Sprintf("pending Run card placed in %s/%s", a.State, a.Section))
		}
	case t.SettleAsk != nil:
		if a.State != Blocked || a.Section != NeedsYou {
			probs = append(probs, fmt.Sprintf("settleAsk placed in %s/%s", a.State, a.Section))
		}
	case ciWait(t) != "":
		if a.State != Waiting || a.Section != WorkingSec {
			probs = append(probs, fmt.Sprintf("ciWait placed in %s/%s", a.State, a.Section))
		}
	case WatchWaitText(t, now) != "":
		if a.State != Waiting || a.Section != WorkingSec {
			probs = append(probs, fmt.Sprintf("armed watch placed in %s/%s", a.State, a.Section))
		}
	default:
		want := Seen
		if t.ActivityAt.After(v.SeenAt) {
			want = Done
		}
		if a.State != want || a.Section != SeenSec {
			probs = append(probs, fmt.Sprintf("idle thread placed in %s/%s want %s/%s", a.State, a.Section, want, SeenSec))
		}
	}
	return strings.Join(probs, "; ")
}

func checkAvailabilityMismatch(t *Thread, v *View, now time.Time) string {
	var probs []string
	avail := func(name string) bool {
		a := Registry[name]
		return a != nil && a.Available(t, v, now) == ""
	}
	wantAvail := func(name string, want bool, ctx string) {
		if got := avail(name); got != want {
			probs = append(probs, fmt.Sprintf("%s available=%t want=%t (%s)", name, got, want, ctx))
		}
	}

	if t.ArchivedAt != nil {
		wantAvail("thread.unarchive", true, "archived")
		for _, cmd := range []string{"thread.archive", "thread.send", "thread.retry", "thread.settle", "thread.settleWhenDone", "thread.snooze", "thread.snoozeWhenDone", "thread.pin", "thread.setView", "thread.handover", "thread.setModel", KindWakeOnQuota} {
			wantAvail(cmd, false, "archived")
		}
	} else {
		wantAvail("thread.unarchive", false, "not archived")
		if t.Settle.At != nil {
			wantAvail("thread.unsettle", true, "settled")
			for _, cmd := range []string{"thread.settle", "thread.settleWhenDone", "thread.snooze", "thread.snoozeWhenDone", "thread.pin", KindWakeOnQuota} {
				wantAvail(cmd, false, "settled")
			}
		}
	}

	wantAvail("thread.interrupt", t.Running, fmt.Sprintf("running=%t", t.Running))
	wantAvail("thread.answer", len(t.Decisions) > 0, fmt.Sprintf("decisions=%d", len(t.Decisions)))
	if len(t.Decisions) > 0 {
		wantAvail("thread.send", false, "decisions open")
		wantAvail("thread.handover", false, "decisions open")
	}
	if t.Running {
		wantAvail("thread.archive", false, "live run")
		wantAvail("thread.handover", false, "live run")
	}
	wantAvail("thread.dismiss", t.Failure != nil, fmt.Sprintf("failure=%t", t.Failure != nil))
	if t.ArchivedAt == nil {
		wantAvail("thread.retry", t.Failure != nil, fmt.Sprintf("failure=%t", t.Failure != nil))
	}
	wantAvail("thread.restart", t.Unresponsive != nil, fmt.Sprintf("unresponsive=%t", t.Unresponsive != nil))
	return strings.Join(probs, "; ")
}

// OverdueRule returns a description of the first armed rule on t or v whose
// condition holds at now with LastValue==false and no future RetryAt, or "".
// After a Reduce / Tick clock pass, no such rule should remain unfired.
func OverdueRule(t *Thread, v *View, now time.Time) string {
	if t == nil {
		return ""
	}
	check := func(where string, rs []Rule) string {
		for _, r := range rs {
			if r.Status != RuleArmed || r.LastValue {
				continue
			}
			if r.RetryAt != nil && now.Before(*r.RetryAt) {
				continue
			}
			if r.When.eval(t, r.Armed, now) == yes {
				return fmt.Sprintf("%s rule %s (%s) holds at %s without firing", where, r.ID, r.Purpose, now.UTC().Format(time.RFC3339))
			}
		}
		return ""
	}
	if s := check("thread", t.Rules); s != "" {
		return s
	}
	if v != nil {
		if s := check("view", v.Rules); s != "" {
			return s
		}
	}
	return ""
}

// OldestWakingNotice returns the earliest At among waking notices within budget
// on t's feed at now, or zero if none may wake.
func OldestWakingNotice(t *Thread, now time.Time) time.Time {
	if t == nil || !t.wakeable() {
		return time.Time{}
	}
	var oldest time.Time
	for _, n := range t.Feed.Queue {
		if !t.Feed.waking(n, now) {
			continue
		}
		if oldest.IsZero() || n.At.Before(oldest) {
			oldest = n.At
		}
	}
	return oldest
}
