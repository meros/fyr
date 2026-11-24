package core

import (
	"slices"
	"testing"
	"time"
)

// SPEC 0.5 V0.82 (1): a PR merged before its checks ended keeps being read
// until they settle, for at most 24 hours after the merge; the phase does
// not move back, and a live change stays live.
func TestV082MergedPRPolledUntilChecksSettle(t *testing.T) {
	url := "https://github.com/acme/app/pull/71"
	merged := ct0
	pr := func(state string, checks Checks) PR {
		p := PR{URL: url, Repo: "acme/app", Number: 71, State: state, Checks: checks, Branch: "feat"}
		if state == "MERGED" {
			p.MergeSHA, p.MergedAt = "m71", &merged
		}
		return p
	}
	th := &Thread{ID: "t1"}
	ChangePRObserved(th, pr("OPEN", ChecksPending), ct0.Add(-time.Hour))
	ChangePRObserved(th, pr("MERGED", ChecksPending), ct0.Add(time.Minute))
	c := &th.Changes[0]
	if c.Phase != PhaseLanded {
		t.Fatalf("phase %s", c.Phase)
	}
	if got := PRURLs(th, ct0.Add(2*time.Minute)); !slices.Contains(got, url) {
		t.Fatalf("a merged PR with pending checks is not polled: %v", got)
	}
	// The next poll reads SUCCESS: the snapshot takes it, the phase stays.
	ChangePRObserved(th, pr("MERGED", ChecksPass), ct0.Add(4*time.Minute))
	c = &th.Changes[0]
	if c.PR.Checks != ChecksPass || c.Phase != PhaseLanded {
		t.Fatalf("after the poll: checks %s phase %s", c.PR.Checks, c.Phase)
	}
	if got := PRURLs(th, ct0.Add(5*time.Minute)); len(got) != 0 {
		t.Fatalf("settled checks are still polled: %v", got)
	}

	// One that stays pending, or has none read yet, stops after 24 hours.
	for _, checks := range []Checks{ChecksPending, ChecksNone} {
		th := &Thread{ID: "t2"}
		ChangePRObserved(th, pr("MERGED", checks), ct0.Add(time.Minute))
		if got := PRURLs(th, ct0.Add(23*time.Hour)); !slices.Contains(got, url) {
			t.Fatalf("%s: not polled within the limit: %v", checks, got)
		}
		if got := PRURLs(th, ct0.Add(24*time.Hour+time.Minute)); len(got) != 0 {
			t.Fatalf("%s: still polled after the limit: %v", checks, got)
		}
	}

	// A live change takes the new checks and stays live.
	th = &Thread{ID: "t3"}
	ChangePRObserved(th, pr("MERGED", ChecksPending), ct0.Add(time.Minute))
	setPhase(th, &th.Changes[0], PhaseLive, ByObserved, ct0.Add(2*time.Minute))
	if got := PRURLs(th, ct0.Add(3*time.Minute)); !slices.Contains(got, url) {
		t.Fatalf("a live change with pending checks is not polled: %v", got)
	}
	ChangePRObserved(th, pr("MERGED", ChecksPass), ct0.Add(4*time.Minute))
	if c := th.Changes[0]; c.Phase != PhaseLive || c.PR.Checks != ChecksPass {
		t.Fatalf("live change: phase %s checks %s", c.Phase, c.PR.Checks)
	}

	// A repo without deployments ended the deploy watch; a later read of the
	// merged PR does not arm it again.
	th = &Thread{ID: "t4"}
	ChangePRObserved(th, pr("MERGED", ChecksPending), ct0.Add(time.Minute))
	id := th.Changes[0].ID
	if FindRule(th, deployRuleID(id)) == nil {
		t.Fatal("setup: no deploy watch")
	}
	ChangeDeployObserved(th, id, DeployObs{None: true}, false, ct0.Add(2*time.Minute))
	ChangePRObserved(th, pr("MERGED", ChecksPending), ct0.Add(4*time.Minute))
	if FindRule(th, deployRuleID(id)) != nil {
		t.Fatal("the deploy watch was armed again")
	}
}

