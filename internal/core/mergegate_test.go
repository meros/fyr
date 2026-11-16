package core

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// V0.112: a merge waits for CI. The owner's yes on a merge ask and the
// owner's press of Merge do not run while Fyr reads the PR's checks as
// pending (running, or none reported yet on a repo with CI). The agent may
// still ask; the ask stays open, and its card says why Merge waits.

const gateHead = "029f9c7640b795c9e9edcd8e875f6490e753b593"

func gatePR(url string, checks Checks, at time.Time) Input {
	return Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Thread: "t1",
		Args: PR{URL: url, Repo: "meros/fyr", Number: 126, State: "OPEN", Checks: checks, MergeState: "CLEAN", Head: gateHead, CheckedAt: &at}}
}

// pendingAsk is #126 at 16:05:22: the PR is open and clean, CI has not
// reported on its head, and the agent asked for the merge.
func pendingAsk(t *testing.T, now time.Time) *Thread {
	t.Helper()
	url := "https://github.com/meros/fyr/pull/126"
	th := prThread(t, url, now)
	th, _, _, err := Reduce(th, &View{}, gatePR(url, ChecksPending, now), now)
	if err != nil {
		t.Fatal(err)
	}
	th, _, _, err = Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeRequest, Thread: "t1",
		Args: ChangeRequest{ID: "d_merge", Change: th.Changes[0].ID, Ask: AskMerge}}, now)
	if err != nil {
		t.Fatalf("the agent may ask for a merge while CI runs: %v", err)
	}
	if len(th.Decisions) != 1 {
		t.Fatalf("setup: decisions %+v", th.Decisions)
	}
	return th
}

func TestOwnerMergeWaitsForPendingChecks(t *testing.T) {
	now := time.Date(2026, 11, 16, 15, 5, 22, 0, time.UTC)
	th := pendingAsk(t, now)
	yes := Input{Source: FromOwner, Kind: "thread.answer", Thread: "t1", Args: Args{"decisionId": "d_merge", "allow": true}}
	_, _, _, err := Reduce(th, &View{}, yes, now.Add(8*time.Second))
	var rej *Rejection
	if err == nil || !errors.As(err, &rej) || !strings.Contains(err.Error(), "CI has not passed on 029f9c7") {
		t.Fatalf("the owner's yes ran a merge on pending checks: %v", err)
	}
	if len(th.Decisions) != 1 {
		t.Fatal("a refused yes closed the ask")
	}
	n := ChangeNeed(th, &th.Changes[0])
	if n == nil || n.Cause != CauseAsk || !strings.Contains(n.Wait, "CI has not passed") {
		t.Fatalf("a merge ask on pending checks does not say why it waits: %+v", n)
	}
	// the press of the change's Merge is refused the same way
	c := &th.Changes[0]
	var merge *ActionSpec
	for i := range c.Offers {
		if c.Offers[i].ID == "fyr.merge" {
			merge = &c.Offers[i]
		}
	}
	if merge == nil {
		t.Fatal("no Merge offer")
	}
	press := Input{Source: FromOwner, Kind: KindActionPress, Thread: "t1", Args: Args{"on": c.ID, "action": "fyr.merge", "digest": merge.Digest, "head": gateHead}}
	if _, _, _, err := Reduce(th, &View{}, press, now.Add(9*time.Second)); err == nil || !strings.Contains(err.Error(), "CI has not passed") {
		t.Fatalf("the owner's press merged on pending checks: %v", err)
	}

	// the checks pass: the ask is in Needs you again and the yes runs the merge
	later := now.Add(4 * time.Minute)
	th, _, _, err = Reduce(th, &View{}, gatePR("https://github.com/meros/fyr/pull/126", ChecksPass, later), later)
	if err != nil {
		t.Fatal(err)
	}
	if n := ChangeNeed(th, &th.Changes[0]); n == nil || n.Wait != "" {
		t.Fatalf("a green merge ask still waits: %+v", n)
	}
	_, _, res, err := Reduce(th, &View{}, yes, later)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Effects) != 1 || res.Effects[0].Kind != EffectRun || res.Effects[0].Request.Action != FyrMerge {
		t.Fatalf("effects %+v", res.Effects)
	}
}

// A repo with no CI (the poller reads none: no check ever showed on its
// PRs) does not wait; a failed check keeps V0.43's rule (the owner may merge
// a PR whose failing checks GitHub does not require).
func TestOwnerMergeOnNoCIOrFailedChecks(t *testing.T) {
	for _, checks := range []Checks{ChecksNone, ChecksFail} {
		now := time.Date(2026, 11, 16, 15, 5, 22, 0, time.UTC)
		th := pendingAsk(t, now)
		th, _, _, err := Reduce(th, &View{}, gatePR("https://github.com/meros/fyr/pull/126", checks, now), now)
		if err != nil {
			t.Fatal(err)
		}
		yes := Input{Source: FromOwner, Kind: "thread.answer", Thread: "t1", Args: Args{"decisionId": "d_merge", "allow": true}}
		if _, _, _, err := Reduce(th, &View{}, yes, now); err != nil {
			t.Errorf("checks %s: %v", checks, err)
		}
	}
}
