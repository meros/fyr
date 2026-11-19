package core

// Observed changes are host records (#35, decision 0018 addendum): one
// record per repo and branch on the host, stored once with its own events.
// A thread holds references to the records it shows (every thread in the
// record's folder, and the thread whose run observed it); a record that
// moves is sent to each of them as a change.observed Input, so each thread
// gets its own notices. A change of the thread on the same repo and branch
// (the agent's of any kind, or a PR's: V0.87) follows the record: the record's git
// facts are copied onto it, and the thread shows that change instead of the
// record. Agent changes stay per thread.

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Workspace is where a code change's git facts are read (decision 0019): a
// checkout or worktree of one repo with one branch.
type Workspace struct {
	Repo   string `json:"repo,omitempty"`
	Path   string `json:"path,omitempty"`
	Branch string `json:"branch,omitempty"`
	Base   string `json:"base,omitempty"`
	// Head is the commit the git observer read last (V0.95 (4)): it finds
	// an ended record by its head when the record has no revision.
	Head string `json:"head,omitempty"`
	// MadeByFyr: Fyr made the worktree and removes it when the change ends
	// (#44).
	MadeByFyr bool `json:"madeByFyr,omitempty"`
}

// Ref is a thread's reference to a host change record. It holds the
// record's identity (repo and branch), so the thread links its own code
// change on that branch without reading the record.
type Ref struct {
	ID     string `json:"id"`
	Repo   string `json:"repo,omitempty"`
	Branch string `json:"branch"`
	// Ended: the record landed or ended; a new change on the branch no
	// longer follows it.
	Ended bool `json:"ended,omitempty"`
}

// The Input kinds of a host change record, and the thread's kind for a
// record it references.
const (
	// KindGitObserved is the facts of one checkout (args GitFacts): they
	// make or update the record of their repo and branch.
	KindGitObserved = "git.observed"
	// KindGitEnded is a record's branch merged into its base, or gone
	// without a merge (args GitEnd).
	KindGitEnded = "git.ended"
	// KindChangeObserved tells a thread that a record it shows moved (args
	// ChangeObservedArgs).
	KindChangeObserved = "change.observed"
)

// GitEnd is how a record's branch ended.
type GitEnd struct {
	Change string
	Merged bool
}

// ChangeObservedArgs is a record before (nil when new) and after an Input.
type ChangeObservedArgs struct {
	Before *Change
	After  Change
}

// IsRecordKind reports an Input kind of a host change record.
func IsRecordKind(kind string) bool {
	return kind == KindGitObserved || kind == KindGitEnded || kind == KindRecordDone || kind == KindRecordEnd
}

// ObservedKey is the key of the record of repo and branch.
func ObservedKey(repo, branch string) string { return "git:" + repo + "#" + branch }

// RecordRepo is the repo of a record: its workspace's, else its repo target.
func RecordRepo(c *Change) string {
	if c.Workspace != nil && c.Workspace.Repo != "" {
		return c.Workspace.Repo
	}
	for _, x := range c.Targets {
		if r, ok := strings.CutPrefix(x, "repo:"); ok {
			return r
		}
	}
	return ""
}

// RecordOn is the record of repo and branch that has not landed, or nil.
func RecordOn(rs []*Change, repo, branch string) *Change {
	for _, c := range rs {
		if c.Branch == branch && c.Phase.InFlight() && !c.IsDone() && RecordRepo(c) == repo {
			return c
		}
	}
	return nil
}

// RecordHead is a record's head: its newest revision, else the head the
// git observer stored on its workspace (V0.95 (4)), else "".
func RecordHead(c *Change) string {
	if n := len(c.Revisions); n > 0 {
		return c.Revisions[n-1].ID
	}
	if c.Workspace != nil {
		return c.Workspace.Head
	}
	return ""
}

// endedAt reports an ended record of repo and branch whose head is head: a
// merged branch that stays checked out (V0.88 (1)). With head unknown, any
// ended record of the branch is that work (V0.95 (4)).
func endedAt(rs []*Change, repo, branch, head string) bool {
	return slices.ContainsFunc(rs, func(c *Change) bool {
		if c.Branch != branch || (c.Phase.InFlight() && !c.IsDone()) || RecordRepo(c) != repo {
			return false
		}
		return head == "" || sameCommit(RecordHead(c), head)
	})
}

