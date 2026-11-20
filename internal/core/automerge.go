package core

import (
	"fmt"
	"maps"
	"regexp"
	"strings"
	"time"
)

// Auto-merge (V0.102): in a repo the owner opted in, Fyr approves an agent's
// merge ask itself once the PR is ready on the head the ask named. The
// setting is the owner's alone (KindAutoMergeSet, a host command with a
// receipt); the approval is an observer's Input (KindAutoMerge) that the
// engine sends for a repo in the setting. No agent tool sends either.
const (
	KindAutoMergeSet = "host.autoMerge"
	KindAutoMerge    = "merge.autoApproved"
	// EffectAutoMergeSet stores the owner's setting on the host.
	EffectAutoMergeSet EffectKind = "autoMerge.set"
)

// AutoMergeArgs names the merge ask Fyr approves and the repo whose setting
// allows it.
type AutoMergeArgs struct {
	Decision string `json:"decision"`
	Repo     string `json:"repo"`
}

// AutoApproval is on a change whose merge Fyr approved by the repo setting:
// the board and the thread say so (V0.102).
// A deploy Fyr approved by the auto-deploy setting has Deploy and no Repo;
// Head is the commit it builds (V0.111).
type AutoApproval struct {
	Repo   string    `json:"repo,omitempty"`
	Deploy bool      `json:"deploy,omitempty"`
	Head   string    `json:"head"`
	At     time.Time `json:"at"`
}

var repoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// RepoKey is the form a repo is stored and compared in: owner/name in lower
// case; "" when s is not a repo name.
func RepoKey(s string) string {
	s = strings.TrimSpace(s)
	if !repoName.MatchString(s) {
		return ""
	}
	return strings.ToLower(s)
}

// autoMergeable is the repo of the PR that the open merge ask d acts on when
// Fyr may merge it now with no press, or why not: the PR is ready ("Merge",
// V0.97 (3), which needs green checks and an idle thread) on the head the
// ask named, and the merge guard passes.
func autoMergeable(t *Thread, d Decision) (repo, why string) {
	return mergeableByFyr(t, d, t.Running || t.RunningTasks() > 0)
}

// mergeableByFyr is autoMergeable with the thread's run given: false asks
// whether Fyr merges it once the run is over.
func mergeableByFyr(t *Thread, d Decision, running bool) (repo, why string) {
	if d.Kind != DecisionEffect || d.Effect == nil || d.Effect.Action != FyrMerge {
		return "", "not a merge ask"
	}
	a := d.Effect.Args
	url, _ := a["pr"].(string)
	head, _ := a["head"].(string)
	c := changeByPR(t, url)
	switch {
	case c == nil || c.PR == nil || c.PR.Head == "":
		return "", "Fyr has not read the PR"
	case !SameHead(c.PR.Head, head):
		return "", HeadMoved
	case Ready(c.PR, running) != ReadyMerge:
		return "", "The PR is not ready to merge"
	}
	if why := FyrEffects[FyrMerge].Blocked(t, a); why != "" {
		return "", why
	}
	return c.PR.Repo, ""
}

// selfMerges reports a merge ask Fyr will merge by itself once the thread's
// run and tasks are over, in a repo of the setting (V0.205).
func selfMerges(t *Thread, d Decision, on func(repo string) bool) bool {
	if on == nil {
		return false
	}
	repo, why := mergeableByFyr(t, d, false)
	return why == "" && on(repo)
}

// ownerAsks are the open decisions that wait on the owner: all but the merges
// Fyr will do by itself (V0.205).
func ownerAsks(t *Thread, on func(repo string) bool) []Decision {
	var out []Decision
	for _, d := range t.Decisions {
		if !selfMerges(t, d, on) {
			out = append(out, d)
		}
	}
	return out
}

// AutoMergeAsks are the thread's open merge asks that Fyr may merge now, with
// their repo; the engine sends KindAutoMerge for the repos in the setting.
func AutoMergeAsks(t *Thread) []AutoMergeArgs {
	var out []AutoMergeArgs
	for _, d := range t.Decisions {
		if repo, why := autoMergeable(t, d); why == "" {
			out = append(out, AutoMergeArgs{Decision: d.ID, Repo: repo})
		}
	}
	return out
}

