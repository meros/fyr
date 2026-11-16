package core

// One work item, one card (V0.87 (2), issue 69). Fyr makes a change keyed
// pr:<repo>#<n> for a PR it finds before the agent names it. When the
// agent's own change for the same work gets that PR, or opens on that PR's
// branch, Fyr's change folds into the agent's: the agent's change keeps its
// id, key, kind and title, and takes everything Fyr observed.

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// PRKey is the key of the change Fyr makes for a PR it finds.
func PRKey(repo string, n int) string { return fmt.Sprintf("pr:%s#%d", repo, n) }

// madeForPR reports a change Fyr made for its PR: keyed by the PR.
func madeForPR(c *Change) bool {
	return c.PR != nil && c.PR.Number > 0 && c.Key == PRKey(c.PR.Repo, c.PR.Number)
}

// onePRChange is the thread's change for the PR at url after folding: when
// several changes hold it, the changes Fyr made for it fold into the first
// one it did not make. Nil when none holds it.
func onePRChange(t *Thread, url string, now time.Time) *Change {
	if url == "" {
		return nil
	}
	var ids []string
	keep := ""
	for i := range t.Changes {
		c := &t.Changes[i]
		if c.PRURL() != url {
			continue
		}
		ids = append(ids, c.ID)
		if keep == "" && !madeForPR(c) {
			keep = c.ID
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if keep == "" {
		keep = ids[0]
	}
	foldPRChanges(t, keep, now, func(x *Change) bool { return x.PRURL() == url })
	return findByID(t, keep)
}

// joinPRWork folds into c the changes Fyr made for c's PR, and, for a change
// on a branch that is not code (code joins by V0.70 (2)), the in-flight
// change Fyr made for a PR on that branch and repo. It returns c after the
// fold.
func joinPRWork(t *Thread, c *Change, now time.Time) *Change {
	if c == nil {
		return nil
	}
	id, url := c.ID, c.PRURL()
	branch, targets, kind := c.Branch, c.Targets, c.Kind
	foldPRChanges(t, id, now, func(x *Change) bool {
		if url != "" && x.PRURL() == url {
			return true
		}
		return kind != KindCode && branch != "" && x.Branch == branch && x.Phase.InFlight() && url == "" && sameRepo(targets, x.Targets)
	})
	return findByID(t, id)
}

// foldPRChanges folds every change Fyr made for a PR that match picks, other
// than the change keep, into keep.
func foldPRChanges(t *Thread, keep string, now time.Time, pick func(*Change) bool) {
	for {
		c := findByID(t, keep)
		if c == nil {
			return
		}
		i := slices.IndexFunc(t.Changes, func(x Change) bool { return x.ID != keep && madeForPR(&x) && pick(&x) })
		if i < 0 {
			return
		}
		x := t.Changes[i]
		foldChange(t, c, &x, now)
		t.Changes = slices.Delete(t.Changes, i, i+1)
	}
}

func findByID(t *Thread, id string) *Change {
	for i := range t.Changes {
		if t.Changes[i].ID == id {
			return &t.Changes[i]
		}
	}
	return nil
}

// foldChange moves x into c: x's key and aliases become aliases of c, and c
// takes x's PR snapshot when it is newer, x's observed phase and conditions
// unless c's are the owner's, its evidence, revisions, actions and Rules,
// and the thread's main change.
func foldChange(t *Thread, c, x *Change, now time.Time) {
	c.Aliases = union(c.Aliases, append([]string{x.Key}, x.Aliases...))
	c.Targets = union(c.Targets, x.Targets)
	c.After = union(c.After, x.After)
	if c.Branch == "" {
		c.Branch = x.Branch
	}
	if c.Group == "" {
		c.Group = x.Group
	}
	if c.Label == "" {
		c.Label = x.Label
	}
	if c.Needs == "" {
		c.Needs = x.Needs
	}
	c.Dismissed = c.Dismissed || x.Dismissed
	if c.Observed == "" {
		c.Observed, c.Workspace = x.Observed, x.Workspace
	}
	if x.PR != nil && (c.PR == nil || c.PR.CheckedAt == nil || (x.PR.CheckedAt != nil && x.PR.CheckedAt.After(*c.PR.CheckedAt))) {
		p := *x.PR
		c.PR = &p
	}
	for _, ev := range x.Evidence {
		addEvidence(c, ev)
	}
	if x.PhaseBy == ByObserved && CanWrite(FieldChangePhase, ByObserved, c.PhaseBy, "") == nil {
		c.Phase, c.PhaseBy, c.LandedAt = x.Phase, x.PhaseBy, x.LandedAt
	}
	for _, k := range x.Conditions {
		if k.Source != ByObserved {
			continue
		}
		if cur := condition(c, k.Type); cur == nil {
			c.Conditions = append(c.Conditions, k)
		} else if cur.Source != ByOwner {
			*cur = k
		}
	}
	if len(c.Annotations) == 0 {
		c.Annotations = x.Annotations
	}
	for _, r := range x.Revisions {
		if !slices.ContainsFunc(c.Revisions, func(y Revision) bool { return y.ID == r.ID }) {
			c.Revisions = append(c.Revisions, r)
		}
	}
	sort.SliceStable(c.Revisions, func(i, j int) bool { return c.Revisions[i].At.Before(c.Revisions[j].At) })
	c.Revisions = keepLast(c.Revisions, maxRevisions)
	for _, a := range x.Offers {
		if !slices.ContainsFunc(c.Offers, func(b ActionSpec) bool { return b.ID == a.ID }) {
			c.Offers = append(c.Offers, a)
		}
	}
	c.Runs = append(c.Runs, x.Runs...)
	if t.MainChange == x.ID {
		t.MainChange = c.ID
	}
	moveRules(t, x.ID, c.ID)
	c.UpdatedAt = now
}

// moveRules points the Rules about change from at change to: their ids,
// subjects, conditions and arguments. A Rule whose new id the thread already
// has is dropped.
func moveRules(t *Thread, from, to string) {
	var out []Rule
	for _, r := range t.Rules {
		b, _ := json.Marshal(r)
		if !strings.Contains(string(b), from) {
			out = append(out, r)
			continue
		}
		var moved Rule
		if json.Unmarshal([]byte(strings.ReplaceAll(string(b), from, to)), &moved) != nil {
			out = append(out, r)
			continue
		}
		if moved.ID != r.ID && (findIn(t.Rules, moved.ID) != nil || findIn(out, moved.ID) != nil) {
			continue
		}
		out = append(out, moved)
	}
	t.Rules = out
}
