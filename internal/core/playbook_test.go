package core

import (
	"strings"
	"testing"
	"time"
)

func learn(t *testing.T, ps map[string]*Project, src Source, args Args) (Result, error) {
	t.Helper()
	args["id"] = "p1"
	_, after, res, err := ReduceProject(ps, nil, Input{Source: src, Kind: KindProjectLearn, Args: args, CmdID: "c"}, ct0.Add(time.Duration(len(ps["p1"].Playbook)+1)*time.Minute))
	if err == nil {
		ps["p1"] = after
	}
	return res, err
}

// V0.project-playbook: an agent writes an entry, the owner's entry is kept
// against it, and a probe runs only on the owner's yes, for that command.
func TestPlaybookLearn(t *testing.T) {
	ps := map[string]*Project{"p1": {ID: "p1", Name: "Acme"}}
	res, err := learn(t, ps, FromAgent, Args{"slot": "stage", "name": "Dev", "how": "automatic on merge, about 5 min", "url": "https://dev.example.com", "probe": "check {sha}", "thread": "t1"})
	if err != nil {
		t.Fatal(err)
	}
	e := ps["p1"].Playbook[0]
	if e.Name != "dev" || e.By != ByAgent || e.Thread != "t1" || e.ProbeOK || e.Runs() || res.Reply.(map[string]any)["needsAllow"] != true || ps["p1"].PlaybookAt.IsZero() {
		t.Fatalf("%+v %+v", e, res.Reply)
	}
	if _, err := learn(t, ps, FromAgent, Args{"slot": "stage", "name": "dev", "allow": true}); err == nil {
		t.Fatal("an agent allowed a probe")
	}
	if _, err := learn(t, ps, FromOwner, Args{"slot": "stage", "name": "dev", "allow": true}); err != nil || !ps["p1"].Playbook[0].Runs() {
		t.Fatalf("allow: %v %+v", err, ps["p1"].Playbook[0])
	}
	// The same command keeps the yes; another one waits again.
	if _, err := learn(t, ps, FromAgent, Args{"slot": "stage", "name": "dev", "how": "automatic on merge", "probe": "check {sha}"}); err != nil || !ps["p1"].Playbook[0].ProbeOK {
		t.Fatalf("the same probe lost its yes: %v", err)
	}
	if _, err := learn(t, ps, FromAgent, Args{"slot": "stage", "name": "dev", "how": "automatic on merge", "probe": "other {sha}"}); err != nil || ps["p1"].Playbook[0].ProbeOK {
		t.Fatalf("a changed probe kept the yes: %v", err)
	}
	// The owner's entry is kept against the agent, also against a removal.
	if _, err := learn(t, ps, FromOwner, Args{"slot": "issue", "how": "GitHub issues, label bug"}); err != nil {
		t.Fatal(err)
	}
	// V0.playbook-moments: what the agent writes there waits as a suggestion.
	if _, err := learn(t, ps, FromAgent, Args{"slot": "issue", "how": "Linear"}); err != nil || ps["p1"].Playbook[1].How != "GitHub issues, label bug" || ps["p1"].Playbook[1].Suggest == nil {
		t.Fatalf("an agent wrote over the owner: %v", err)
	}
	if _, err := learn(t, ps, FromAgent, Args{"slot": "issue", "remove": true}); err == nil {
		t.Fatal("an agent removed the owner's entry")
	}
	if _, err := learn(t, ps, FromOwner, Args{"slot": "stage", "name": "prod", "how": "release tag"}); err != nil {
		t.Fatal(err)
	}
	st := ps["p1"].Stages()
	if len(st) != 2 || st[0].Name != "dev" || !st[1].IsLive() || len(ps["p1"].ProbeStages()) != 0 {
		t.Fatalf("stages %+v", st)
	}
	for _, bad := range []Args{
		{"slot": "deploy", "how": "x"},
		{"slot": "stage", "how": "x"},
		{"slot": "stage", "name": "has space", "how": "x"},
		{"slot": "verify"},
		{"slot": "verify", "how": "x", "probe": "y"},
		{"slot": "stage", "name": "qa", "url": "ftp://x"},
		{"slot": "run", "how": strings.Repeat("x", 601)},
	} {
		if _, err := learn(t, ps, FromAgent, bad); err == nil {
			t.Fatalf("taken: %v", bad)
		}
	}
	if _, err := learn(t, ps, FromOwner, Args{"slot": "stage", "name": "dev", "remove": true}); err != nil || len(ps["p1"].Stages()) != 1 {
		t.Fatalf("remove: %v", err)
	}
	pr := PlaybookPrompt(ps["p1"])
	if !strings.Contains(pr, `Stage prod, live: release tag; no probe`) || !strings.Contains(pr, "Issue: GitHub issues, label bug") {
		t.Fatalf("prompt:\n%s", pr)
	}
	if PlaybookPrompt(&Project{Name: "x"}) != "" {
		t.Fatal("an empty playbook says something")
	}
}

