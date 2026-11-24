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
	if _, err := learn(t, ps, FromAgent, Args{"slot": "issue", "how": "Linear"}); err == nil || !strings.Contains(err.Error(), "the owner wrote the issue entry") {
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
	if ProbeScript("x {sha} {sha}", "abc1234") != "x abc1234 abc1234" || ProbeScript("x {sha}", "m1; rm -rf /") != "" {
		t.Fatal("probe script")
	}
}
