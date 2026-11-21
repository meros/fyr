package core

import (
	"testing"
	"time"
)

// V0.multi-pr: a thread or change that results in many PRs tracks every PR:
// each PR gets its own code change, can be targeted by pr.merge,
// pr.rerunFailed and thread.dismissPR, and dismissing the main PR promotes
// another non-dismissed PR.
func TestMultiPRChangeOpenAndEvidence(t *testing.T) {
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	th := &Thread{ID: "t1"}
	v := &View{}

	// Two PRs opened on the same branch name in different repos do not collide.
	r1, _, _, err := Reduce(th, v, Input{Source: FromAgent, Kind: KindChangeOpen, Args: ChangeOpenArgs{
		Open: ChangeOpen{
			Key: "api-v2", Kind: KindCode, Title: "API v2", Branch: "feat/v2",
			Targets:  []string{"repo:github.com/acme/api"},
			Evidence: []Evidence{{Kind: "pr", URL: "https://github.com/acme/api/pull/10", Title: "#10"}},
		},
		PR: "https://github.com/acme/api/pull/10",
	}}, now)
	if err != nil {
		t.Fatal(err)
	}
	r2, _, _, err := Reduce(r1, v, Input{Source: FromAgent, Kind: KindChangeOpen, Args: ChangeOpenArgs{
		Open: ChangeOpen{
			Key: "web-v2", Kind: KindCode, Title: "Web v2", Branch: "feat/v2",
			Targets:  []string{"repo:github.com/acme/web"},
			Evidence: []Evidence{{Kind: "pr", URL: "https://github.com/acme/web/pull/20", Title: "#20"}},
		},
		PR: "https://github.com/acme/web/pull/20",
	}}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Changes) != 2 {
		t.Fatalf("want 2 changes, got %d: %+v", len(r2.Changes), r2.Changes)
	}
	if r2.Changes[1].Phase != PhaseReview {
		t.Fatalf("second PR change phase %q, want review", r2.Changes[1].Phase)
	}
	if len(PRURLs(r2, now.Add(time.Minute))) != 2 {
		t.Fatalf("poller URLs %v, want 2", PRURLs(r2, now.Add(time.Minute)))
	}

	// Attaching a third PR as evidence on an existing change also tracks it.
	r3, _, _, err := Reduce(r2, v, Input{Source: FromAgent, Kind: KindChangeUpdate, Args: ChangeUpdateArgs{
		Change:   "api-v2",
		Evidence: []Evidence{{Kind: "pr", URL: "https://github.com/acme/docs/pull/30", Title: "#30"}},
	}}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(PRURLs(r3, now.Add(2*time.Minute))) != 3 {
		t.Fatalf("poller URLs after evidence %v, want 3", PRURLs(r3, now.Add(2*time.Minute)))
	}
	if c := r3.PRChange("https://github.com/acme/docs/pull/30"); c == nil || c.PR.Number != 30 {
		t.Fatalf("PRChange for 3rd PR: %+v", c)
	}
}

// V0.multi-pr: DigestArgs.PRs links every PR printed by gh pr create.
func TestDigestMultiplePRs(t *testing.T) {
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	th := &Thread{ID: "t1"}
	v := &View{}
	next, _, _, err := Reduce(th, v, Input{Source: Observer(ObsProcess), Kind: KindDigest, Args: DigestArgs{
		PRs: []PR{
			{URL: "https://github.com/acme/a/pull/1", Repo: "acme/a", Number: 1, State: "OPEN", Checks: ChecksPending},
			{URL: "https://github.com/acme/b/pull/2", Repo: "acme/b", Number: 2, State: "OPEN", Checks: ChecksPending},
		},
	}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if urls := PRURLs(next, now); len(urls) != 2 {
		t.Fatalf("want 2 PR URLs, got %v", urls)
	}
}

// V0.multi-pr: pr.merge, pr.rerunFailed and thread.dismissPR act on a chosen
// PR; dismissing the main PR promotes another active PR.
func TestMultiPROwnerActions(t *testing.T) {
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	th := &Thread{ID: "t1"}
	v := &View{}
	u1 := "https://github.com/acme/api/pull/11"
	u2 := "https://github.com/acme/web/pull/22"
	PRObserved(th, PR{URL: u1, Repo: "acme/api", Number: 11, State: "OPEN", Checks: ChecksPass, Head: "1111111"}, now)
	PRObserved(th, PR{URL: u2, Repo: "acme/web", Number: 22, State: "OPEN", Checks: ChecksFail, Conflict: true, Head: "2222222"}, now.Add(time.Minute))

	// Main PR (#22) has conflicts, but #11 is mergeable: pr.merge is available
	// and can merge #11 explicitly while refusing #22.
	merge := Registry[FyrMerge]
	if why := merge.Available(th, v, now.Add(2*time.Minute)); why != "" {
		t.Fatalf("pr.merge should be available via #11, got %q", why)
	}
	effs, err := merge.Apply(th, v, Args{"pr": u1, "method": "squash", "head": "1111111"}, now.Add(2*time.Minute))
	if err != nil || len(effs) != 1 || effs[0].Request.Args["pr"] != u1 {
		t.Fatalf("merge #11: effs=%+v err=%v", effs, err)
	}
	if _, err := merge.Apply(th, v, Args{"pr": u2, "method": "squash", "head": "2222222"}, now.Add(2*time.Minute)); err == nil {
		t.Fatal("merge #22 with conflicts succeeded")
	}
	if _, err := merge.Apply(th, v, Args{"pr": "https://github.com/acme/other/pull/99"}, now.Add(2*time.Minute)); err == nil {
		t.Fatal("merge unknown PR succeeded")
	}

	// Re-run failed can target #22 specifically.
	rerun := Registry[FyrRerun]
	effs, err = rerun.Apply(th, v, Args{"pr": u2}, now.Add(2*time.Minute))
	if err != nil || len(effs) != 1 || effs[0].Request.Args["pr"] != u2 {
		t.Fatalf("rerun #22: effs=%+v err=%v", effs, err)
	}

	// #22 merges while #11 stays open: dismissPR is available, dismisses #22,
	// and promotes #11 to MainPR.
	ChangePRObserved(th, PR{URL: u2, Repo: "acme/web", Number: 22, State: "MERGED", Checks: ChecksPass}, now.Add(3*time.Minute))
	dismiss := Registry["thread.dismissPR"]
	if why := dismiss.Available(th, v, now.Add(3*time.Minute)); why != "" {
		t.Fatalf("dismissPR should be available for merged #22: %q", why)
	}
	if _, err := dismiss.Apply(th, v, Args{"pr": u1}, now.Add(3*time.Minute)); err == nil {
		t.Fatal("dismissing open #11 succeeded")
	}
	if _, err := dismiss.Apply(th, v, Args{"pr": u2}, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("dismissing merged #22 failed: %v", err)
	}
	if p := th.MainPR(); p == nil || p.URL != u1 {
		t.Fatalf("after dismissing #22, MainPR = %+v, want #11", p)
	}
	if why := dismiss.Available(th, v, now.Add(4*time.Minute)); why == "" {
		t.Fatal("dismissPR should be unavailable when only open #11 remains")
	}
}