// V0.project-playbook: a link's members take the newest playbook whole.
func TestPlaybookShared(t *testing.T) {
	old := ct0.Add(time.Hour)
	ps := map[string]*Project{"p1": {ID: "p1", Name: "Acme", Link: "l_0123456789abcdef", PlaybookAt: old, Playbook: []PlaybookEntry{{Slot: SlotRun, How: "make run", By: ByOwner}}}}
	send := func(at time.Time, pb any) (*Project, error) {
		_, after, _, err := ReduceProject(ps, nil, Input{Source: FromOwner, Kind: KindProjectSharedPlaybook, CmdID: "c",
			Args: Args{"id": "p1", "link": "l_0123456789abcdef", "at": at.Format(time.RFC3339Nano), "playbook": pb}}, ct0)
		return after, err
	}
	stage := map[string]any{"slot": "stage", "name": "dev", "probe": "check {sha}", "probeOk": true, "by": "agent"}
	if p, err := send(old.Add(-time.Minute), []any{stage}); err != nil || p.Playbook[0].Slot != SlotRun {
		t.Fatalf("an older playbook won: %v %+v", err, p)
	}
	p, err := send(old.Add(time.Minute), []any{stage})
	if err != nil || len(p.Playbook) != 1 || !p.Playbook[0].Runs() || !p.PlaybookAt.Equal(old.Add(time.Minute)) {
		t.Fatalf("%v %+v", err, p)
	}
	if _, err := send(old.Add(2*time.Minute), []any{map[string]any{"slot": "nope"}}); err == nil {
		t.Fatal("an unknown slot was stored")
	}
	if p, err := send(old.Add(3*time.Minute), nil); err != nil || len(p.Playbook) != 0 {
		t.Fatalf("an emptied playbook did not follow: %v", err)
	}
}

// V0.project-playbook: a probe's read moves the change through its stages,
// the stage's address is kept, and the agent's line says where to look. A
// repo without GitHub deployments keeps its watch for the probes.
func TestProbeStages(t *testing.T) {
	th, c := mergedThread(t)
	ChangeDeployObserved(th, c.ID, DeployObs{None: true}, true, ct0.Add(3*time.Minute))
	if len(DeployWatches(th, ct0.Add(time.Hour))) != 0 || len(ProbeWatches(th, ct0.Add(time.Hour))) != 1 {
		t.Fatalf("GitHub is read again, or the probes lost their watch: %+v", th.Rules)
	}
	before := *th
	before.Changes = []Change{*c}
	before.Changes[0].Conditions = append([]Condition(nil), c.Conditions...)
	dev := PlaybookEntry{Slot: SlotStage, Name: "dev"}
	prod := PlaybookEntry{Slot: SlotStage, Name: "prod"}
	ChangeDeployObserved(th, c.ID, DeployObs{Probe: true, Deployments: []Deployment{{Env: "dev", State: "success", URL: "https://dev.example.com"}}}, true, ct0.Add(10*time.Minute))
	if !StageReached(c, dev) || StageReached(c, prod) || c.Phase != PhaseLanded {
		t.Fatalf("%+v", c.Conditions)
	}
	ns := factNotices(Reduced{Before: &before, After: th}, false)
	if len(ns) != 1 || ns[0].Subject != "Deploy to dev (https://dev.example.com) on PR #7" {
		t.Fatalf("notices %+v", ns)
	}
	if len(ProbeWatches(th, ct0.Add(2*24*time.Hour))) != 1 {
		t.Fatal("a change on dev is no longer watched for prod on its second day")
	}
	ChangeDeployObserved(th, c.ID, DeployObs{Probe: true, Deployments: []Deployment{{Env: "prod", Production: true, State: "success"}}}, true, ct0.Add(20*time.Minute))
	if c.Phase != PhaseLive || !StageReached(c, prod) || len(ProbeWatches(th, ct0.Add(21*time.Minute))) != 0 {
		t.Fatalf("phase %s", c.Phase)
	}
	if ProbeScript("x {sha} {sha}", "abc1234", "") != "x abc1234 abc1234" || ProbeScript("x {sha}", "m1; rm -rf /", "") != "" ||
		ProbeScript("x {sha} {repo}", "abc1234", "acme/app") != "x abc1234 acme/app" || ProbeScript("x {sha} {repo}", "abc1234", "acme/app; rm -rf /") != "" || ProbeScript("x {sha} {repo}", "abc1234", "") != "" {
		t.Fatal("probe script")
	}
}

