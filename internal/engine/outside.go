package engine

import (
	"context"
	"fmt"
	"log"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/github"
)

// Outside runs the Fyr effects that reach past the host (SPEC 5.10): a PR
// merge or a re-run of failed checks through GitHub, and a deploy. fyrd sets
// it as the engine's EffectRunner.
type Outside struct {
	Gh github.Runner
	// PR is the PR tab's cache (V0.43); a merge or a re-run forgets the PR.
	PR *github.Details
	// Deploy runs fyr.deploy in dir (V0.13).
	Deploy func(ctx context.Context, threadID string, req core.EffectReq, dir string) (string, error)
}

func (x Outside) Run(ctx context.Context, threadID string, req core.EffectReq) (string, error) {
	switch req.Action {
	case core.FyrMerge:
		url, _ := req.Args["pr"].(string)
		method, _ := req.Args["method"].(string)
		head, _ := req.Args["head"].(string)
		defer x.forget(url)
		// V0.43: read the PR again and refuse what GitHub would refuse;
		// V0.97: merge only the head the owner saw.
		return github.MergeChecked(ctx, x.Gh, url, method, head)
	case core.FyrRerun:
		url, _ := req.Args["pr"].(string)
		defer x.forget(url)
		return github.RerunFailed(ctx, x.Gh, url)
	case core.FyrDeploy:
		if x.Deploy == nil {
			return "", fmt.Errorf("this host does not deploy")
		}
		dir, _ := req.Args["dir"].(string)
		return x.Deploy(ctx, threadID, req, dir)
	}
	return "", fmt.Errorf("unknown effect %q", req.Action)
}

func (x Outside) forget(url string) {
	if x.PR != nil {
		x.PR.Forget(url)
	}
}

// PRWatch is what the GitHub poller reads and reports (V0.43, V0.66): the
// PRs of the threads' code changes and the merge commits whose deployments
// to read, and the observations, as Inputs from the observers.
type PRWatch struct{ E *Engine }

// Snapshot is the PR of every code change in flight, and of a merged one
// whose checks have not settled (V0.82), the main one among
// them (V0.59 (4), #32), of the threads that are not archived.
func (w PRWatch) Snapshot() []github.PRThread {
	var out []github.PRThread
	now := w.E.Now()
	for _, t := range w.E.Snapshot() {
		if t.ArchivedAt != nil {
			continue
		}
		for _, u := range core.PRURLs(t.Thread, now) {
			out = append(out, github.PRThread{ID: t.ID, URL: u})
		}
	}
	return out
}

func (w PRWatch) Observe(id string, pr core.PR) {
	w.E.Apply(core.Input{Source: core.Observer(core.ObsGitHub), Kind: core.KindPRObserved, Thread: id, Args: pr})
}

// Deploys are the merge commits whose deployments the poller reads (V0.66).
func (w PRWatch) Deploys() []github.DeployThread {
	var out []github.DeployThread
	now := w.E.Now()
	for _, t := range w.E.Snapshot() {
		if t.ArchivedAt != nil {
			continue
		}
		for _, d := range core.DeployWatches(t.Thread, now) {
			out = append(out, github.DeployThread{ID: t.ID, Change: d.Change, Repo: d.Repo, SHA: d.SHA, First: d.First})
		}
	}
	return out
}

func (w PRWatch) ObserveDeploy(id, change string, d core.DeployObs) {
	w.E.Apply(core.Input{Source: core.Observer(core.ObsDeploy), Kind: core.KindDeployObserved, Thread: id, Args: core.DeployObservedArgs{Change: change, Obs: d}})
}

// SelfDeployed makes live, in every thread, the landed code changes of repo
// whose merge commit the deployed build holds (V0.82): contains asks git,
// outside the engine's lock, and each thread's changes go down the Input
// path as one observation of the deploy observer. It returns how many
// changes it sent.
func (e *Engine) SelfDeployed(repo, build string, contains func(sha string) bool) int {
	n := 0
	for _, t := range e.Snapshot() {
		var ids []string
		for _, r := range core.SelfDeployCandidates(t.Thread, repo) {
			if contains(r.SHA) {
				ids = append(ids, r.Change)
			}
		}
		if len(ids) == 0 {
			continue
		}
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsDeploy), Kind: core.KindSelfDeployed, Thread: t.ID,
			Args: core.SelfDeployedArgs{Build: build, Changes: ids}}); err != nil {
			log.Printf("self-deploy %s: thread %s: %v", build, t.ID, err)
			continue
		}
		n += len(ids)
	}
	return n
}

// ThreadsOn counts the threads on a profile that are not archived; a
// provider in use is not deleted (V0.50).
func (e *Engine) ThreadsOn(profile string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, t := range e.threads {
		if t.Profile == profile && t.ArchivedAt == nil {
			n++
		}
	}
	return n
}
