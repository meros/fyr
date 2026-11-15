package core

import (
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"sort"
	"strings"
	"time"
)

// Changes are records (SPEC 0.5 V0.59, decision 0018): one model for a git
// branch, a PR, a server change, a migration, a design or a document.

// Phases of a change. Working, review, landed and live are in order; the two
// ends stop it.
const (
	PhaseWorking   = "working"
	PhaseReview    = "review"
	PhaseLanded    = "landed"
	PhaseLive      = "live"
	PhaseAbandoned = "abandoned"
	PhaseReverted  = "reverted"
)

// Who set a phase or a condition.
const (
	ByAgent    = "agent"
	ByObserved = "observed"
	ByOwner    = "owner"
)

// ChangeKinds are the kinds a change may have.
var ChangeKinds = []string{"code", "infra", "host", "data", "doc", "design", "other"}

var phaseRank = map[string]int{PhaseWorking: 0, PhaseReview: 1, PhaseLanded: 2, PhaseLive: 3}

const (
	maxChanges   = 50
	maxRevisions = 20
	endedKeep    = 30 * 24 * time.Hour
)

// Change is one change a thread makes or asks for.
type Change struct {
	ID         string      `json:"id"`
	Key        string      `json:"key"`
	Kind       string      `json:"kind"`
	Title      string      `json:"title"`
	Targets    []string    `json:"targets,omitempty"`
	Branch     string      `json:"branch,omitempty"`
	Revisions  []Revision  `json:"revisions,omitempty"`
	Phase      string      `json:"phase"`
	Label      string      `json:"label,omitempty"`
	Conditions []Condition `json:"conditions,omitempty"`
	Evidence   []Evidence  `json:"evidence,omitempty"`
	Group      string      `json:"group,omitempty"`
	After      []string    `json:"after,omitempty"`
	Needs      string      `json:"needs,omitempty"` // "owner" or ""
	// Actions the owner can press (V0.60): declared by the agent, or built in
	// from what Fyr observed. Runs are their last results.
	Actions []Offer    `json:"actions,omitempty"`
	Runs    []OfferRun `json:"runs,omitempty"`
	// V0.66: the failed checks' annotations while the PR is open, and the
	// merge commit Fyr watches for deployments.
	Annotations []Annotation `json:"annotations,omitempty"`
	DeployWatch *DeployWatch `json:"deployWatch,omitempty"`
	PhaseBy    string      `json:"phaseBy"`
	CreatedAt  time.Time   `json:"createdAt"`
	UpdatedAt  time.Time   `json:"updatedAt"`
}

// Revision is one version of a change: a commit, a plan digest, a doc version.
type Revision struct {
	ID      string    `json:"id"`
	Summary string    `json:"summary,omitempty"`
	URL     string    `json:"url,omitempty"`
	By      string    `json:"by"`
	At      time.Time `json:"at"`
}

// Condition is one independent fact about a change.
type Condition struct {
	Type   string    `json:"type"`
	Status string    `json:"status"` // ok, fail, pending
	Reason string    `json:"reason,omitempty"`
	Source string    `json:"source"`
	At     time.Time `json:"at"`
}

// Evidence links proof: a PR, a plan, a log, a preview, a deployment.
type Evidence struct {
	Kind       string `json:"kind"`
	URL        string `json:"url,omitempty"`
	Attachment string `json:"attachment,omitempty"`
	Title      string `json:"title,omitempty"`
}

// SettleWhen settles a thread when a change reaches a phase (V0.59 (7)).
type SettleWhen struct {
	Change string `json:"change"`
	Phase  string `json:"phase"`
}

// Ended reports a phase that stops a change.
func Ended(phase string) bool { return phase == PhaseAbandoned || phase == PhaseReverted }

// ValidPhase reports a known phase.
func ValidPhase(p string) bool { _, ok := phaseRank[p]; return ok || Ended(p) }

// Reached reports that phase has got at least as far as want.
func Reached(phase, want string) bool {
	if phase == want {
		return true
	}
	a, ok1 := phaseRank[phase]
	b, ok2 := phaseRank[want]
	return ok1 && ok2 && a >= b
}

// FindChange is the change with this id or key, or nil.
func FindChange(t *Thread, ref string) *Change {
	if ref == "" {
		return nil
	}
	for i := range t.Changes {
		if t.Changes[i].ID == ref {
			return &t.Changes[i]
		}
	}
	for i := range t.Changes {
		if t.Changes[i].Key == ref {
			return &t.Changes[i]
		}
	}
	return nil
}