// V0.playbook-moments: what an agent writes on the owner's entry waits
// beside it; the owner takes it (a new probe waits for its yes) or drops it.
func TestPlaybookSuggestion(t *testing.T) {
	ps := map[string]*Project{"p1": {ID: "p1", Name: "Acme"}}
	if _, err := learn(t, ps, FromOwner, Args{"slot": "stage", "name": "dev", "how": "by hand", "probe": "old {sha}"}); err != nil {
		t.Fatal(err)
	}
	learn(t, ps, FromOwner, Args{"slot": "stage", "name": "dev", "allow": true})
	res, err := learn(t, ps, FromAgent, Args{"slot": "stage", "name": "dev", "how": "automatic on merge", "probe": "new {sha}", "thread": "t9"})
	e := ps["p1"].Playbook[0]
	if err != nil || res.Reply.(map[string]any)["suggested"] != true || e.How != "by hand" || !e.Runs() || e.Suggest == nil || e.Suggest.Probe != "new {sha}" {
		t.Fatalf("%v %+v", err, e)
	}
	if _, err := learn(t, ps, FromAgent, Args{"slot": "stage", "name": "dev", "take": true}); err == nil {
		t.Fatal("an agent took its own suggestion")
	}
	if _, err := learn(t, ps, FromOwner, Args{"slot": "stage", "name": "dev", "dismiss": true}); err != nil || ps["p1"].Playbook[0].Suggest != nil || ps["p1"].Playbook[0].How != "by hand" {
		t.Fatalf("dismiss: %v", err)
	}
	learn(t, ps, FromAgent, Args{"slot": "stage", "name": "dev", "how": "automatic on merge", "probe": "new {sha}", "thread": "t9"})
	if _, err := learn(t, ps, FromOwner, Args{"slot": "stage", "name": "dev", "take": true}); err != nil {
		t.Fatal(err)
	}
	e = ps["p1"].Playbook[0]
	if e.How != "automatic on merge" || e.Probe != "new {sha}" || e.ProbeOK || e.By != ByOwner || e.Suggest != nil {
		t.Fatalf("take: %+v", e)
	}
}

