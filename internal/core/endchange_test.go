package core

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// prThread is a thread whose code change follows an open PR, with the
// built-in Merge it offers (the live case of V0.72: a fixture URL).
func prThread(t *testing.T, url string, now time.Time) *Thread {
	t.Helper()
	th := &Thread{ID: "t1"}
	th, _, _, err := Reduce(th, &View{}, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Thread: "t1",
		Args: PR{URL: url, Repo: "acme/app", Number: 88, State: "OPEN", Checks: ChecksNone, CheckedAt: &now}}, now)
	if err != nil {
		t.Fatal(err)
	}
	th.MainChange = th.Changes[0].ID
	if c := &th.Changes[0]; c.Phase != PhaseReview || !slices.ContainsFunc(c.Offers, func(a ActionSpec) bool { return a.ID == "fyr.merge" }) {
		t.Fatalf("setup: %+v", c)
	}
	return th
}

// V0.72 (1): GitHub answers that the PR does not exist: the change ends as
// abandoned (observed) with that note, nothing polls the URL, and the
// built-in Merge goes away.
func TestMissingPREndsTheChange(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	url := "https://github.com/acme/app/pull/88"
	th := prThread(t, url, now)
	later := now.Add(2 * time.Minute)
	th, _, _, err := Reduce(th, &View{}, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Thread: "t1",
		Args: PR{URL: url, Repo: "acme/app", Number: 88, State: PRMissing, CheckedAt: &later}}, later)
	if err != nil {
		t.Fatal(err)
	}
	c := &th.Changes[0]
	if c.Phase != PhaseAbandoned || c.PhaseBy != ByObserved || c.Label != MissingPRLabel {
		t.Fatalf("phase %s by %s, label %q", c.Phase, c.PhaseBy, c.Label)
	}
	if len(PRURLs(th, ct0)) != 0 {
		t.Fatalf("still polled: %v", PRURLs(th, ct0))
	}
	if len(c.Offers) != 0 {
		t.Fatalf("actions remain: %+v", c.Offers)
	}
	if why := Registry[FyrMerge].Available(th, &View{}, later); why == "" {
		t.Fatal("Merge is still available")
	}
	if c.Title != "acme/app#88" {
		t.Fatalf("title %q", c.Title)
	}
	// A link of the same URL later does not bring it back (it is no fact).
	PRObserved(th, PR{URL: url, Repo: "acme/app", Number: 88, State: "OPEN", Checks: ChecksPending}, later)
	if th.Changes[0].Phase != PhaseAbandoned {
		t.Fatalf("a link reopened it: %s", th.Changes[0].Phase)
	}
	if txt := NoticeText(th.Feed.Queue); !strings.Contains(txt, "does not exist on GitHub") {
		t.Fatalf("the agent is not told: %q", txt)
	}
}

// V0.72 (2): the owner ends an observed change; the agent cannot, and
// cannot move it again afterwards; a later fact does not move it either.
func TestOwnerEndsAnObservedChange(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	url := "https://github.com/acme/app/pull/88"
	th := prThread(t, url, now)
	id := th.Changes[0].ID
	end := func(src Source, args map[string]any) (*Thread, error) {
		x, _, _, err := Reduce(th, &View{}, Input{Source: src, Kind: KindChangeEnd, Thread: "t1", Args: args}, now)
		return x, err
	}
	if _, err := end(FromAgent, map[string]any{"change": id}); err == nil {
		t.Fatal("the agent ended a change with the owner's command")
	}
	if _, err := end(FromOwner, map[string]any{"change": id, "phase": "landed"}); err == nil {
		t.Fatal("ended as landed")
	}
	x, err := end(FromOwner, map[string]any{"change": id, "phase": "reverted", "reason": "a test fixture"})
	if err != nil {
		t.Fatal(err)
	}
	th = x
	c := &th.Changes[0]
	if c.Phase != PhaseReverted || c.PhaseBy != ByOwner || c.Label != "a test fixture" || len(c.Offers) != 0 {
		t.Fatalf("after the end: %s by %s, %q, %d offers", c.Phase, c.PhaseBy, c.Label, len(c.Offers))
	}
	if len(PRURLs(th, ct0)) != 0 {
		t.Fatal("still polled")
	}
	if txt := NoticeText(th.Feed.Queue); !strings.Contains(txt, "The owner ended") {
		t.Fatalf("the agent is not told: %q", txt)
	}
	if _, err := end(FromOwner, map[string]any{"change": id}); err == nil {
		t.Fatal("ended twice")
	}
	_, _, _, err = Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeUpdate, Thread: "t1",
		Args: ChangeUpdateArgs{Change: id, Update: ChangeUpdate{Phase: PhaseReview}}}, now)
	if err == nil || !strings.Contains(err.Error(), "owner ended") {
		t.Fatalf("the agent moved the owner's end: %v", err)
	}
	ChangePRObserved(th, PR{URL: url, Repo: "acme/app", Number: 88, State: "OPEN", Checks: ChecksPass, CheckedAt: &now}, now)
	if th.Changes[0].Phase != PhaseReverted {
		t.Fatalf("a fact moved the owner's end: %s", th.Changes[0].Phase)
	}
}

// V0.72 (2): the default end is abandoned, and the thread offers the
// command only while a change has not ended.
func TestOwnerEndDefaultsToAbandoned(t *testing.T) {
	now := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
	th := &Thread{ID: "t1"}
	if why := Registry[KindChangeEnd].Available(th, &View{}, now); why == "" {
		t.Fatal("available without a change")
	}
	OpenChange(th, ChangeOpen{Key: "k8s:prod/api", Kind: KindInfra, Title: "Replicas"}, ByAgent, now)
	if why := Registry[KindChangeEnd].Available(th, &View{}, now); why != "" {
		t.Fatal(why)
	}
	th, _, _, err := Reduce(th, &View{}, Input{Source: FromOwner, Kind: KindChangeEnd, Thread: "t1", Args: map[string]any{"change": "k8s:prod/api"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if c := th.Changes[0]; c.Phase != PhaseAbandoned || c.PhaseBy != ByOwner || c.Label != "" {
		t.Fatalf("%+v", c)
	}
}