// ChangeOpen is what the agent (or an observer) starts a change with.
type ChangeOpen struct {
	Key, Kind, Title, Branch, Group string
	Targets, After                  []string
	Revision                        *Revision
	Evidence                        []Evidence
}

// ChangeID is a change's id: stable for its key, so a retried call or a
// second observer finds the same change.
func ChangeID(key string) string {
	h := fnv.New64a()
	h.Write([]byte(key))
	return fmt.Sprintf("c_%012x", h.Sum64()&0xffffffffffff)
}

// OpenChange returns the change with o.Key, or makes it. A new change
// starts in working. Fields given on a known key fill only what is empty, so
// a second call never undoes the first.
func OpenChange(t *Thread, o ChangeOpen, by string, now time.Time) (*Change, bool, error) {
	o.Key = strings.TrimSpace(o.Key)
	if o.Key == "" {
		return nil, false, errors.New("key is required: a stable name for this change, for example \"k8s:prod/api replicas\"")
	}
	if o.Kind != "" && !validKind(o.Kind) {
		return nil, false, fmt.Errorf("kind must be one of %s", strings.Join(ChangeKinds, ", "))
	}
	if c := FindChange(t, o.Key); c != nil {
		if c.Title == "" {
			c.Title = o.Title
		}
		if c.Branch == "" {
			c.Branch = o.Branch
		}
		if c.Group == "" {
			c.Group = o.Group
		}
		c.Targets = union(c.Targets, o.Targets)
		c.After = union(c.After, o.After)
		for _, ev := range o.Evidence {
			addEvidence(c, ev)
		}
		if o.Revision != nil {
			addRevision(c, *o.Revision, now)
		}
		c.UpdatedAt = now
		return c, false, nil
	}
	if o.Kind == "" {
		o.Kind = "other"
	}
	if strings.TrimSpace(o.Title) == "" {
		o.Title = o.Key
	}
	c := Change{ID: ChangeID(o.Key), Key: o.Key, Kind: o.Kind, Title: strings.TrimSpace(o.Title), Targets: union(nil, o.Targets), Branch: o.Branch,
		Group: o.Group, After: union(nil, o.After), Phase: PhaseWorking, PhaseBy: by, CreatedAt: now, UpdatedAt: now}
	for _, ev := range o.Evidence {
		addEvidence(&c, ev)
	}
	if o.Revision != nil {
		addRevision(&c, *o.Revision, now)
	}
	t.Changes = append(t.Changes, c)
	trimChanges(t, now)
	return FindChange(t, c.ID), true, nil
}

// ReviseChange adds a revision; the same id twice is one revision. A new
// revision sets the observed and reported conditions back to pending: they
// were about the old one.
func ReviseChange(c *Change, r Revision, now time.Time) bool {
	if !addRevision(c, r, now) {
		return false
	}
	for i := range c.Conditions {
		if c.Conditions[i].Source != ByOwner && c.Conditions[i].Status != "pending" {
			c.Conditions[i].Status, c.Conditions[i].Reason, c.Conditions[i].At = "pending", "", now
		}
	}
	c.UpdatedAt = now
	return true
}

// ChangeUpdate is a status report: each set field changes.
type ChangeUpdate struct {
	Phase      string
	Label      *string
	Conditions []Condition
	Needs      *string
}

