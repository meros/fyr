package core

import (
	"strings"
	"testing"
	"time"
)

// V0.review-thread-notice: unresolved review threads fail the review
// condition whatever the review decision, tell the agent with the count
// and wake it, fire a review fail watch, and clear when resolved.
func TestReviewThreadsNotice(t *testing.T) {
	reviewOf := func(th *Thread) *Condition { return condition(&th.Changes[0], CondReview) }
	key := func(th *Thread) string { return "cond:" + th.Changes[0].ID + ":review" }

	t.Run("commented review with threads fails and notifies", func(t *testing.T) {
		th, res := observePR(t, feedThread(), PR{State: "OPEN", Checks: ChecksPass, Head: "h1", UnresolvedThreads: 2}, ct0)
		k := reviewOf(th)
		if k == nil || k.Status != CondFail || k.Reason != "2 unresolved review threads" {
			t.Fatalf("review condition: %+v", k)
		}
		n := queued(t, th, key(th))
		if n.Urgency != Wake || n.What != "Review on PR #7: 2 unresolved review threads." || !wakes(res) {
			t.Fatalf("notice: %+v wakes=%v", n, wakes(res))
		}
		if b := th.Changes[0].Blocking(); b != "2 unresolved review threads" {
			t.Fatalf("blocking = %q", b)
		}
	})

	t.Run("one thread is singular", func(t *testing.T) {
		th, _ := observePR(t, feedThread(), PR{State: "OPEN", Checks: ChecksPass, Head: "h1", UnresolvedThreads: 1}, ct0)
		if k := reviewOf(th); k == nil || k.Reason != "1 unresolved review thread" {
			t.Fatalf("review condition: %+v", k)
		}
	})

	t.Run("resolved clears, a new thread on the same head fires again", func(t *testing.T) {
		th, _ := observePR(t, feedThread(), PR{State: "OPEN", Checks: ChecksPass, Head: "h1", UnresolvedThreads: 2}, ct0)
		th.Feed.Queue = nil
		th, _ = observePR(t, th, PR{State: "OPEN", Checks: ChecksPass, Head: "h1", UnresolvedThreads: 0}, ct0.Add(time.Minute))
		if k := reviewOf(th); k != nil {
			t.Fatalf("resolved threads left %+v", k)
		}
		th, _ = observePR(t, th, PR{State: "OPEN", Checks: ChecksPass, Head: "h1", Review: "APPROVED", UnresolvedThreads: 0}, ct0.Add(2*time.Minute))
		if k := reviewOf(th); k == nil || k.Status != CondOK {
			t.Fatalf("approved after resolving: %+v", k)
		}
		th.Feed.Queue = nil
		th, res := observePR(t, th, PR{State: "OPEN", Checks: ChecksPass, Head: "h1", Review: "APPROVED", UnresolvedThreads: 2}, ct0.Add(3*time.Minute))
		if n := queued(t, th, key(th)); n.What != "Review on PR #7: 2 unresolved review threads." || !wakes(res) {
			t.Fatalf("new threads: %+v", n)
		}
	})

	t.Run("a read that cannot see threads keeps the count", func(t *testing.T) {
		th, _ := observePR(t, feedThread(), PR{State: "OPEN", Checks: ChecksPass, Head: "h1", UnresolvedThreads: 2}, ct0)
		th, _ = observePR(t, th, PR{State: "OPEN", Checks: ChecksPass, Head: "h1", UnresolvedThreads: -1}, ct0.Add(time.Minute))
		if k := reviewOf(th); k == nil || k.Status != CondFail || th.Changes[0].PR.UnresolvedThreads != 2 {
			t.Fatalf("unread threads changed the condition: %+v %+v", k, th.Changes[0].PR)
		}
	})

	t.Run("changes requested still fails and names itself", func(t *testing.T) {
		th, _ := observePR(t, feedThread(), PR{State: "OPEN", Checks: ChecksPass, Head: "h1", Review: "CHANGES_REQUESTED", UnresolvedThreads: 3}, ct0)
		if k := reviewOf(th); k == nil || k.Status != CondFail || k.Reason != "Changes requested" {
			t.Fatalf("review condition: %+v", k)
		}
		if n := queued(t, th, key(th)); n.What != "Review on PR #7: changes requested." {
			t.Fatalf("notice: %+v", n)
		}
		if why := prGuard(true)(th, map[string]any{"pr": fprURL}); why != "Changes requested" {
			t.Fatalf("merge guard = %q", why)
		}
	})

	t.Run("threads alone do not stop the owner's merge and the card offers to address them", func(t *testing.T) {
		th, _ := observePR(t, feedThread(), PR{State: "OPEN", Checks: ChecksPass, Head: "h1", UnresolvedThreads: 2}, ct0)
		if why := prGuard(true)(th, map[string]any{"pr": fprURL}); why == "Changes requested" {
			t.Fatalf("merge guard refused for threads: %q", why)
		}
		found := false
		for _, a := range th.Changes[0].Offers {
			if a.ID == "fyr.review" {
				found = strings.Contains(a.Prompt, "has 2 unresolved review threads.")
			}
		}
		if !found {
			t.Fatal("no Address review offer naming the threads")
		}
	})
}

// V0.review-thread-notice: a {type: review, status: fail} watch fires when
// a COMMENTED review leaves threads, with an empty review decision.
func TestReviewThreadsFireWatch(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	const url = "https://github.com/o/r/pull/12"
	step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: url, State: "OPEN", Checks: ChecksPass}}, ct0)
	c := th.Changes[0]
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_1", Then: "notify",
		When: When{Kind: WhenCondition, Change: c.Key, Type: CondReview, Status: CondFail}}}, ct0)
	res := step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: url, State: "OPEN", Checks: ChecksPass, UnresolvedThreads: 2}}, ct0.Add(time.Minute))
	if fired(res, "w_1") != 1 {
		t.Fatalf("review fail watch did not fire: %+v", res.Events)
	}
}
