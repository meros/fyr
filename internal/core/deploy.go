package core

import (
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

// DeployWatch is the merge commit Fyr reads deployments of.
type DeployWatch struct {
	Repo     string    `json:"repo"`
	SHA      string    `json:"sha"`
	MergedAt time.Time `json:"mergedAt"`
	// Checked: the first read is done; Seen: the commit has a deployment;
	// Done: the watch ended (live, production failed, or no deployments).
	Checked bool `json:"checked,omitempty"`
	Seen    bool `json:"seen,omitempty"`
	Done    bool `json:"done,omitempty"`
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
}

const (
	watchNoDeploy = 24 * time.Hour
	watchMax      = 7 * 24 * time.Hour
)

// watchMerge starts the watch when the PR merged and the change is not live.
func watchMerge(c *Change, p PR, now time.Time) {
	if p.State != "MERGED" || p.MergeSHA == "" || p.Repo == "" || c.DeployWatch != nil || c.Phase != PhaseLanded {
		return
	}
	at := now
	if p.MergedAt != nil {
		at = *p.MergedAt
	}
	c.DeployWatch = &DeployWatch{Repo: p.Repo, SHA: p.MergeSHA, MergedAt: at}
}

func watching(c *Change, now time.Time) bool {
	w := c.DeployWatch
	if w == nil || w.Done || c.Phase != PhaseLanded {
		return false
	}
	limit := watchNoDeploy
	if w.Seen {
		limit = watchMax
	}
	return now.Sub(w.MergedAt) < limit
}

// DeployWatches are the merge commits of the thread to read deployments of.
func DeployWatches(t *Thread, now time.Time) []DeployRef {
	var out []DeployRef
	for i := range t.Changes {
		c := &t.Changes[i]
		if watching(c, now) {
			out = append(out, DeployRef{Change: c.ID, Repo: c.DeployWatch.Repo, SHA: c.DeployWatch.SHA, First: !c.DeployWatch.Checked})
		}
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
	if c == nil || c.DeployWatch == nil || c.DeployWatch.Done || c.Phase != PhaseLanded {
		return
	}
	w := c.DeployWatch
	w.Checked = true
	if d.None {
		w.Done = true
		return
	}
	defer syncBuiltins(c)
	for _, x := range d.Deployments {
		w.Seen = true
		prod := IsProduction(x.Env, x.Production)
		typ := "deploy"
		if !prod {
			typ = "deploy:" + x.Env
		}
		switch x.State {
		case "success", "inactive":
			setCondition(c, typ, "ok", "", ByObserved, now)
			if prod {
				w.Done = true
				if u := firstOf(x.URL, x.LogURL); u != "" {
					addEvidence(c, Evidence{Kind: "deployment", URL: u, Title: x.Env})
				}
				setPhase(t, c, PhaseLive, ByObserved, now)
			}
		case "failure", "error":
			if prod {
				if k := condition(c, typ); k == nil || k.Status != "fail" {
					t.ActivityAt = now
				}
				w.Done = true
				if x.LogURL != "" {
					addEvidence(c, Evidence{Kind: "deploy-log", URL: x.LogURL, Title: x.Env})
				}
			}
			setCondition(c, typ, "fail", "deploy to "+x.Env+" failed", ByObserved, now)
		default:
			setCondition(c, typ, "pending", "deploying to "+x.Env, ByObserved, now)
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
