package core

// The load passes (V0.structure-cost (2)): every repair of a record stored by an
// older build lives here. DecodeThread reads the shapes stored before; the
// engine's Load calls Migrate once per thread and MigrateRecord once per
// host record, and stores what they changed.

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

// Migrate repairs a thread stored by an older build, once at load: the
// observed changes stored before #35 move into the host records rs, a change
// that is not code ends (V0.changes-code-only (4)), what landed or ended
// before V0.98 is done, and a Rule on a retired Condition kind is brought
// up to date. It reports whether t changed.
func Migrate(t *Thread, rs map[string]*Change, now time.Time) bool {
	changed := migrateObserved(t, rs)
	changed = migrateCodeOnly(t, now) || changed
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

// DecodeThread reads a stored thread, also in the shape stored before #31:
// the thread's runs in offerRuns, the view's actions and runs mixed into the
// thread's, and a change's actions under "actions"; and before #32: the PR on
// the thread, dismissedPRs and settleWhenMerged; and before #33: the
// settle in settledAt, settleWhenDone and settleWhen, and the live view's
// flag in liveView; and before #46: settle.when and a change's deployWatch,
// which become Rules.
func DecodeThread(b []byte) (*Thread, error) {
	t := &Thread{}
	if err := json.Unmarshal(b, t); err != nil {
		return nil, err
	}
	if err := migrateComponents(t, b); err != nil {
		return nil, err
	}
	if err := migratePR(t, b); err != nil {
		return nil, err
	}
	if err := migrateRules(t, b); err != nil {
		return nil, err
	}
	for i := range t.Changes { // V0.70: stored before landedAt
		c := &t.Changes[i]
		if c.LandedAt == nil && c.Phase.Reached(PhaseLanded) {
			at := c.UpdatedAt
			c.LandedAt = &at
		}
	}
	if bytes.Contains(b, []byte(`"offerRuns"`)) || bytes.Contains(b, []byte(`"actions"`)) {
		var old struct {
			Runs    []ActionRun `json:"offerRuns"`
			Changes []struct {
				Offers []ActionSpec `json:"actions"`
			} `json:"changes"`
		}
		if err := json.Unmarshal(b, &old); err != nil {
			return nil, err
		}
		if len(t.Runs) == 0 {
			t.Runs = old.Runs
		}
		for i := range old.Changes {
			if i < len(t.Changes) && len(t.Changes[i].Offers) == 0 {
				t.Changes[i].Offers = old.Changes[i].Offers
			}
		}
	}
	isView := func(on string) bool { return on == OnView }
	if slices.ContainsFunc(t.Offers, func(a ActionSpec) bool { return isView(a.On) }) || slices.ContainsFunc(t.Runs, func(r ActionRun) bool { return isView(r.On) }) {
		if t.Live == nil {
			t.Live = &LiveView{}
		}
		for _, a := range t.Offers {
			if isView(a.On) {
				t.Live.Offers = append(t.Live.Offers, a)
			}
		}
		for _, r := range t.Runs {
			if isView(r.On) {
				t.Live.Runs = append(t.Live.Runs, r)
			}
		}
		t.Offers = slices.DeleteFunc(t.Offers, func(a ActionSpec) bool { return isView(a.On) })
		t.Runs = slices.DeleteFunc(t.Runs, func(r ActionRun) bool { return isView(r.On) })
	}
	return t, nil
}

// migrateComponents moves a thread stored before #33 into its Settle and
// Live components.
func migrateComponents(t *Thread, b []byte) error {
	keys := []string{`"settledAt"`, `"settleWhenDone"`, `"settleWhen"`, `"liveView"`}
	if !slices.ContainsFunc(keys, func(k string) bool { return bytes.Contains(b, []byte(k)) }) {
		return nil
	}
	var old struct {
		SettledAt      *time.Time `json:"settledAt"`
		SettleWhenDone bool       `json:"settleWhenDone"`
		SettleWhen     *struct {
			Change string `json:"change"`
			Phase  Phase  `json:"phase"`
		} `json:"settleWhen"`
		LiveView bool `json:"liveView"`
	}
	if err := json.Unmarshal(b, &old); err != nil {
		return err
	}
	if t.Settle.At == nil && !Settling(t) {
		t.Settle.At = old.SettledAt
		if old.SettleWhenDone {
			SettleWhenDone(t, ByOwner, t.UpdatedAt)
		}
		if w := old.SettleWhen; w != nil && w.Change != "" {
			SettleWhenReached(t, w.Change, w.Phase, ByOwner, t.UpdatedAt)
		}
	}
	if old.LiveView {
		if t.Live == nil {
			t.Live = &LiveView{}
		}
		t.Live.Enabled = true
	}
	return nil
}

// migrateRules moves what watched before #46 into Rules: settle.when (#33)
// into settle Rules, a change's deployWatch that had not ended into its
// deploy Rule, and a change with a PR gets the Rule of its built-in actions.
// A deploy Rule stored before #48 also ends when its change ends.
func migrateRules(t *Thread, b []byte) error {
	now := t.UpdatedAt
	if bytes.Contains(b, []byte(`"when"`)) || bytes.Contains(b, []byte(`"deployWatch"`)) {
		var old struct {
			Settle struct {
				When json.RawMessage `json:"when"`
			} `json:"settle"`
			Changes []struct {
				ID          string `json:"id"`
				DeployWatch *struct {
					Repo     string    `json:"repo"`
					SHA      string    `json:"sha"`
					MergedAt time.Time `json:"mergedAt"`
					Checked  bool      `json:"checked"`
					Seen     bool      `json:"seen"`
					Done     bool      `json:"done"`
				} `json:"deployWatch"`
			} `json:"changes"`
		}
		if err := json.Unmarshal(b, &old); err != nil {
			return err
		}
		// settle.when was a list of {kind, change, phase}; settle Rules have
		// their own "when" objects, which are not stored under settle.
		var conds []struct {
			Kind   string `json:"kind"`
			Change string `json:"change"`
			Phase  Phase  `json:"phase"`
		}
		json.Unmarshal(old.Settle.When, &conds)
		for _, w := range conds {
			switch w.Kind {
			case "done":
				SettleWhenDone(t, ByOwner, now)
			case "change":
				SettleWhenReached(t, w.Change, w.Phase, ByOwner, now)
			}
		}
		for _, oc := range old.Changes {
			w, c := oc.DeployWatch, FindChange(t, oc.ID)
			if w == nil || w.Done || c == nil || FindRule(t, deployRuleID(c.ID)) != nil {
				continue
			}
			ArmRule(t, Rule{ID: deployRuleID(c.ID), Purpose: PurposeDeploy, About: c.ID, When: deployWhen(c, w.MergedAt, w.Seen),
				Then: Then{Kind: KindRuleEnd}, After: AfterDisarm, CreatedBy: BySystem,
				Observe: &Observe{Observer: ObsDeploy, Repo: w.Repo, SHA: w.SHA, Since: w.MergedAt, Read: w.Checked}}, now)
		}
	}
	for i := range t.Changes {
		armBuiltins(t, &t.Changes[i], now)
	}
	for i := range t.Rules {
		endsWithChange(&t.Rules[i])
	}
	return nil
}

// migratePR moves a thread stored before #32 onto its changes: a change's
// "pr" evidence becomes its PR, thread.pr goes to its change (made when none
// has it) and that change is the main one, a dismissed PR's change is
// dismissed, and settleWhenMerged waits for the main change to land.
func migratePR(t *Thread, b []byte) error {
	for i := range t.Changes {
		c := &t.Changes[i]
		for _, ev := range c.Evidence {
			if ev.Kind == "pr" && c.PR == nil && ev.URL != "" {
				repo, n := parsePRURL(ev.URL)
				c.PR = &PR{URL: ev.URL, Repo: repo, Number: n, State: "OPEN", Checks: ChecksPending}
				switch {
				case c.Phase == PhaseAbandoned:
					c.PR.State = "CLOSED"
				case !c.Phase.InFlight():
					c.PR.State = "MERGED"
				}
			}
		}
	}
	if !bytes.Contains(b, []byte(`"pr"`)) && !bytes.Contains(b, []byte(`"dismissedPRs"`)) && !bytes.Contains(b, []byte(`"settleWhenMerged"`)) {
		return nil
	}
	var old struct {
		PR               *PR      `json:"pr"`
		DismissedPRs     []string `json:"dismissedPRs"`
		SettleWhenMerged bool     `json:"settleWhenMerged"`
	}
	if err := json.Unmarshal(b, &old); err != nil {
		return err
	}
	if p := old.PR; p != nil && p.URL != "" && t.MainChange == "" {
		at := t.UpdatedAt
		if p.CheckedAt != nil {
			at = *p.CheckedAt
		}
		activity := t.ActivityAt
		c := changeByPR(t, p.URL)
		if c == nil || c.PR == nil || c.PR.CheckedAt == nil {
			c = ChangePRObserved(t, *p, at)
		}
		t.ActivityAt = activity // moving the PR is not news
		if c != nil {
			t.MainChange = c.ID
			if old.SettleWhenMerged && p.State == "OPEN" {
				SettleWhenReached(t, c.ID, PhaseLanded, ByOwner, t.UpdatedAt)
			}
		}
	}
	for _, u := range old.DismissedPRs {
		if c := changeByPR(t, u); c != nil {
			c.Dismissed = true
			if t.MainChange == c.ID {
				t.MainChange = ""
			}
		}
	}
	return nil
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

// migrateObserved moves the observed git changes a thread stored before #35
// into host records rs (by id; the newest copy wins) and gives the thread a
// reference to each. One that the agent or a PR added to (an alias, a PR,
// actions, evidence, a label, a Rule) stays in the thread and follows its
// record.
// It reports whether the thread changed.
func migrateObserved(t *Thread, rs map[string]*Change) bool {
	changed := false
	rules, _ := json.Marshal(t.Rules)
	keep := t.Changes[:0:0]
	for _, c := range t.Changes {
		if c.Kind != KindCode || !strings.HasPrefix(c.Key, "git:") || c.Observed != "" {
			keep = append(keep, c)
			continue
		}
		changed = true
		rec := cloneChange(&c)
		rec.Aliases, rec.PR, rec.Actions, rec.Evidence, rec.Label, rec.Group, rec.After, rec.Dismissed, rec.Annotations = nil, nil, Actions{}, nil, "", "", nil, false, nil
		repo := RecordRepo(rec)
		if rec.Workspace == nil {
			rec.Workspace = &Workspace{Repo: repo, Path: t.Cwd, Branch: rec.Branch}
		}
		if old := rs[rec.ID]; old == nil || rec.UpdatedAt.After(old.UpdatedAt) {
			rs[rec.ID] = rec
		}
		if !slices.ContainsFunc(t.Refs, func(x Ref) bool { return x.ID == rec.ID }) {
			t.Refs = append(t.Refs, Ref{ID: rec.ID, Repo: repo, Branch: rec.Branch, Ended: !rec.Phase.InFlight()})
		}
		pure := c.PR == nil && len(c.Aliases) == 0 && len(c.Offers) == 0 && len(c.Runs) == 0 && len(c.Evidence) == 0 &&
			c.Label == "" && c.Group == "" && len(c.After) == 0 && t.MainChange != c.ID && !strings.Contains(string(rules), c.ID)
		if !pure {
			c.Observed = rec.ID
			keep = append(keep, c)
		}
	}
	if changed {
		t.Changes = keep
	}
	return changed
}