func init() {
	RegisterKind(&InputKind{Name: KindAutoMergeSet, On: RecHost, From: ownerOnly, Await: true, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
		a := mapArgs(in)
		repo := RepoKey(a.Str("repo"))
		if repo == "" {
			return Result{}, reject(RejectBadArgs, "repo must be owner/name, got %q", a.Str("repo"))
		}
		on, ok := a["on"].(bool)
		if !ok {
			return Result{}, reject(RejectBadArgs, "on must be true or false")
		}
		return Result{Effects: []Effect{{Kind: EffectAutoMergeSet, Request: &EffectReq{Action: KindAutoMergeSet, Args: map[string]any{"repo": repo, "on": on}}}}}, nil
	}})
	RegisterKind(&InputKind{Name: KindAutoMerge, From: observed, Reduce: reduceAutoMerge})
}

// reduceAutoMerge closes the merge ask as approved by the repo setting and
// runs the merge bound to the ask's head, as the owner's yes would.
func reduceAutoMerge(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[AutoMergeArgs](in)
	if err != nil {
		return Result{}, err
	}
	t := r.T
	idx := -1
	for i, d := range t.Decisions {
		if d.ID == a.Decision {
			idx = i
		}
	}
	if idx < 0 {
		return Result{}, reject(RejectNotFound, "no open decision %q", a.Decision)
	}
	d := t.Decisions[idx]
	repo, why := autoMergeable(t, d)
	if why != "" {
		return Result{}, reject(RejectUnavailable, "%s", why)
	}
	if RepoKey(repo) != RepoKey(a.Repo) {
		return Result{}, reject(RejectBadArgs, "the PR is in %s, not %s", repo, a.Repo)
	}
	t.Decisions = append(t.Decisions[:idx:idx], t.Decisions[idx+1:]...)
	if len(t.Decisions) == 0 {
		t.Decisions = nil
	}
	req := *d.Effect
	req.Args = maps.Clone(req.Args)
	req.Args["auto"] = repo
	head, _ := req.Args["head"].(string)
	if id, _ := req.Args["change"].(string); id != "" {
		if c := FindChange(t, id); c != nil {
			c.AutoApproved = &AutoApproval{Repo: repo, Head: head, At: now}
			c.UpdatedAt = now
			t.QuietMerge = c.ID // V0.200
		}
	}
	answer := fmt.Sprintf("Auto-approved: %s merges without a press (head %s).", repo, shortSHA(head))
	return Result{
		Events: []Event{{"decision.closed", map[string]any{
			"id": d.ID, "answer": answer, "auto": true, "action": req.Action, "args": req.Args,
		}}},
		Effects: []Effect{{Kind: EffectRun, Allow: true, Request: &req}},
	}, nil
}

// PushQuiet reports that the thread's attention state is only an auto-merge
// at work (V0.200) or a question Fyr read in text (V0.210), so it sends no push; on says whether a repo is in the
// setting. Three states qualify: an ask Fyr approves itself, the PR that
// reads Ready while its approved merge runs, and done after that merge (the
// ask closing, or the turn the merge's result woke). A refused merge, a new
// head, a live deploy, a question and a message from anyone end it.
func PushQuiet(t *Thread, state State, on func(repo string) bool) bool {
	switch state {
	case Asking:
		for _, d := range t.Decisions {
			if repo, why := mergeableByFyr(t, d, false); why != "" || !on(repo) {
				return false
			}
		}
		return len(t.Decisions) > 0
	case Done:
		return autoMerged(t) != nil
	case Blocked:
		c := autoMerged(t)
		if c == nil || t.Unresponsive != nil {
			return false
		}
		ready, n := ThreadNeed(t)
		return n != nil && n.Cause == CauseReady && ready.ID == c.ID
	}
	return false
}

// autoMerged is the change whose auto-approved merge is still the thread's
// latest news, or nil.
func autoMerged(t *Thread) *Change {
	if t.QuietMerge == "" {
		return nil
	}
	c := FindChange(t, t.QuietMerge)
	if c == nil || c.AutoApproved == nil || c.AutoApproved.Repo == "" || c.PR == nil || c.Phase == PhaseLive || !SameHead(c.PR.Head, c.AutoApproved.Head) {
		return nil
	}
	if k := condition(c, CondMerge); k != nil && k.Status == CondFail {
		return nil
	}
	return c
}

// autoLine is the first sentence of an auto-approved effect's result.
func autoLine(req EffectReq) string {
	if on, _ := req.Args["autoDeploy"].(bool); on && req.Action == FyrDeploy { // V0.111
		commit, _ := req.Args["commit"].(string)
		return fmt.Sprintf("Auto-approved by the auto-deploy setting (%s).", shortSHA(commit))
	}
	repo, _ := req.Args["auto"].(string)
	if repo == "" {
		return ""
	}
	return fmt.Sprintf("Auto-approved by the auto-merge setting of %s.", repo)
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
