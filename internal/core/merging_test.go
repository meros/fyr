package core

import (
	"testing"
	"time"
)

// V0.merge-in-flight: a merge that was started reads Waiting "Merging #88" in
// Moving without you until GitHub reports the PR merged; it read "Merge" in
// Your move from the press until the poller's next read.
func TestMergeInFlight(t *testing.T) {
	now := time.Date(2026, 11, 16, 13, 0, 0, 0, time.UTC)
	url := "https://github.com/acme/app/pull/88"
	start := func() (*Thread, Effect) {
		th := askMerge(t, url, now)
		th, _, _, err := Reduce(th, &View{}, greenPR(url, "abc1234def5678", now), now)
		if err != nil {
			t.Fatal(err)
		}
		th, _, res, err := Reduce(th, &View{}, autoIn("acme/app"), now)
		if err != nil || len(res.Effects) != 1 {
			t.Fatalf("the merge did not start: %v %+v", err, res.Effects)
		}
		return th, res.Effects[0]
	}
	result := func(th *Thread, ef Effect, ok bool, at time.Time) *Thread {
		th, _, _, err := Reduce(th, &View{}, Input{Source: FromEffect("fx1"), Kind: KindEffectResult, Thread: "t1",
			Args: EffectResult{Effect: ef, OK: ok, Text: "x"}}, at)
		if err != nil {
			t.Fatal(err)
		}
		return th
	}

	th, ef := start()
	c := &th.Changes[0]
	if !c.InMerge() || c.Merging.Head != "abc1234def5678" {
		t.Fatalf("no marker: %+v", c.Merging)
	}
	a := Compute(th, &View{}, now)
	if a.State != Waiting || a.Section != WorkingSec || a.Reason != "Merging #88" {
		t.Fatalf("started: %+v", a)
	}
	if n := ChangeNeed(th, c); n == nil || n.Cause != CauseMerging || !n.Muted {
		t.Fatalf("need: %+v", n)
	}
	if !WorkGoingAt(th, now) || NeedsOwnerAt(th, now) {
		t.Fatalf("going %v, needs %v", WorkGoingAt(th, now), NeedsOwnerAt(th, now))
	}

	// GitHub took the merge; the PR still reads open until the poller sees it.
	ok := result(th, ef, true, now.Add(time.Minute))
	if a := Compute(ok, &View{}, now.Add(time.Minute)); a.Reason != "Merging #88" {
		t.Fatalf("after an ok result: %+v", a)
	}
	// The poller reads it merged: the marker goes and the thread moves on.
	merged, _, _, err := Reduce(ok, &View{}, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Thread: "t1",
		Args: PR{URL: url, Repo: "acme/app", Number: 88, State: "MERGED", Head: "abc1234def5678", CheckedAt: &now}}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if merged.Changes[0].InMerge() || Compute(merged, &View{}, now.Add(2*time.Minute)).Reason == "Merging #88" {
		t.Fatalf("merged: %+v, %+v", merged.Changes[0].Merging, Compute(merged, &View{}, now.Add(2*time.Minute)))
	}

	// A refused merge ends the marker at once.
	th, ef = start()
	refused := result(th, ef, false, now.Add(time.Minute))
	if refused.Changes[0].Merging != nil {
		t.Fatalf("refused: %+v", refused.Changes[0].Merging)
	}
	if a := Compute(refused, &View{}, now.Add(time.Minute)); a.Reason == "Merging #88" {
		t.Fatalf("refused still reads merging: %+v", a)
	}

	// No news: the clock ends it after MergingFor, and NextCheck asks for that reduce.
	th, _ = start()
	end := now.Add(MergingFor)
	if at := NextCheck(th, &View{}, now); at == nil || at.After(end) {
		t.Fatalf("NextCheck %v, want at most %v", at, end)
	}
	th, _, _, err = Reduce(th, &View{}, Input{Source: Observer(ObsClock), Kind: KindClock, Thread: "t1"}, end)
	if err != nil {
		t.Fatal(err)
	}
	if th.Changes[0].Merging != nil {
		t.Fatalf("no news for MergingFor: %+v", th.Changes[0].Merging)
	}
}
