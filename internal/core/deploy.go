package core

import (
	"slices"
	"strings"
	"time"
)

// Merged to live (SPEC 0.5 V0.66): GitHub Deployments of a merged PR's merge
// commit move its change to live, and failed checks' annotations ride on the
// change while the PR is open.

// Annotation is a check run's annotation on the PR's head commit.
type Annotation struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	EndLine int    `json:"endLine,omitempty"`
	Level   string `json:"level"` // failure, warning, notice
	Message string `json:"message"`
	Title   string `json:"title,omitempty"`
	Check   string `json:"check"`
}

// Deployment is the newest deployment of one environment and its newest
// status: success, inactive, failure, error, or a pending state.
type Deployment struct {
	Env        string
	Production bool
	State      string
	URL        string // the environment URL
	LogURL     string
}

// DeployObs is one read of a merge commit's deployments. None: the repo has
// no deployment at all.
type DeployObs struct {
	None        bool
	Deployments []Deployment
}

// DeployRef is one watched merge commit for the poller. First: read the
// repo's newest deployments, so a repo without any costs one call.
type DeployRef struct {
	Change, Repo, SHA string
	First             bool
	// Since is the merge; the poller reads less often as the watch ages
	// (V0.173).
	Since time.Time
}

const (
	watchNoDeploy = 24 * time.Hour
	watchMax      = 7 * 24 * time.Hour
)

// The deploy watch (#46) is a Rule of the thread about a landed change: the
// deployments observer reads its merge commit while it is armed, and it
// ends when the change is live, the production deploy failed, the change
// ended (#48), or its time limit passes: 24 hours without a deployment of
// the commit, 7 days with one. A repo without any deployment ends it at the
// first read.
func deployRuleID(changeID string) string { return PurposeDeploy + ":" + changeID }

func deployLimit(since time.Time, seen bool) When {
	limit := watchNoDeploy
	if seen {
		limit = watchMax
	}
	at := since.Add(limit)
	return When{Kind: WhenAt, At: &at}
}

func deployWhen(c *Change, since time.Time, seen bool) When {
	return Any(When{Kind: WhenReached, Change: c.ID, Phase: PhaseLive},
		When{Kind: WhenCondition, Change: c.ID, Type: CondDeploy, Status: CondFail},
		deployLimit(since, seen),
		When{Kind: WhenEnded, Change: c.ID})
}

// endsWithChange gives a deploy watch stored before #48 its changeEnded
// clause.
func endsWithChange(r *Rule) {
	if r.Purpose != PurposeDeploy || slices.ContainsFunc(r.When.Any, func(w When) bool { return w.Kind == WhenEnded }) {
		return
	}
	r.When.Any = append(r.When.Any, When{Kind: WhenEnded, Change: r.About})
}

// watchMerge arms the watch when the PR merged and the change is not live.
// A change that already has a deploy condition was watched before.
func watchMerge(t *Thread, c *Change, p PR, now time.Time) {
	if p.State != "MERGED" || p.MergeSHA == "" || p.Repo == "" || c.Phase != PhaseLanded || FindRule(t, deployRuleID(c.ID)) != nil || deploySeen(c) {
		return
	}
	at := now
	if p.MergedAt != nil {
		at = *p.MergedAt
	}
	if now.Sub(at) >= watchNoDeploy {
		return
	}
	ArmRule(t, Rule{ID: deployRuleID(c.ID), Purpose: PurposeDeploy, About: c.ID, When: deployWhen(c, at, false),
		Then: Then{Kind: KindRuleEnd}, After: AfterDisarm, CreatedBy: BySystem,
		Observe: &Observe{Observer: ObsDeploy, Repo: p.Repo, SHA: p.MergeSHA, Since: at}}, now)
}

// deploySeen reports a deployment of the change observed: a deploy
// condition.
func deploySeen(c *Change) bool {
	return slices.ContainsFunc(c.Conditions, func(k Condition) bool {
		return k.Type == CondDeploy || strings.HasPrefix(string(k.Type), condDeployPrefix)
	})
}