// UpdateChange applies a status report from by (V0.59 (2)): an observed
// phase or condition is not changed by the agent, and the agent never marks
// a review or an approval ok.
func UpdateChange(t *Thread, c *Change, u ChangeUpdate, by string, now time.Time) error {
	if u.Phase != "" {
		if !ValidPhase(u.Phase) {
			return fmt.Errorf("phase must be working, review, landed, live, abandoned or reverted")
		}
		if by == ByAgent && c.PhaseBy == ByObserved && u.Phase != c.Phase {
			return fmt.Errorf("Fyr observes the phase of %q (%s); a report cannot change it", c.Title, c.Phase)
		}
	}
	for _, k := range u.Conditions {
		if k.Type == "" {
			return errors.New("each condition needs a type")
		}
		if k.Status != "ok" && k.Status != "fail" && k.Status != "pending" {
			return fmt.Errorf("condition %q: status must be ok, fail or pending", k.Type)
		}
		if by == ByAgent && k.Status == "ok" && (k.Type == "review" || k.Type == "approval") {
			return fmt.Errorf("an agent cannot mark %q ok; ask the owner with change_request", k.Type)
		}
		if cur := condition(c, k.Type); by == ByAgent && cur != nil && cur.Source == ByObserved {
			return fmt.Errorf("Fyr observes %q of %q; a report cannot change it", k.Type, c.Title)
		}
	}
	if u.Needs != nil && *u.Needs != "" && *u.Needs != "owner" {
		return errors.New(`needs must be "owner" or empty`)
	}
	if u.Phase != "" {
		setPhase(t, c, u.Phase, by, now)
	}
	if u.Label != nil {
		c.Label = *u.Label
	}
	for _, k := range u.Conditions {
		setCondition(c, k.Type, k.Status, k.Reason, by, now)
	}
	if u.Needs != nil {
		c.Needs = *u.Needs
		if c.Needs != "" {
			t.ActivityAt = now
		}
	}
	c.UpdatedAt = now
	return nil
}

// AttachEvidence adds proof; the same kind and URL (or attachment) twice is one.
func AttachEvidence(c *Change, evs []Evidence, now time.Time) error {
	for _, ev := range evs {
		if ev.Kind == "" || (ev.URL == "" && ev.Attachment == "") {
			return errors.New("evidence needs a kind and a url or an attachment")
		}
	}
	for _, ev := range evs {
		addEvidence(c, ev)
	}
	c.UpdatedAt = now
	return nil
}

// ChangePRObserved writes a PR snapshot into the thread's code change for
// that PR (V0.59 (4)): found by the PR URL, else by its head branch, else
// made. Open is review, merged landed, closed abandoned.
func ChangePRObserved(t *Thread, p PR, now time.Time) {
	if p.URL == "" {
		return
	}
	c := changeByEvidence(t, "pr", p.URL)
	if c == nil && p.Branch != "" {
		for i := range t.Changes {
			x := &t.Changes[i]
			if x.Kind == "code" && x.Branch == p.Branch && !Ended(x.Phase) && prOf(x) == "" {
				c = x
				break
			}
		}
	}
	if c == nil {
		title := p.Title
		if title == "" {
			title = fmt.Sprintf("%s#%d", p.Repo, p.Number)
		}
		c, _, _ = OpenChange(t, ChangeOpen{Key: fmt.Sprintf("pr:%s#%d", p.Repo, p.Number), Kind: "code", Title: title, Branch: p.Branch}, ByObserved, now)
		if c == nil {
			return
		}
	}
	if p.Title != "" && (c.Title == "" || c.Title == c.Key || c.Title == c.Branch) {
		c.Title = p.Title
	}
	if c.Branch == "" {
		c.Branch = p.Branch
	}
	addEvidence(c, Evidence{Kind: "pr", URL: p.URL, Title: fmt.Sprintf("#%d", p.Number)})
	if p.Repo != "" {
		repo := p.Repo
		if strings.Count(repo, "/") == 1 {
			repo = "github.com/" + repo // the git observer's form
		}
		c.Targets = union(c.Targets, []string{"repo:" + repo})
	}
	phase := PhaseReview
	switch p.State {
	case "MERGED":
		phase = PhaseLanded
	case "CLOSED":
		phase = PhaseAbandoned
	}
	// A merged change that something reported live stays live.
	if !(phase == PhaseLanded && c.Phase == PhaseLive) {
		setPhase(t, c, phase, ByObserved, now)
	}
	defer syncBuiltins(c)
	watchMerge(c, p, now)
	if p.State == "OPEN" && p.Checks == "fail" {
		c.Annotations = p.Annotations
	} else {
		c.Annotations = nil
	}
	if p.State != "OPEN" {
		// Checks, review and conflict describe the PR before it merged or
		// closed; they no longer need anyone (decision 0018).
		dropConditions(c, "checks", "review", "conflict")
		c.UpdatedAt = now
		return
	}
	switch p.Checks {
	case "pass":
		setCondition(c, "checks", "ok", "", ByObserved, now)
	case "fail":
		setCondition(c, "checks", "fail", "CI failed", ByObserved, now)
	case "pending":
		setCondition(c, "checks", "pending", "", ByObserved, now)
	}
	switch p.Review {
	case "APPROVED":
		setCondition(c, "review", "ok", "", ByObserved, now)
	case "CHANGES_REQUESTED":
		setCondition(c, "review", "fail", "Changes requested", ByObserved, now)
	case "REVIEW_REQUIRED":
		setCondition(c, "review", "pending", "", ByObserved, now)
	}
	if p.Conflict {
		setCondition(c, "conflict", "fail", "Merge conflict", ByObserved, now)
	} else if condition(c, "conflict") != nil {
		setCondition(c, "conflict", "ok", "", ByObserved, now)
	}
	c.UpdatedAt = now
}

