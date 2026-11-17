package core

import (
	"strings"
	"testing"
	"time"
)

// V0.112: a merge waits for CI. The owner's Merge on the PR tab does not run
// while Fyr reads the PR's checks as pending (running, or none reported
// yet on a repo with CI). Since V0.136 an agent's merge ask on pending
// checks is held and never shown, so the owner's yes cannot meet one.

const gateHead = "029f9c7640b795c9e9edcd8e875f6490e753b593"
const gateURL = "https://github.com/meros/fyr/pull/126"

func gatePR(url string, checks Checks, at time.Time) Input {
	return Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Thread: "t1",
		Args: PR{URL: url, Repo: "meros/fyr", Number: 126, State: "OPEN", Checks: checks, MergeState: "CLEAN", Head: gateHead, CheckedAt: &at}}
}

func TestOwnerPressWaitsForPendingChecks(t *testing.T) {
	now := time.Date(2026, 11, 16, 15, 5, 22, 0, time.UTC)
	th := prThread(t, gateURL, now)
	th, _, _, err := Reduce(th, &View{}, gatePR(gateURL, ChecksPending, now), now)
	if err != nil {
		t.Fatal(err)
	}
	// the PR tab's Merge… (V0.43); since V0.136 the card has no Merge while CI runs
	press := func(th *Thread, at time.Time) error {
		_, _, _, err := Reduce(th, &View{}, Input{Source: FromOwner, Kind: FyrMerge, Thread: "t1",
			Args: Args{"method": "squash", "head": gateHead}}, at)
		return err
	}
	if err := press(th, now.Add(9*time.Second)); err == nil || !strings.Contains(err.Error(), "CI has not passed on 029f9c7") {
		t.Fatalf("the owner's press merged on pending checks: %v", err)
	}
	later := now.Add(4 * time.Minute)
	th, _, _, err = Reduce(th, &View{}, gatePR(gateURL, ChecksPass, later), later)
	if err != nil {
		t.Fatal(err)
	}
	if err := press(th, later); err != nil {
		t.Fatalf("green: %v", err)
	}
}

// A repo with no CI (the poller reads none: no check ever showed on its
// PRs) does not wait: the agent's ask opens at once and the yes runs.
func TestOwnerMergeOnNoCI(t *testing.T) {
	now := time.Date(2026, 11, 16, 15, 5, 22, 0, time.UTC)
	th := prThread(t, gateURL, now)
	th, _, _, err := Reduce(th, &View{}, gatePR(gateURL, ChecksNone, now), now)
	if err != nil {
		t.Fatal(err)
	}
	th, _, _, err = Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeRequest, Thread: "t1",
		Args: ChangeRequest{ID: "d_merge", Change: th.Changes[0].ID, Ask: AskMerge}}, now)
	if err != nil || len(th.Decisions) != 1 {
		t.Fatalf("ask: %v %+v", err, th.Decisions)
	}
	yes := Input{Source: FromOwner, Kind: "thread.answer", Thread: "t1", Args: Args{"decisionId": "d_merge", "allow": true}}
	if _, _, _, err := Reduce(th, &View{}, yes, now); err != nil {
		t.Fatal(err)
	}
}