// SPEC 0.5 V0.82 (1): a change that landed before the poller saw its PR
// merged (the git observer landed it) is read until the PR is merged.
func TestV082LandedChangeWithOpenPRIsPolled(t *testing.T) {
	url := "https://github.com/acme/app/pull/72"
	th := &Thread{ID: "t1"}
	ChangePRObserved(th, PR{URL: url, Repo: "acme/app", Number: 72, State: "OPEN", Checks: ChecksPass}, ct0)
	setPhase(th, &th.Changes[0], PhaseLanded, ByObserved, ct0.Add(time.Minute))
	if got := PRURLs(th, ct0.Add(2*time.Minute)); !slices.Contains(got, url) {
		t.Fatalf("not polled: %v", got)
	}
	if got := PRURLs(th, ct0.Add(25*time.Hour)); len(got) != 0 {
		t.Fatalf("polled after the limit: %v", got)
	}
}

// SPEC 0.5 V0.82 (2): a self-deploy moves the landed code changes of the
// deployed repo whose merge commit is in the build to live, observed, with
// a deploy condition ok and the build as evidence; others stay.
func TestV082SelfDeployMakesChangesLive(t *testing.T) {
	th := &Thread{ID: "t1"}
	merged := ct0
	for _, p := range []PR{
		{URL: "https://github.com/meros/fyr/pull/71", Repo: "meros/fyr", Number: 71, State: "MERGED", Checks: ChecksPass, MergeSHA: "m71", MergedAt: &merged},
		{URL: "https://github.com/meros/fyr/pull/73", Repo: "meros/fyr", Number: 73, State: "MERGED", Checks: ChecksPass, MergeSHA: "m73", MergedAt: &merged},
		{URL: "https://github.com/meros/other/pull/5", Repo: "meros/other", Number: 5, State: "MERGED", Checks: ChecksPass, MergeSHA: "m5", MergedAt: &merged},
		{URL: "https://github.com/meros/fyr/pull/80", Repo: "meros/fyr", Number: 80, State: "OPEN", Checks: ChecksPass},
	} {
		ChangePRObserved(th, p, ct0)
	}
	refs := SelfDeployCandidates(th, "github.com/Meros/fyr")
	var shas []string
	for _, r := range refs {
		shas = append(shas, r.SHA)
	}
	slices.Sort(shas)
	if !slices.Equal(shas, []string{"m71", "m73"}) {
		t.Fatalf("candidates %+v", refs)
	}
	c71 := th.Changes[0].ID
	now := ct0.Add(time.Hour)
	// The agent cannot send it.
	if _, _, _, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindSelfDeployed, Thread: "t1", Args: SelfDeployedArgs{Build: "b1", Changes: []string{c71}}}, now); err == nil {
		t.Fatal("the agent sent a deploy observation")
	}
	th, _, _, err := Reduce(th, &View{}, Input{Source: Observer(ObsDeploy), Kind: KindSelfDeployed, Thread: "t1", Args: SelfDeployedArgs{Build: "20261116-073115", Changes: []string{c71}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	c := th.Changes[0]
	if c.Phase != PhaseLive || c.PhaseBy != ByObserved {
		t.Fatalf("phase %s by %s", c.Phase, c.PhaseBy)
	}
	if k := condition(&c, CondDeploy); k == nil || k.Status != CondOK || k.Source != ByObserved {
		t.Fatalf("deploy condition %+v", k)
	}
	if !slices.ContainsFunc(c.Evidence, func(e Evidence) bool { return e.Kind == "build" && e.Title == "20261116-073115" }) {
		t.Fatalf("evidence %+v", c.Evidence)
	}
	if th.Changes[1].Phase != PhaseLanded {
		t.Fatalf("a change not in the build moved: %s", th.Changes[1].Phase)
	}
	if got := SelfDeployCandidates(th, "github.com/meros/fyr"); len(got) != 1 || got[0].SHA != "m73" {
		t.Fatalf("a live change is a candidate again: %+v", got)
	}
}