// DeployWatches are the merge commits of the thread to read deployments of:
// its armed deploy Rules that do not end at now.
func DeployWatches(t *Thread, now time.Time) []DeployRef {
	var out []DeployRef
	for _, r := range RulesOf(t, PurposeDeploy) {
		if r.Status != RuleArmed || r.Observe == nil || r.When.eval(t, r.Armed, now) == yes {
			continue
		}
		out = append(out, DeployRef{Change: r.About, Repo: r.Observe.Repo, SHA: r.Observe.SHA, First: !r.Observe.Read, Since: r.Observe.Since})
	}
	return out
}

// IsProduction reports a production environment by the deployment's flag or
// its name.
func IsProduction(env string, flag bool) bool {
	e := strings.ToLower(strings.TrimSpace(env))
	return flag || e == "production" || e == "prod"
}

// ChangeDeployObserved writes one read of deployments into the change.
func ChangeDeployObserved(t *Thread, changeID string, d DeployObs, now time.Time) {
	c := FindChange(t, changeID)
	r := FindRule(t, deployRuleID(changeID))
	if c == nil || r == nil || r.Observe == nil || c.Phase != PhaseLanded {
		return
	}
	r.Observe.Read = true
	if d.None {
		DropRules(t, func(x Rule) bool { return x.ID == r.ID })
		return
	}
	if len(d.Deployments) > 0 && !deploySeen(c) { // seen: the limit is 7 days
		r.When = deployWhen(c, r.Observe.Since, true)
	}
	for _, x := range d.Deployments {
		prod := IsProduction(x.Env, x.Production)
		typ := CondDeploy
		if !prod {
			typ = CondDeployTo(x.Env)
		}
		switch x.State {
		case "success", "inactive":
			setCondition(c, typ, CondOK, "", ByObserved, now)
			if prod {
				if u := firstOf(x.URL, x.LogURL); u != "" {
					addEvidence(c, Evidence{Kind: "deployment", URL: u, Title: x.Env})
				}
				setPhase(t, c, PhaseLive, ByObserved, now)
			}
		case "failure", "error":
			if prod {
				if k := condition(c, typ); k == nil || k.Status != CondFail {
					t.ActivityAt = now
				}
				if x.LogURL != "" {
					addEvidence(c, Evidence{Kind: "deploy-log", URL: x.LogURL, Title: x.Env})
				}
			}
			setCondition(c, typ, CondFail, "deploy to "+x.Env+" failed", ByObserved, now)
		default:
			setCondition(c, typ, CondPending, "deploying to "+x.Env, ByObserved, now)
		}
	}
	c.UpdatedAt = now
}

func firstOf(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// SelfDeployCandidates are the thread's landed code changes in repo (the
// git observer's form, github.com/owner/name) with a merged PR's merge
// commit: what a self-deploy of repo may have made live (V0.82). The host
// asks git which of them the build holds.
func SelfDeployCandidates(t *Thread, repo string) []DeployRef {
	var out []DeployRef
	for i := range t.Changes {
		c := &t.Changes[i]
		if selfDeployable(c, repo) {
			out = append(out, DeployRef{Change: c.ID, Repo: repo, SHA: c.PR.MergeSHA})
		}
	}
	return out
}

func selfDeployable(c *Change, repo string) bool {
	if c.Kind != KindCode || c.Phase != PhaseLanded || c.PR == nil || c.PR.MergeSHA == "" || repo == "" {
		return false
	}
	return slices.ContainsFunc(c.Targets, func(x string) bool {
		r, ok := strings.CutPrefix(x, "repo:")
		return ok && strings.EqualFold(r, repo)
	})
}

// ChangesSelfDeployed moves the changes a self-deploy's build holds to live
// (V0.82): observed, with the condition deploy ok and the build as
// evidence. A change that is no longer landed, or that the owner ended,
// stays.
func ChangesSelfDeployed(t *Thread, a SelfDeployedArgs, now time.Time) {
	for _, id := range a.Changes {
		c := FindChange(t, id)
		if c == nil || c.Kind != KindCode || c.Phase != PhaseLanded || CanWrite(FieldChangePhase, ByObserved, c.PhaseBy, "") != nil {
			continue
		}
		setCondition(c, CondDeploy, CondOK, "", ByObserved, now)
		if a.Build != "" {
			addEvidence(c, Evidence{Kind: "build", Title: a.Build})
		}
		setPhase(t, c, PhaseLive, ByObserved, now)
		c.UpdatedAt = now
	}
}
