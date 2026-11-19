package core

import (
	"strings"
	"testing"
	"time"
)

// V0.111: Fyr approves an agent's deploy ask itself when the engine's gate
// passed the checkout. The approval is an observer's Input; the agent and a
// client cannot send it, and the setting is the owner's alone.

const deployCommit = "c0ffee1234567890c0ffee1234567890c0ffee12"

func askDeploy(t *testing.T, now time.Time) *Thread {
	t.Helper()
	th := prThread(t, "https://github.com/acme/app/pull/88", now)
	// V0.changes-code-only (5): a Fyr deploy is asked with effect_request.
	th, _, _, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindEffectRequest, Thread: "t1",
		Args: EffectRequest{ID: "d_deploy", Action: FyrDeploy, Args: map[string]any{"dir": "/src/fyr"}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

func autoDeployIn(a AutoDeployArgs) Input {
	a.Decision = "d_deploy"
	return Input{Source: Observer(ObsHost), Kind: KindAutoDeploy, Thread: "t1", Args: a}
}

func TestAutoDeployApprovesTheAskAndBindsTheCommit(t *testing.T) {
	now := time.Date(2026, 11, 16, 13, 0, 0, 0, time.UTC)
	th := askDeploy(t, now)
	asks := AutoDeployAsks(th)
	if len(asks) != 1 || asks[0].Decision != "d_deploy" || asks[0].Dir != "/src/fyr" {
		t.Fatalf("asks %+v", asks)
	}
	th, _, res, err := Reduce(th, &View{}, autoDeployIn(AutoDeployArgs{Commit: deployCommit}), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(th.Decisions) != 0 {
		t.Fatalf("the ask is still open: %+v", th.Decisions)
	}
	if len(res.Effects) != 1 || res.Effects[0].Request == nil {
		t.Fatalf("effects %+v", res.Effects)
	}
	req := res.Effects[0].Request
	if req.Action != FyrDeploy || req.Args["commit"] != deployCommit || req.Args["autoDeploy"] != true || req.Args["dir"] != "/src/fyr" {
		t.Fatalf("ran %+v", req)
	}
	d := closedEvent(res, "d_deploy")
	if a, _ := d["answer"].(string); !strings.Contains(a, "Auto-approved") || !strings.Contains(a, "c0ffee1") {
		t.Fatalf("answer %q", a)
	}
	// V0.changes-code-only (5): an effect_request deploy names no change,
	// so no change carries the approval; the answer above says it.
	if c := th.Changes[0]; c.AutoApproved != nil {
		t.Fatalf("a change the deploy did not name says it: %+v", c.AutoApproved)
	}
	var out Result
	report(&out, *req, true, false, "tests failed, nothing was switched")
	line, _ := out.Events[0].Data.(map[string]any)["text"].(string)
	if !strings.HasPrefix(line, "Auto-approved by the auto-deploy setting (c0ffee1).") || !strings.Contains(line, "tests failed") {
		t.Fatalf("result line %q", line)
	}
	if len(AutoDeployAsks(th)) != 0 {
		t.Fatal("a closed ask is offered again")
	}
}

func TestAutoDeployHeldAsk(t *testing.T) {
	now := time.Date(2026, 11, 16, 13, 0, 0, 0, time.UTC)
	th := askDeploy(t, now)
	for _, a := range []AutoDeployArgs{{Held: "the checkout has uncommitted changes"}, {Wait: true}} {
		th2, _, res, err := Reduce(th, &View{}, autoDeployIn(a), now)
		if err != nil {
			t.Fatal(err)
		}
		if len(th2.Decisions) != 1 || len(res.Effects) != 0 {
			t.Fatalf("%+v: the ask closed or ran", a)
		}
		if len(res.Events) != 1 || res.Events[0].Type != "effect.error" {
			t.Fatalf("%+v: events %+v", a, res.Events)
		}
		m := res.Events[0].Data.(map[string]any)
		text, _ := m["error"].(string)
		if m["kind"] != "autoDeploy" || (a.Held != "" && !strings.Contains(text, "uncommitted")) || (a.Wait && !strings.Contains(text, "Another Fyr deploy runs")) {
			t.Fatalf("%+v: %q", a, text)
		}
	}
	// No commit and no reason is not an approval.
	if _, _, _, err := Reduce(th, &View{}, autoDeployIn(AutoDeployArgs{}), now); err == nil {
		t.Fatal("approved with no commit")
	}
}

func TestOnlyAnObserverAutoDeploys(t *testing.T) {
	now := time.Date(2026, 11, 16, 13, 0, 0, 0, time.UTC)
	th := askDeploy(t, now)
	for _, src := range []Source{FromAgent, FromOwner} {
		in := autoDeployIn(AutoDeployArgs{Commit: deployCommit})
		in.Source = src
		if _, _, _, err := Reduce(th, &View{}, in, now); err == nil {
			t.Fatalf("%s sent the auto-approval", src)
		}
	}
	// A merge ask is not a deploy ask.
	mt := askMerge(t, "https://github.com/acme/app/pull/88", now)
	in := autoDeployIn(AutoDeployArgs{Commit: deployCommit})
	in.Args = AutoDeployArgs{Decision: "d_merge", Commit: deployCommit}
	if _, _, _, err := Reduce(mt, &View{}, in, now); err == nil {
		t.Fatal("a merge ask was auto-deployed")
	}
}

func TestAutoDeploySettingIsTheOwners(t *testing.T) {
	now := time.Date(2026, 11, 16, 13, 0, 0, 0, time.UTC)
	if k := Kinds[KindAutoDeploySet]; k == nil || len(k.From) != 1 || k.From[0] != ByOwner {
		t.Fatalf("the setting is not the owner's alone: %+v", k)
	}
	if _, err := ReduceHost(Input{Source: FromAgent, Kind: KindAutoDeploySet, Args: Args{"on": false}}, now); err == nil {
		t.Fatal("an agent changed the setting")
	}
	if _, err := ReduceHost(Input{Source: FromOwner, Kind: KindAutoDeploySet, Args: Args{"on": "yes"}}, now); err == nil {
		t.Fatal("on that is not a bool was accepted")
	}
	res, err := ReduceHost(Input{Source: FromOwner, Kind: KindAutoDeploySet, Args: Args{"on": false}}, now)
	if err != nil || len(res.Effects) != 1 || res.Effects[0].Kind != EffectAutoDeploySet || res.Effects[0].Request.Args["on"] != false {
		t.Fatalf("res %+v %v", res, err)
	}
}