// defaultBranch reports the repo's default branch: origin/HEAD's branch,
// else the base's branch, else main or master (V0.95 (4)).
func defaultBranch(g GitFacts) bool {
	switch {
	case g.Default != "":
		return g.Branch == g.Default
	case g.Base != "" && g.Base != "origin/HEAD":
		return g.Branch == strings.TrimPrefix(g.Base, "origin/")
	}
	return g.Branch == "main" || g.Branch == "master"
}

// sameCommit compares two commit ids that may be short.
func sameCommit(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// MergedPRCovers reports a merged PR that ends rec: the PR's head is rec's
// newest revision, or either is unknown (V0.88 (2)).
func MergedPRCovers(p *PR, rec *Change) bool {
	if p == nil || p.State != "MERGED" {
		return false
	}
	head := RecordHead(rec)
	if p.Head == "" || head == "" {
		return true
	}
	return sameCommit(p.Head, head)
}

// ReduceRecord applies one Input of a record kind to a copy of the record
// it is about, among the host's records rs (pure). It returns the record
// before (nil for a new one) and after (nil when the Input is about no
// record), and the record's events.
func ReduceRecord(rs []*Change, in Input, now time.Time) (before, after *Change, evs []Event, err error) {
	// V0.98 (4): the owner's Done is the one Input a record takes from
	// anyone but an observer.
	want := ByObserved
	if in.Kind == KindRecordDone || in.Kind == KindRecordEnd { // V0.101: and Abandon
		want = ByOwner
	}
	if in.Source.Principal() != want {
		return nil, nil, nil, reject(RejectRights, "%s cannot send %s", in.Source, in.Kind)
	}
	event := func(c *Change, op string) Event {
		return Event{"change", map[string]any{"op": op, "change": map[string]any{"id": c.ID, "title": c.Title, "kind": c.Kind, "phase": c.Phase, "branch": c.Branch, "repo": RecordRepo(c)}}}
	}
	switch in.Kind {
	case KindGitObserved:
		g, err := argsOf[GitFacts](in)
		if err != nil || g.Branch == "" {
			return nil, nil, nil, err // outside git: nothing
		}
		c := RecordOn(rs, g.Repo, g.Branch)
		if c == nil {
			// V0.95 (4): only commits ahead of the base on a branch that is
			// not the default make a record.
			if g.Ahead == 0 || defaultBranch(g) {
				return nil, nil, nil, nil
			}
			if endedAt(rs, g.Repo, g.Branch, g.Head) {
				return nil, nil, nil, nil // V0.88 (1): the work that ended, still checked out
			}
			key := freshKey(rs, ObservedKey(g.Repo, g.Branch))
			c = &Change{ID: ChangeID(key), Key: key, Kind: KindCode, Title: g.Branch, Branch: g.Branch, Phase: PhaseWorking, PhaseBy: ByObserved, CreatedAt: now, UpdatedAt: now, TouchedAt: &now}
			if g.Repo != "" {
				c.Targets = []string{"repo:" + g.Repo}
			}
			evs = append(evs, event(c, "observed"))
		} else {
			before, c = c, cloneChange(c)
			if defaultBranch(g) {
				// V0.105 (2): an old record on the default branch is done
				// and never observed again.
				c.Done = &DoneMark{At: now, By: ByObserved, Reason: DoneDefaultBranch}
				c.UpdatedAt, c.TouchedAt = now, &now
				return before, c, []Event{event(c, "done")}, nil
			}
		}
		ws := &Workspace{Repo: g.Repo, Path: g.Path, Branch: g.Branch, Base: g.Base, Head: g.Head}
		if c.Workspace != nil {
			ws.MadeByFyr = c.Workspace.MadeByFyr
			if ws.Path == "" {
				ws.Path = c.Workspace.Path
			}
			if ws.Head == "" {
				ws.Head = c.Workspace.Head
			}
		}
		c.Workspace = ws
		gitConditions(c, g, now)
		if before != nil && !sameChange(before, c) {
			c.UpdatedAt, c.TouchedAt = now, &now
		}
		return before, c, evs, nil
	case KindGitEnded:
		a, err := argsOf[GitEnd](in)
		if err != nil {
			return nil, nil, nil, err
		}
		i := slices.IndexFunc(rs, func(c *Change) bool { return c.ID == a.Change })
		if i < 0 || !rs[i].Phase.InFlight() {
			return nil, nil, nil, nil
		}
		before = rs[i]
		c := cloneChange(before)
		phase, op := PhaseAbandoned, "gone"
		if a.Merged {
			phase, op = PhaseLanded, "merged"
		}
		setPhase(nil, c, phase, ByObserved, now)
		// Pushed and clean described the branch before it ended.
		dropConditions(c, CondPushed, CondClean)
		c.TouchedAt = &now
		if c.Done == nil { // V0.98 (1)
			c.Done = &DoneMark{At: now, By: ByObserved, Reason: string(phase)}
			if a.Merged {
				c.Done.Reason = "merged"
			}
		}
		return before, c, []Event{event(c, op)}, nil
	case KindRecordDone:
		before, c, err := recordDone(rs, in, now)
		if err != nil {
			return nil, nil, nil, err
		}
		return before, c, []Event{event(c, "done")}, nil
	case KindRecordEnd:
		before, c, err := recordEnd(rs, in, now)
		if err != nil {
			return nil, nil, nil, err
		}
		return before, c, []Event{event(c, "end")}, nil
	}
	return nil, nil, nil, reject(RejectBadArgs, "unknown action %s", in.Kind)
}

// gitConditions sets pushed and clean from the facts, and a revision for
// commits ahead of the base.
func gitConditions(c *Change, g GitFacts, now time.Time) {
	if g.Unpushed > 0 {
		setCondition(c, CondPushed, CondPending, plural(g.Unpushed, "commit")+" not pushed", ByObserved, now)
	} else {
		setCondition(c, CondPushed, CondOK, "", ByObserved, now)
	}
	if g.Dirty > 0 {
		setCondition(c, CondClean, CondPending, plural(g.Dirty, "file")+" changed, not committed", ByObserved, now)
	} else {
		setCondition(c, CondClean, CondOK, "", ByObserved, now)
	}
	if g.Head != "" && g.Ahead > 0 {
		addRevision(c, Revision{ID: g.Head, Summary: fmt.Sprintf("%s ahead of %s", plural(g.Ahead, "commit"), g.Base), By: ByObserved}, now)
	}
}

// freshKey is key, or key~N when a record with that key has landed or
// ended: new work after that is a new record (V0.59 (3)).
func freshKey(rs []*Change, key string) string {
	k := key
	for n := 2; slices.ContainsFunc(rs, func(c *Change) bool { return c.Key == k }); n++ {
		k = fmt.Sprintf("%s~%d", key, n)
	}
	return k
}

func cloneChange(c *Change) *Change {
	b, _ := json.Marshal(c)
	out := &Change{}
	json.Unmarshal(b, out)
	return out
}

// sameChange compares two changes as stored, apart from UpdatedAt.
func sameChange(a, b *Change) bool {
	x, y := *a, *b
	x.UpdatedAt, y.UpdatedAt = time.Time{}, time.Time{}
	bx, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return string(bx) == string(by)
}

func init() {
	RegisterKind(&InputKind{Name: KindChangeObserved, From: observed, Untouched: true, Reduce: reduceChangeObserved})
}

// reduceChangeObserved keeps the thread's reference to a record, and copies
// the record's git facts onto the thread's change that follows it.
func reduceChangeObserved(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[ChangeObservedArgs](in)
	if err != nil {
		return Result{}, err
	}
	t, rec := r.T, &a.After
	ref := Ref{ID: rec.ID, Repo: RecordRepo(rec), Branch: rec.Branch, Ended: !rec.Phase.InFlight() || rec.IsDone()}
	if i := slices.IndexFunc(t.Refs, func(x Ref) bool { return x.ID == rec.ID }); i >= 0 {
		t.Refs[i] = ref
	} else {
		t.Refs = append(t.Refs, ref)
	}
	if c := follower(t, rec); c != nil {
		follow(t, c, rec, now)
	}
	return Result{}, nil
}

// repoTargets is the targets list of a repo, empty for none.
func repoTargets(repo string) []string {
	if repo == "" {
		return nil
	}
	return []string{"repo:" + repo}
}

// follower is the thread's change that follows rec: the one linked to it,
// else an in-flight change of any kind on its branch and repo that follows
// no record in flight (V0.87 (1)).
func follower(t *Thread, rec *Change) *Change {
	for i := range t.Changes {
		if t.Changes[i].Observed == rec.ID {
			return &t.Changes[i]
		}
	}
	if !rec.Phase.InFlight() {
		return nil
	}
	for i := range t.Changes {
		c := &t.Changes[i]
		if c.Branch != "" && c.Branch == rec.Branch && c.Phase.InFlight() && freeToLink(t, c) && sameRepo(c.Targets, repoTargets(RecordRepo(rec))) {
			return c
		}
	}
	return nil
}

// freeToLink reports a change that follows no record, or one that ended.
func freeToLink(t *Thread, c *Change) bool {
	if c.Observed == "" {
		return true
	}
	i := slices.IndexFunc(t.Refs, func(x Ref) bool { return x.ID == c.Observed })
	return i < 0 || t.Refs[i].Ended
}

// linkRef links a change of the thread with a branch, of any kind (V0.87
// (1)), to the in-flight record it references on the same repo and branch
// (change_open, a PR). The facts arrive with the record's next move.
func linkRef(t *Thread, c *Change) {
	if c == nil || c.Branch == "" || !c.Phase.InFlight() || !freeToLink(t, c) {
		return
	}
	for _, ref := range t.Refs {
		if !ref.Ended && ref.Branch == c.Branch && sameRepo(c.Targets, repoTargets(ref.Repo)) {
			c.Observed = ref.ID
			return
		}
	}
}

// follow copies a record's git facts onto the change that follows it: the
// workspace, pushed and clean, the record's revisions (all of them until
// the agent writes one of its own, then the newest: V0.87 (1)), and the end
// of its branch unless a PR says how the change ended.
func follow(t *Thread, c *Change, rec *Change, now time.Time) {
	was := cloneChange(c)
	c.Observed = rec.ID
	if rec.Workspace != nil {
		w := *rec.Workspace
		// V0.change-worktree: the worktree Fyr made stays Fyr's.
		w.MadeByFyr = w.MadeByFyr || c.Workspace != nil && c.Workspace.MadeByFyr
		c.Workspace = &w
	}
	for _, typ := range []CondType{CondPushed, CondClean} {
		if k := condition(rec, typ); k != nil {
			setCondition(c, typ, k.Status, k.Reason, ByObserved, now)
		} else if k := condition(c, typ); k != nil && k.Source == ByObserved {
			dropConditions(c, typ)
		}
	}
	if !slices.ContainsFunc(c.Revisions, func(r Revision) bool { return r.By != ByObserved }) {
		c.Revisions = slices.Clone(rec.Revisions)
	} else if n := len(rec.Revisions); n > 0 {
		addRevision(c, rec.Revisions[n-1], now)
	}
	if !rec.Phase.InFlight() && c.Phase.InFlight() && c.PR == nil {
		setPhase(t, c, rec.Phase, ByObserved, now)
	}
	if !sameChange(was, c) {
		c.UpdatedAt = now
	}
}

// ShownChanges are the changes a thread shows: its own, then each record
// it references that none of its own follows, with the threads that
// reference it. rec finds a record and those threads; a missing record is
// skipped.
func ShownChanges(t *Thread, rec func(id string) (*Change, []string)) []Change {
	out := slices.Clone(t.Changes)
	for _, ref := range t.Refs {
		if slices.ContainsFunc(t.Changes, func(c Change) bool { return c.Observed == ref.ID || c.ID == ref.ID }) {
			continue
		}
		c, threads := rec(ref.ID)
		if c == nil {
			continue
		}
		x := *cloneChange(c)
		x.Threads = threads
		out = append(out, x)
	}
	return out
}

// MigrateObserved moves the observed git changes a thread stored before #35
// into host records rs (by id; the newest copy wins) and gives the thread a
// reference to each. One that the agent or a PR added to (an alias, a PR,
// actions, evidence, a label, a Rule) stays in the thread and follows its
// record.
// It reports whether the thread changed.
func MigrateObserved(t *Thread, rs map[string]*Change) bool {
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

// RecordExpired reports a record that landed or ended more than 30 days
// ago: the host drops it, and the threads their references.
func RecordExpired(c *Change, now time.Time) bool {
	return (!c.Phase.InFlight() || c.IsDone()) && now.Sub(c.UpdatedAt) > endedKeep
}
