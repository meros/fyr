package core

import (
	"errors"
	"fmt"
	"hash/fnv"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Changes are records (SPEC 0.5 V0.59, decision 0018): one model for a git
// branch, a PR, a server change, a migration, a design or a document.

// Who set a phase or a condition.
const (
	ByAgent    = "agent"
	ByObserved = "observed"
	ByOwner    = "owner"
)

const (
	maxChanges   = 50
	maxRevisions = 20
	endedKeep    = 30 * 24 * time.Hour
)

// Change is one change a thread makes or asks for.
type Change struct {
	ID         string      `json:"id"`
	Key        string      `json:"key"`
	Kind       ChangeKind  `json:"kind"`
	Title      string      `json:"title"`
	Targets    []string    `json:"targets,omitempty"`
	Branch     string      `json:"branch,omitempty"`
	Revisions  []Revision  `json:"revisions,omitempty"`
	Phase      Phase       `json:"phase"`
	Label      string      `json:"label,omitempty"`
	Conditions []Condition `json:"conditions,omitempty"`
	Evidence   []Evidence  `json:"evidence,omitempty"`
	Group      string      `json:"group,omitempty"`
	After      []string    `json:"after,omitempty"`
	Needs      string      `json:"needs,omitempty"` // "owner" or ""
	// PR is the code change's pull request as GitHub last reported it (#32);
	// its "pr" evidence is the link the Changes board shows.
	PR *PR `json:"pr,omitempty"`
	// Dismissed: the owner put the merged or closed PR away (V0.36); it is
	// never the thread's main PR again.
	Dismissed bool `json:"dismissed,omitempty"`
	// Actions the owner can press (V0.60): declared by the agent, or built in
	// from what Fyr observed, and their last runs.
	Actions
	// V0.66: the failed checks' annotations while the PR is open. The deploy
	// watch of its merge commit is a Rule of the thread (#46).
	Annotations []Annotation `json:"annotations,omitempty"`
	PhaseBy     string       `json:"phaseBy"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
	// LandedAt is when the change first reached landed (V0.70): a failure
	// recorded before it is history and no longer blocks; one reported in
	// the same call as the landing, or later, blocks.
	LandedAt *time.Time `json:"landedAt,omitempty"`
	// Aliases are other keys that name this change: an agent's key for a
	// code change it joined by repo and branch (V0.70 (2)).
	Aliases []string `json:"aliases,omitempty"`
	// Workspace is where the change's git facts are read (decision 0019):
	// set on a host record by the git observer, and on a thread's code
	// change when it links one.
	Workspace *Workspace `json:"workspace,omitempty"`
	// Observed is the host record of the same repo and branch that this
	// thread's change follows (#35): its git facts are copied here.
	Observed string `json:"observed,omitempty"`
	// Threads are the threads that reference a host record; set only where
	// it is shown (the thread DTO, change_list), never stored.
	Threads []string `json:"threads,omitempty"`
	// Parked is the owner's Not now (V0.84): it holds while the change is
	// as the owner saw it.
	Parked *Park `json:"parked,omitempty"`
}

// Park is when the owner pressed Not now on a change, and a fingerprint of
// what they saw then (V0.84).
type Park struct {
	At   time.Time `json:"at"`
	Seen string    `json:"seen"`
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
	Type   CondType   `json:"type"`
	Status CondStatus `json:"status"`
	Reason string     `json:"reason,omitempty"`
	Source string     `json:"source"`
	At     time.Time  `json:"at"`
}

// Evidence links proof: a PR, a plan, a log, a preview, a deployment.
type Evidence struct {
	Kind       string `json:"kind"`
	URL        string `json:"url,omitempty"`
	Attachment string `json:"attachment,omitempty"`
	Title      string `json:"title,omitempty"`
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
		if t.Changes[i].Key == ref || slices.Contains(t.Changes[i].Aliases, ref) {
			return &t.Changes[i]
		}
	}
	return nil
}

// ChangeOpen is what the agent (or an observer) starts a change with.
type ChangeOpen struct {
	Key, Title, Branch, Group string
	Kind                      ChangeKind
	Targets, After            []string
	Revision                  *Revision
	Evidence                  []Evidence
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
	if o.Kind != "" && !o.Kind.Valid() {
		return nil, false, fmt.Errorf("kind must be one of %s", strings.Join(Names(ChangeKinds()), ", "))
	}
	c := FindChange(t, o.Key)
	if c == nil && o.Kind == KindCode && o.Branch != "" {
		// One change per repo and branch (V0.70 (2)): the agent's key joins
		// the change Fyr already keeps for that branch.
		if c = codeChangeOn(t, o.Branch, o.Targets); c != nil {
			c.Aliases = union(c.Aliases, []string{o.Key})
		}
	}
	if c != nil {
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
		linkRef(t, c)
		return joinPRWork(t, c, now), false, nil
	}
	if o.Kind == "" {
		o.Kind = KindOther
	}
	if strings.TrimSpace(o.Title) == "" {
		o.Title = o.Key
	}
	nc := Change{ID: ChangeID(o.Key), Key: o.Key, Kind: o.Kind, Title: strings.TrimSpace(o.Title), Targets: union(nil, o.Targets), Branch: o.Branch,
		Group: o.Group, After: union(nil, o.After), Phase: PhaseWorking, PhaseBy: by, CreatedAt: now, UpdatedAt: now}
	for _, ev := range o.Evidence {
		addEvidence(&nc, ev)
	}
	if o.Revision != nil {
		addRevision(&nc, *o.Revision, now)
	}
	t.Changes = append(t.Changes, nc)
	trimChanges(t, now)
	c = FindChange(t, nc.ID)
	linkRef(t, c)
	// V0.87 (2): the change Fyr made for this work's PR folds into it.
	return joinPRWork(t, c, now), true, nil
}

// ReviseChange adds a revision; the same id twice is one revision. A new
// revision sets the observed and reported conditions back to pending: they
// were about the old one.
func ReviseChange(c *Change, r Revision, now time.Time) bool {
	if !addRevision(c, r, now) {
		return false
	}
	for i := range c.Conditions {
		if c.Conditions[i].Source != ByOwner && c.Conditions[i].Status != CondPending {
			c.Conditions[i].Status, c.Conditions[i].Reason, c.Conditions[i].At = CondPending, "", now
		}
	}
	c.UpdatedAt = now
	return true
}

// ChangeUpdate is a status report: each set field changes.
type ChangeUpdate struct {
	Phase      Phase
	Label      *string
	Conditions []Condition
	Needs      *string
}

// UpdateChange applies a status report from by (V0.59 (2)). What by may
// write is the field rights table (rights.go).
func UpdateChange(t *Thread, c *Change, u ChangeUpdate, by string, now time.Time) error {
	if u.Phase != "" {
		if !u.Phase.Valid() {
			return fmt.Errorf("phase must be %s", OrList(Phases()))
		}
		if u.Phase != c.Phase {
			if err := CanWrite(FieldChangePhase, by, c.PhaseBy, fmt.Sprintf("the phase of %q (%s)", c.Title, c.Phase)); err != nil {
				return err
			}
		}
	}
	for _, k := range u.Conditions {
		if k.Type == "" {
			return errors.New("each condition needs a type")
		}
		if !k.Status.Valid() {
			return fmt.Errorf("condition %q: status must be %s", k.Type, OrList(CondStatuses()))
		}
		if f := condField(k.Type, k.Status); f != "" {
			if err := CanWrite(f, by, "", fmt.Sprintf("%q", k.Type)); err != nil {
				return err
			}
		}
		cur := ""
		if x := condition(c, k.Type); x != nil {
			cur = x.Source
		}
		if err := CanWrite(FieldChangeCondition, by, cur, fmt.Sprintf("%q of %q", k.Type, c.Title)); err != nil {
			return err
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

// maxEndReason is the longest reason the owner gives for an end.
const maxEndReason = 200

// endChange is the owner's change.end {change, phase?, reason?} (V0.72): it
// ends any change that has not ended, observed ones too (the owner decides),
// as abandoned (the default) or reverted. The reason becomes the label; the
// change's actions go, and nothing polls its PR again.
func endChange(t *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
	c := FindChange(t, strings.TrimSpace(a.Str("change")))
	if c == nil {
		return nil, NoChange(a.Str("change"))
	}
	if c.Phase.Ended() {
		return nil, reject(RejectUnavailable, "%q is already %s", c.Title, c.Phase)
	}
	phase := Phase(strings.TrimSpace(a.Str("phase")))
	if phase == "" {
		phase = PhaseAbandoned
	}
	if !phase.Ended() {
		return nil, fmt.Errorf("%w: phase must be abandoned or reverted", ErrBadArgs)
	}
	if err := CanWrite(FieldChangePhase, ByOwner, c.PhaseBy, fmt.Sprintf("the phase of %q", c.Title)); err != nil {
		return nil, err
	}
	reason := strings.Join(strings.Fields(a.Str("reason")), " ")
	if r := []rune(reason); len(r) > maxEndReason {
		reason = string(r[:maxEndReason-1]) + "…"
	}
	setPhase(t, c, phase, ByOwner, now)
	c.Label, c.Offers, c.Annotations = reason, nil, nil
	c.UpdatedAt = now
	v.SeenAt = now
	return nil, nil
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
// that PR (V0.59 (4), #32): found by the PR URL, else by its head branch, else
// made. Open is review, merged landed, closed abandoned, and missing (V0.72)
// abandoned with MissingPRLabel. A change the owner ended stays. It moves ActivityAt
// when the PR starts to block, merges or closes, so polling alone never marks
// a thread done.
func ChangePRObserved(t *Thread, p PR, now time.Time) *Change {
	if p.URL == "" {
		return nil
	}
	if p.Repo == "" || p.Number == 0 {
		p.Repo, p.Number = parsePRURL(p.URL)
	}
	c := onePRChange(t, p.URL, now) // V0.87 (2): two changes that hold the PR fold into one
	if p.State == PRMissing && c == nil {
		return nil // V0.72: no change is made for a PR that does not exist
	}
	if c != nil && CanWrite(FieldChangePhase, ByObserved, c.PhaseBy, "") != nil {
		return c // V0.72: the owner ended it; a fact does not move it again
	}
	if c == nil && p.Branch != "" {
		c = changeOnPRBranch(t, p.Branch)
	}
	if c == nil {
		title := p.Title
		if title == "" {
			title = fmt.Sprintf("%s#%d", p.Repo, p.Number)
		}
		c, _, _ = OpenChange(t, ChangeOpen{Key: PRKey(p.Repo, p.Number), Kind: KindCode, Title: title, Branch: p.Branch}, ByObserved, now)
		if c == nil {
			return nil
		}
	}
	oldState, oldBlocking := "", c.Blocking()
	if c.PR != nil {
		oldState = c.PR.State
	}
	defer func() {
		if b := c.Blocking(); (oldState != p.State || oldBlocking != b) && (b != "" || p.State != "OPEN") {
			t.ActivityAt = now
		}
	}()
	snap := p
	snap.Annotations = nil
	if p.State == PRMissing && c.PR != nil {
		// GitHub said nothing about it; what it said before stays.
		snap.Title, snap.Branch, snap.Head = c.PR.Title, c.PR.Branch, c.PR.Head
	}
	c.PR = &snap
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
	linkRef(t, c)
	phase := PhaseReview
	switch p.State {
	case "MERGED":
		phase = PhaseLanded
	case "CLOSED":
		phase = PhaseAbandoned
	case PRMissing:
		phase = PhaseAbandoned
		c.Label = MissingPRLabel
	}
	// A merged change that something reported live stays live.
	if !(phase == PhaseLanded && c.Phase == PhaseLive) {
		setPhase(t, c, phase, ByObserved, now)
	}
	armBuiltins(t, c, now)
	if oldState != "MERGED" {
		// V0.82: a merged PR is read again while its checks settle; a
		// watch a repo without deployments ended is not armed again.
		watchMerge(t, c, p, now)
	}
	if p.State == "OPEN" && p.Checks == ChecksFail {
		c.Annotations = p.Annotations
	} else {
		c.Annotations = nil
	}
	if p.State != "OPEN" {
		// Checks, review and conflict describe the PR before it merged or
		// closed; they no longer need anyone (decision 0018).
		dropConditions(c, CondChecks, CondReview, CondConflict)
		c.UpdatedAt = now
		return c
	}
	switch p.Checks {
	case ChecksPass:
		setCondition(c, CondChecks, CondOK, "", ByObserved, now)
	case ChecksFail:
		setCondition(c, CondChecks, CondFail, "CI failed", ByObserved, now)
	case ChecksPending:
		setCondition(c, CondChecks, CondPending, "", ByObserved, now)
	}
	switch p.Review {
	case "APPROVED":
		setCondition(c, CondReview, CondOK, "", ByObserved, now)
	case "CHANGES_REQUESTED":
		setCondition(c, CondReview, CondFail, "Changes requested", ByObserved, now)
	case "REVIEW_REQUIRED":
		setCondition(c, CondReview, CondPending, "", ByObserved, now)
	}
	if p.Conflict {
		setCondition(c, CondConflict, CondFail, "Merge conflict", ByObserved, now)
	} else if condition(c, CondConflict) != nil {
		setCondition(c, CondConflict, CondOK, "", ByObserved, now)
	}
	c.UpdatedAt = now
	return c
}

// GitFacts are what the host reads from one checkout: the thread's folder
// or a change's workspace.
type GitFacts struct {
	Repo   string // the origin remote, normalised (github.com/owner/name), or the root folder
	Path   string // the checkout's root folder
	Branch string
	Base   string
	// BaseHead is where the base points (origin/main); V0.76 (1): a branch behind it is not merged into it.
	BaseHead string
	Ahead    int // commits since the merge base with the base
	Unpushed int // commits not on the upstream; all when there is none
	Dirty    int // changed and untracked files
	Head     string
}

// ChangeBlocking is why a change of the thread needs the owner (V0.59 (6)),
// or "". It is the one source of "blocked", PRs included (#32).
func ChangeBlocking(t *Thread) string {
	for i := len(t.Changes) - 1; i >= 0; i-- {
		if r := t.Changes[i].Blocking(); r != "" {
			return t.Changes[i].Title + ": " + r
		}
	}
	return ""
}

// Blocking is why this change needs the owner, or "": a failed condition
// (a PR's checks, review or conflict among them) or needs owner. A change
// the owner parked does not block while it is as they saw it (V0.84).
func (c *Change) Blocking() string {
	if c.ParkHolds() {
		return ""
	}
	return c.blocks()
}

// blocks is Blocking without the owner's Not now.
func (c *Change) blocks() string {
	if c.Phase.Ended() {
		return ""
	}
	for _, k := range c.Conditions {
		if k.Status == CondFail && (c.LandedAt == nil || !k.At.Before(*c.LandedAt)) {
			if k.Reason != "" {
				return k.Reason
			}
			return string(k.Type) + " failed"
		}
	}
	if c.Needs == "owner" {
		return "needs you"
	}
	return ""
}

// --- helpers ---

// setPhase moves a change; t is its thread, nil for a host record.
func setPhase(t *Thread, c *Change, phase Phase, by string, now time.Time) {
	if c.Phase == phase && c.PhaseBy == by {
		return
	}
	moved := c.Phase != phase
	c.Phase, c.PhaseBy, c.UpdatedAt = phase, by, now
	switch {
	case phase.Reached(PhaseLanded) && c.LandedAt == nil:
		c.LandedAt = &now
	case !phase.Reached(PhaseLanded) && !phase.Ended():
		c.LandedAt = nil
	}
	if moved && phase != PhaseWorking && t != nil {
		t.ActivityAt = now
	}
}

func condition(c *Change, typ CondType) *Condition {
	for i := range c.Conditions {
		if c.Conditions[i].Type == typ {
			return &c.Conditions[i]
		}
	}
	return nil
}

func setCondition(c *Change, typ CondType, status CondStatus, reason, by string, now time.Time) {
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
	c.Revisions = keepLast(c.Revisions, maxRevisions)
	return true
}

// addEvidence adds proof once. The first "pr" evidence of a change without a
// PR gives it one, open until GitHub reports otherwise (#32).
func addEvidence(c *Change, ev Evidence) {
	for _, x := range c.Evidence {
		if x.Kind == ev.Kind && x.URL == ev.URL && x.Attachment == ev.Attachment {
			return
		}
	}
	c.Evidence = append(c.Evidence, ev)
	if ev.Kind == "pr" && c.PR == nil && ev.URL != "" {
		repo, n := parsePRURL(ev.URL)
		c.PR = &PR{URL: ev.URL, Repo: repo, Number: n, State: "OPEN", Checks: ChecksPending}
	}
}

// changeOnPRBranch is the change a PR found by its head branch attaches to
// (V0.59 (4), V0.87 (2)): a change of any kind on that branch that has not
// ended and has no PR, the one that follows a host record first.
func changeOnPRBranch(t *Thread, branch string) *Change {
	var first *Change
	for i := range t.Changes {
		x := &t.Changes[i]
		if x.Branch != branch || x.Phase.Ended() || x.PRURL() != "" {
			continue
		}
		if x.Observed != "" {
			return x
		}
		if first == nil {
			first = x
		}
	}
	return first
}

// changeByPR is the change whose PR has this URL, or nil.
func changeByPR(t *Thread, url string) *Change {
	if url == "" {
		return nil
	}
	for i := range t.Changes {
		if t.Changes[i].PRURL() == url {
			return &t.Changes[i]
		}
	}
	return nil
}

// PRURL is the URL of the change's PR, or "".
func (c *Change) PRURL() string {
	if c.PR == nil {
		return ""
	}
	return c.PR.URL
}

var prURLRe = regexp.MustCompile(`github\.com/([^/\s]+/[^/\s]+)/pull/(\d+)`)

// parsePRURL reads owner/repo and the number from a GitHub PR URL.
func parsePRURL(url string) (string, int) {
	m := prURLRe.FindStringSubmatch(url)
	if m == nil {
		return "", 0
	}
	n, _ := strconv.Atoi(m[2])
	return m[1], n
}

// settleAfterMerge is how long after the merge a PR whose snapshot is not
// final is still read (V0.82).
const settleAfterMerge = 24 * time.Hour

// PRURLs are the PR URLs to poll: the thread's changes in flight, and
// (V0.82) a landed or live change whose PR snapshot is not final, within
// settleAfterMerge of the merge.
func PRURLs(t *Thread, now time.Time) []string {
	var out []string
	for i := range t.Changes {
		c := &t.Changes[i]
		if u := c.PRURL(); u != "" && (c.Phase.InFlight() || settling(c, now)) {
			out = append(out, u)
		}
	}
	return out
}

// settling reports a landed or live change whose PR GitHub has not yet
// shown merged or closed, or whose checks are pending or not read yet,
// within settleAfterMerge of the merge (mergedAt, else when it landed).
func settling(c *Change, now time.Time) bool {
	if c.PR == nil || !c.Phase.Reached(PhaseLanded) || c.Phase.Ended() {
		return false
	}
	at := c.PR.MergedAt
	if at == nil {
		at = c.LandedAt
	}
	if at == nil || now.Sub(*at) >= settleAfterMerge {
		return false
	}
	final := c.PR.State == "MERGED" || c.PR.State == "CLOSED"
	return !final || c.PR.Checks == ChecksPending || c.PR.Checks == ChecksNone
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
		if c.Phase.Ended() && now.Sub(c.UpdatedAt) > endedKeep {
			continue
		}
		keep = append(keep, c)
	}
	t.Changes = keep
	for len(t.Changes) > maxChanges {
		idx := -1
		for i, c := range t.Changes {
			if c.Phase.Ended() && (idx < 0 || c.UpdatedAt.Before(t.Changes[idx].UpdatedAt)) {
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

func dropConditions(c *Change, types ...CondType) {
	out := c.Conditions[:0]
	for _, k := range c.Conditions {
		if !slices.Contains(types, k.Type) {
			out = append(out, k)
		}
	}
	c.Conditions = out
}

// codeChangeOn is the thread's code change on branch in the same repo, not
// landed (in flight), or nil. Without a repo on either side the branch alone
// decides.
func codeChangeOn(t *Thread, branch string, targets []string) *Change {
	for i := range t.Changes {
		c := &t.Changes[i]
		if c.Kind == KindCode && c.Branch == branch && c.Phase.InFlight() && sameRepo(c.Targets, targets) {
			return c
		}
	}
	return nil
}

// sameRepo reports targets that share a repo, or a side that names none.
func sameRepo(a, b []string) bool {
	repos := func(ts []string) []string {
		var out []string
		for _, x := range ts {
			if strings.HasPrefix(x, "repo:") {
				out = append(out, x)
			}
		}
		return out
	}
	x, y := repos(a), repos(b)
	return len(x) == 0 || len(y) == 0 || slices.ContainsFunc(x, func(r string) bool { return slices.Contains(y, r) })
}
