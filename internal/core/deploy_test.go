package core

import (
	"strings"
	"testing"
	"time"
)

const dpr = "https://github.com/o/r/pull/7"

func mergedThread(t *testing.T) (*Thread, *Change) {
	t.Helper()
	th := &Thread{}
	ChangePRObserved(th, PR{URL: dpr, Repo: "o/r", Number: 7, Title: "Fix it", State: "OPEN", Checks: "pass", Head: "h1"}, ct0)
	at := ct0.Add(time.Minute)
	ChangePRObserved(th, PR{URL: dpr, Repo: "o/r", Number: 7, Title: "Fix it", State: "MERGED", Checks: "pass", MergeSHA: "m1", MergedAt: &at}, ct0.Add(2*time.Minute))
	return th, &th.Changes[0]
}

// SPEC 0.5 V0.66 (1): a merged PR's change watches its merge commit.
func TestMergeStartsDeployWatch(t *testing.T) {
	th, c := mergedThread(t)
	if c.Phase != PhaseLanded || c.DeployWatch == nil || c.DeployWatch.SHA != "m1" || c.DeployWatch.Repo != "o/r" || !c.DeployWatch.MergedAt.Equal(ct0.Add(time.Minute)) {
		t.Fatalf("watch: %+v %+v", c.Phase, c.DeployWatch)
	}
	w := DeployWatches(th, ct0.Add(time.Hour))
	if len(w) != 1 || w[0].Change != c.ID || w[0].SHA != "m1" || w[0].Repo != "o/r" || !w[0].First {
		t.Fatalf("watches: %+v", w)
	}
}

// SPEC 0.5 V0.66 (3): a production success moves the change to live, with
// the environment URL as evidence; the watch ends.
func TestProductionSuccessIsLive(t *testing.T) {
	th, c := mergedThread(t)
	at := ct0.Add(10 * time.Minute)
	ChangeDeployObserved(th, c.ID, DeployObs{Deployments: []Deployment{
		{Env: "staging", State: "success", URL: "https://staging.example"},
		{Env: "Production", State: "success", URL: "https://app.example", LogURL: "https://github.com/o/r/actions/runs/9"},
	}}, at)
	if c.Phase != PhaseLive || c.PhaseBy != ByObserved {
		t.Fatalf("phase %s by %s", c.Phase, c.PhaseBy)
	}
	if k := condition(c, "deploy"); k == nil || k.Status != "ok" || k.Source != ByObserved {
		t.Fatalf("deploy: %+v", k)
	}
	if k := condition(c, "deploy:staging"); k == nil || k.Status != "ok" {
		t.Fatalf("deploy:staging: %+v", k)
	}
	found := false
	for _, ev := range c.Evidence {
		found = found || (ev.Kind == "deployment" && ev.URL == "https://app.example")
	}
	if !found {
		t.Fatalf("evidence: %+v", c.Evidence)
	}
	if len(DeployWatches(th, at)) != 0 {
		t.Fatal("a live change is still watched")
	}
	if !th.ActivityAt.Equal(at) {
		t.Fatal("going live does not move activityAt")
	}
}

// SPEC 0.5 V0.66 (3), (6): a production failure blocks the thread and offers
// Investigate the failed deploy; the watch ends.
func TestProductionFailureBlocks(t *testing.T) {
	th, c := mergedThread(t)
	at := ct0.Add(10 * time.Minute)
	ChangeDeployObserved(th, c.ID, DeployObs{Deployments: []Deployment{{Env: "prod-eu", Production: true, State: "failure", LogURL: "https://github.com/o/r/actions/runs/9"}}}, at)
	if c.Phase != PhaseLanded {
		t.Fatalf("phase %s", c.Phase)
	}
	k := condition(c, "deploy")
	if k == nil || k.Status != "fail" || k.Reason != "deploy to prod-eu failed" {
		t.Fatalf("deploy: %+v", k)
	}
	if got := ChangeBlocking(th); got != "Fix it: deploy to prod-eu failed" {
		t.Fatalf("blocking %q", got)
	}
	var inv *Offer
	for i := range c.Actions {
		if c.Actions[i].ID == "fyr.deploy" {
			inv = &c.Actions[i]
		}
	}
	if inv == nil || inv.Run != RunAgent || inv.Label != "Investigate the failed deploy" || !strings.Contains(inv.Prompt, "https://github.com/o/r/actions/runs/9") || inv.Digest == "" {
		t.Fatalf("actions: %+v", c.Actions)
	}
	if len(DeployWatches(th, at)) != 0 {
		t.Fatal("a failed production deploy is still watched")
	}
}