// GitFacts are what the host reads from the thread's folder after a run.
type GitFacts struct {
	Repo     string // the origin remote, normalised (github.com/owner/name), or the root folder
	Branch   string
	Base     string
	Ahead    int // commits since the merge base with the base
	Unpushed int // commits not on the upstream; all when there is none
	Dirty    int // changed and untracked files
	Head     string
}

// GitObserved keeps the code change of the thread's branch (V0.59 (3)). A
// branch with commits, unpushed work or changed files has one; it is made in
// working and the observed conditions pushed and clean follow the facts.
func GitObserved(t *Thread, g GitFacts, now time.Time) {
	if g.Branch == "" {
		return
	}
	var c *Change
	for i := range t.Changes {
		x := &t.Changes[i]
		if x.Kind == "code" && x.Branch == g.Branch && InFlight(x.Phase) {
			c = x
			break
		}
	}
	if c == nil {
		if g.Ahead == 0 && g.Unpushed == 0 && g.Dirty == 0 {
			return
		}
		var targets []string
		if g.Repo != "" {
			targets = []string{"repo:" + g.Repo}
		}
		c, _, _ = OpenChange(t, ChangeOpen{Key: freshKey(t, "git:"+g.Repo+"#"+g.Branch), Kind: "code", Title: g.Branch, Branch: g.Branch, Targets: targets}, ByObserved, now)
		if c == nil {
			return
		}
		c.PhaseBy = ByObserved
	}
	if g.Unpushed > 0 {
		setCondition(c, "pushed", "pending", plural(g.Unpushed, "commit")+" not pushed", ByObserved, now)
	} else {
		setCondition(c, "pushed", "ok", "", ByObserved, now)
	}
	if g.Dirty > 0 {
		setCondition(c, "clean", "pending", plural(g.Dirty, "file")+" changed, not committed", ByObserved, now)
	} else {
		setCondition(c, "clean", "ok", "", ByObserved, now)
	}
	if g.Head != "" && g.Ahead > 0 {
		addRevision(c, Revision{ID: g.Head, Summary: fmt.Sprintf("%s ahead of %s", plural(g.Ahead, "commit"), g.Base), By: ByObserved}, now)
	}
	c.UpdatedAt = now
}

// ChangeBlocking is why a change needs the owner (V0.59 (6)), or "".
func ChangeBlocking(t *Thread) string {
	for i := len(t.Changes) - 1; i >= 0; i-- {
		c := &t.Changes[i]
		if Ended(c.Phase) {
			continue
		}
		for _, k := range c.Conditions {
			if k.Status == "fail" {
				r := k.Reason
				if r == "" {
					r = k.Type + " failed"
				}
				return c.Title + ": " + r
			}
		}
		if c.Needs == "owner" {
			return c.Title + ": needs you"
		}
	}
	return ""
}

// CheckSettleChange settles the thread when its settle_when change has
// reached the phase (V0.59 (7)).
func CheckSettleChange(t *Thread, now time.Time) {
	if t.SettleWhen == nil || t.SettledAt != nil {
		return
	}
	c := FindChange(t, t.SettleWhen.Change)
	if c != nil && Reached(c.Phase, t.SettleWhen.Phase) {
		t.SettledAt = &now
		t.SettleWhen = nil
	}
}

// --- helpers ---

func validKind(k string) bool {
	for _, x := range ChangeKinds {
		if x == k {
			return true
		}
	}
	return false
}

func setPhase(t *Thread, c *Change, phase, by string, now time.Time) {
	if c.Phase == phase && c.PhaseBy == by {
		return
	}
	moved := c.Phase != phase
	c.Phase, c.PhaseBy, c.UpdatedAt = phase, by, now
	if moved && phase != PhaseWorking {
		t.ActivityAt = now
	}
	CheckSettleChange(t, now)
}