// V0.playbook-moments: a stage the owner checks asks once when the change
// gets there; the next stage's deploy command is a Run card only after the
// owner said it looks good; an entry written after the fact asks nothing.
func TestStageMoments(t *testing.T) {
	th, c := mergedThread(t)
	c.PR.MergeSHA = "abc1234"
	dev := PlaybookEntry{Slot: SlotStage, Name: "dev", OwnerChecks: true, URL: "https://dev.example.com", At: ct0}
	prod := PlaybookEntry{Slot: SlotStage, Name: "prod", Deploy: "release {sha}", At: ct0}
	stages := []PlaybookEntry{dev, prod}
	now := ct0.Add(5 * time.Minute)
	if ms := StageMoments(th, c, stages, now); len(ms) != 0 {
		t.Fatalf("before dev: %+v", ms)
	}
	// Without a check before it, the first stage's command is offered at the merge.
	if ms := StageMoments(th, c, []PlaybookEntry{prod}, now); len(ms) != 1 || ms[0].Command != "release abc1234" {
		t.Fatalf("first stage: %+v", ms)
	}
	ChangeDeployObserved(th, c.ID, DeployObs{Probe: true, Deployments: []Deployment{{Env: "dev", State: "success", URL: "https://dev.example.com"}}}, true, now)
	ms := StageMoments(th, c, stages, now)
	if len(ms) != 1 || ms[0].Ask == "" || ms[0].Stage != "dev" || ms[0].URL != "https://dev.example.com" {
		t.Fatalf("on dev: %+v", ms)
	}
	late := dev
	late.At = now.Add(time.Minute)
	if ms := StageMoments(th, c, []PlaybookEntry{late}, now.Add(2*time.Minute)); len(ms) != 0 {
		t.Fatalf("an entry written after the stage was reached asked: %+v", ms)
	}
	if ms := StageMoments(th, c, stages, now.Add(25*time.Hour)); len(ms) != 0 {
		t.Fatalf("a day later: %+v", ms)
	}
	in := Input{Source: Observer(ObsDeploy), Kind: KindStageMoment, Args: ms[0]}
	if _, err := reduceStageMoment(Rec{T: th}, in, now); err != nil {
		t.Fatal(err)
	}
	c = &th.Changes[0]
	if len(th.Decisions) != 1 || !strings.Contains(th.Decisions[0].Questions[0].Question, "PR #7 (Fix it) is on dev: https://dev.example.com") {
		t.Fatalf("%+v", th.Decisions)
	}
	reduceStageMoment(Rec{T: th}, in, now)
	if len(th.Decisions) != 1 || len(StageMoments(th, c, stages, now)) != 0 {
		t.Fatal("asked twice, or prod was offered before the answer")
	}
	d := th.Decisions[0]
	stageAnswered(th, d, map[string]string{d.Questions[0].Question: StageIsWrong})
	if len(StageMoments(th, c, stages, now)) != 0 {
		t.Fatal("prod was offered after Something is wrong")
	}
	stageAnswered(th, d, map[string]string{d.Questions[0].Question: StageLooksGood})
	ms = StageMoments(th, c, stages, now)
	if len(ms) != 1 || ms[0].Command != "release abc1234" || ms[0].Stage != "prod" {
		t.Fatalf("after Looks good: %+v", ms)
	}
	if _, err := reduceStageMoment(Rec{T: th}, Input{Kind: KindStageMoment, Args: ms[0]}, now); err != nil {
		t.Fatal(err)
	}
	if a := PendingRequest(th); a == nil || a.Command != "release abc1234" || !strings.Contains(a.Why, "Deploy PR #7 to prod") {
		t.Fatalf("run card: %+v", a)
	}
	if len(StageMoments(th, &th.Changes[0], stages, now)) != 0 {
		t.Fatal("offered twice")
	}
}

// V0.playbook-probe-health: what a probe did on this host (its last answer,
// the merges it missed) stays this host's when a link's playbook arrives,
// for the same command only; and a run is read as one of four answers.
func TestProbeAnswerIsTheHosts(t *testing.T) {
	mine := &ProbeResult{State: ProbeTimeout, Exit: -1, At: ct0}
	ps := map[string]*Project{"p1": {ID: "p1", Name: "Acme", Link: "l_0123456789abcdef", PlaybookAt: ct0, Playbook: []PlaybookEntry{
		{Slot: SlotStage, Name: "dev", Probe: "check {sha}", ProbeOK: true, Missed: 2, Last: mine, By: ByOwner}}}}
	send := func(min int, probe string) *Project {
		t.Helper()
		_, after, _, err := ReduceProject(ps, nil, Input{Source: FromOwner, Kind: KindProjectSharedPlaybook, CmdID: "c",
			Args: Args{"id": "p1", "link": "l_0123456789abcdef", "at": ct0.Add(time.Duration(min) * time.Minute).Format(time.RFC3339Nano),
				"playbook": []any{map[string]any{"slot": "stage", "name": "dev", "probe": probe, "probeOk": true, "by": "owner", "missed": 0,
					"last": map[string]any{"state": "reached", "at": ct0.Format(time.RFC3339Nano)}}}}}, ct0)
		if err != nil {
			t.Fatal(err)
		}
		return after
	}
	if e := send(1, "check {sha}").Playbook[0]; e.Missed != 2 || e.Last == nil || e.Last.State != ProbeTimeout || !e.Broken() {
		t.Fatalf("another host's answer replaced this host's: %+v %+v", e, e.Last)
	}
	if e := send(2, "other {sha}").Playbook[0]; e.Missed != 0 || e.Last != nil {
		t.Fatalf("another command kept the old one's answer: %+v", e)
	}
	for _, c := range []struct {
		exit     int
		timedOut bool
		want     string
	}{{0, false, ProbeReached}, {1, false, ProbeWaiting}, {127, false, ProbeError}, {126, false, ProbeError}, {-1, false, ProbeError}, {-1, true, ProbeTimeout}, {0, true, ProbeTimeout}} {
		if got := ProbeState(c.exit, c.timedOut); got != c.want {
			t.Errorf("ProbeState(%d, %v) = %s, want %s", c.exit, c.timedOut, got, c.want)
		}
	}
}