// SPEC 0.5 V0.66 (3), (4): pending production and other environments are
// conditions; they do not move the phase.
func TestPendingAndStaging(t *testing.T) {
	th, c := mergedThread(t)
	at := ct0.Add(10 * time.Minute)
	ChangeDeployObserved(th, c.ID, DeployObs{Deployments: []Deployment{
		{Env: "production", State: "in_progress"},
		{Env: "staging", State: "error"},
	}}, at)
	if c.Phase != PhaseLanded {
		t.Fatalf("phase %s", c.Phase)
	}
	if k := condition(c, "deploy"); k == nil || k.Status != "pending" || k.Reason != "deploying to production" {
		t.Fatalf("deploy: %+v", k)
	}
	if k := condition(c, "deploy:staging"); k == nil || k.Status != "fail" {
		t.Fatalf("deploy:staging: %+v", k)
	}
	w := DeployWatches(th, at)
	if len(w) != 1 || w[0].First {
		t.Fatalf("watch after a seen deployment: %+v", w)
	}
	// Seen deployments keep the watch past 24 h, up to 7 days.
	if len(DeployWatches(th, ct0.Add(48*time.Hour))) != 1 || len(DeployWatches(th, ct0.Add(8*24*time.Hour))) != 0 {
		t.Fatal("watch window with deployments")
	}
}

// SPEC 0.5 V0.66 (2), (5): a repo without deployments ends the watch and
// changes nothing; without a deployment the watch ends after 24 h.
func TestNoDeployments(t *testing.T) {
	th, c := mergedThread(t)
	before := len(c.Conditions)
	ChangeDeployObserved(th, c.ID, DeployObs{None: true}, ct0.Add(5*time.Minute))
	if c.Phase != PhaseLanded || len(c.Conditions) != before || len(DeployWatches(th, ct0.Add(6*time.Minute))) != 0 {
		t.Fatalf("no deployments: %+v", c)
	}

	th2, c2 := mergedThread(t)
	ChangeDeployObserved(th2, c2.ID, DeployObs{}, ct0.Add(5*time.Minute)) // the repo has some, not this commit yet
	if w := DeployWatches(th2, ct0.Add(23*time.Hour)); len(w) != 1 || w[0].First {
		t.Fatalf("within 24 h: %+v", w)
	}
	if len(DeployWatches(th2, ct0.Add(25*time.Hour))) != 0 {
		t.Fatal("watched past 24 h without a deployment")
	}
}

// SPEC 0.5 V0.66 (1): a change reported live before the merge is not watched.
func TestLiveIsNotWatched(t *testing.T) {
	th := &Thread{}
	ChangePRObserved(th, PR{URL: dpr, Repo: "o/r", Number: 7, State: "OPEN", Checks: "pass"}, ct0)
	c := &th.Changes[0]
	c.Phase = PhaseLive
	ChangePRObserved(th, PR{URL: dpr, Repo: "o/r", Number: 7, State: "MERGED", MergeSHA: "m1"}, ct0)
	if c.DeployWatch != nil || len(DeployWatches(th, ct0)) != 0 {
		t.Fatalf("watch on a live change: %+v", c.DeployWatch)
	}
}

// SPEC 0.5 V0.66 (7): annotations follow failing checks of an open PR.
func TestAnnotationsFollowFailingChecks(t *testing.T) {
	th := &Thread{}
	notes := []Annotation{{Path: "a.go", Line: 3, Level: "failure", Message: "boom", Check: "lint"}}
	ChangePRObserved(th, PR{URL: dpr, Repo: "o/r", Number: 7, State: "OPEN", Checks: "fail", Head: "h1", Annotations: notes}, ct0)
	c := &th.Changes[0]
	if len(c.Annotations) != 1 || c.Annotations[0].Check != "lint" {
		t.Fatalf("annotations: %+v", c.Annotations)
	}
	ChangePRObserved(th, PR{URL: dpr, Repo: "o/r", Number: 7, State: "OPEN", Checks: "pending", Head: "h2"}, ct0)
	if c.Annotations != nil {
		t.Fatalf("annotations kept after the checks stopped failing: %+v", c.Annotations)
	}
}