func condition(c *Change, typ string) *Condition {
	for i := range c.Conditions {
		if c.Conditions[i].Type == typ {
			return &c.Conditions[i]
		}
	}
	return nil
}

func setCondition(c *Change, typ, status, reason, by string, now time.Time) {
	if k := condition(c, typ); k != nil {
		if k.Status != status || k.Reason != reason || k.Source != by {
			k.Status, k.Reason, k.Source, k.At = status, reason, by, now
		}
		return
	}
	c.Conditions = append(c.Conditions, Condition{Type: typ, Status: status, Reason: reason, Source: by, At: now})
}

func addRevision(c *Change, r Revision, now time.Time) bool {
	if r.ID == "" {
		return false
	}
	for _, x := range c.Revisions {
		if x.ID == r.ID {
			return false
		}
	}
	if r.By == "" {
		r.By = ByAgent
	}
	r.At = now
	c.Revisions = append(c.Revisions, r)
	if len(c.Revisions) > maxRevisions {
		c.Revisions = c.Revisions[len(c.Revisions)-maxRevisions:]
	}
	return true
}

func addEvidence(c *Change, ev Evidence) {
	for _, x := range c.Evidence {
		if x.Kind == ev.Kind && x.URL == ev.URL && x.Attachment == ev.Attachment {
			return
		}
	}
	c.Evidence = append(c.Evidence, ev)
}

func changeByEvidence(t *Thread, kind, url string) *Change {
	if url == "" {
		return nil
	}
	for i := range t.Changes {
		for _, ev := range t.Changes[i].Evidence {
			if ev.Kind == kind && ev.URL == url {
				return &t.Changes[i]
			}
		}
	}
	return nil
}

// prOf is the PR URL of a change, or "".
func prOf(c *Change) string {
	for _, ev := range c.Evidence {
		if ev.Kind == "pr" {
			return ev.URL
		}
	}
	return ""
}

// PRURLs are the PR URLs of the thread's changes that are not ended or landed.
func PRURLs(t *Thread) []string {
	var out []string
	for i := range t.Changes {
		c := &t.Changes[i]
		if u := prOf(c); u != "" && InFlight(c.Phase) {
			out = append(out, u)
		}
	}
	return out
}

func union(a, b []string) []string {
	for _, x := range b {
		x = strings.TrimSpace(x)
		if x == "" {
			continue
		}
		dup := false
		for _, y := range a {
			if y == x {
				dup = true
				break
			}
		}
		if !dup {
			a = append(a, x)
		}
	}
	return a
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// trimChanges keeps at most maxChanges: ended changes older than 30 days go
// first, then the oldest ended ones, then the oldest.
func trimChanges(t *Thread, now time.Time) {
	keep := t.Changes[:0]
	for _, c := range t.Changes {
		if Ended(c.Phase) && now.Sub(c.UpdatedAt) > endedKeep {
			continue
		}
		keep = append(keep, c)
	}
	t.Changes = keep
	for len(t.Changes) > maxChanges {
		idx := -1
		for i, c := range t.Changes {
			if Ended(c.Phase) && (idx < 0 || c.UpdatedAt.Before(t.Changes[idx].UpdatedAt)) {
				idx = i
			}
		}
		if idx < 0 {
			sort.SliceStable(t.Changes, func(i, j int) bool { return t.Changes[i].UpdatedAt.Before(t.Changes[j].UpdatedAt) })
			idx = 0
		}
		t.Changes = append(t.Changes[:idx], t.Changes[idx+1:]...)
	}
}


// freshKey is key, or key~N when a change with that key has landed or
// ended: new work after that is a new change (V0.59 (3)).
func freshKey(t *Thread, key string) string {
	k := key
	for n := 2; ; n++ {
		c := FindChange(t, k)
		if c == nil || InFlight(c.Phase) {
			return k
		}
		k = fmt.Sprintf("%s~%d", key, n)
	}
}

// InFlight reports a phase before landed: the change can still fail,
// block and take new work.
func InFlight(phase string) bool { return phase == PhaseWorking || phase == PhaseReview }

func dropConditions(c *Change, types ...string) {
	out := c.Conditions[:0]
	for _, k := range c.Conditions {
		if !slices.Contains(types, k.Type) {
			out = append(out, k)
		}
	}
	c.Conditions = out
}
