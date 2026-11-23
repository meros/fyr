// V0.thread-invariants: unit tests for every pure thread state & transition
// invariant, regression tests for the bugs they uncovered, and a randomized
// state-machine fuzz test across owner commands, agent tools, observers, and
// clock ticks.
package core

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"slices"
	"testing"
	"time"
)

func hasViolation(vs []Violation, id string) bool {
	return slices.ContainsFunc(vs, func(v Violation) bool { return v.ID == id })
}

func TestThreadInvariantsPureStateViolations(t *testing.T) {
	now := t0.Add(10 * time.Minute)
	later := now.Add(time.Hour)

	t.Run("archived-live-work", func(t *testing.T) {
		th := &Thread{ID: "t1", ArchivedAt: ptr(t0), Conversation: Conversation{Running: true, RunStartedAt: ptr(t0)}}
		vs := CheckThread(th, &View{}, now, nil)
		if !hasViolation(vs, InvThreadArchivedLiveWork) {
			t.Fatalf("expected %s, got %+v", InvThreadArchivedLiveWork, vs)
		}
		// Armed rule on archived thread.
		th2 := &Thread{ID: "t2", ArchivedAt: ptr(t0), Rules: []Rule{{ID: "w1", Purpose: PurposeWatch, When: When{Kind: WhenAt, At: ptr(later)}, Then: Then{Kind: KindRuleNotify}, Status: RuleArmed}}}
		if vs := CheckThread(th2, &View{}, now, nil); !hasViolation(vs, InvThreadArchivedLiveWork) {
			t.Fatalf("expected %s for armed rule on archived thread, got %+v", InvThreadArchivedLiveWork, vs)
		}
		// Clean archived thread has zero violations.
		clean := &Thread{ID: "t3", ArchivedAt: ptr(t0)}
		if vs := CheckThread(clean, &View{}, now, nil); len(vs) != 0 {
			t.Fatalf("clean archived thread had violations: %+v", vs)
		}
	})

	t.Run("settled-contradiction", func(t *testing.T) {
		th := &Thread{ID: "t1", Settle: Settle{At: ptr(t0)}, PinnedAt: ptr(t0), SettleAsk: &SettleAsk{At: t0, Why: "done"}}
		v := snoozed(View{}, later, t0)
		vs := CheckThread(th, &v, now, nil)
		if !hasViolation(vs, InvThreadSettledContradict) {
			t.Fatalf("expected %s, got %+v", InvThreadSettledContradict, vs)
		}
		clean := &Thread{ID: "t2", Settle: Settle{At: ptr(t0)}}
		if vs := CheckThread(clean, &View{}, now, nil); len(vs) != 0 {
			t.Fatalf("clean settled thread had violations: %+v", vs)
		}
	})

	t.Run("run-state-contradiction", func(t *testing.T) {
		cases := []struct {
			name string
			th   Thread
		}{
			{"running without runStartedAt", Thread{ID: "t", Conversation: Conversation{Running: true}}},
			{"not running with runStartedAt", Thread{ID: "t", Conversation: Conversation{RunStartedAt: ptr(t0)}}},
			{"not running with activity", Thread{ID: "t", Conversation: Conversation{Activity: "Reading file"}}},
			{"running and starting", Thread{ID: "t", Conversation: Conversation{Running: true, RunStartedAt: ptr(t0), Starting: ptr(t0)}}},
			{"running and failure", Thread{ID: "t", Conversation: Conversation{Running: true, RunStartedAt: ptr(t0), Failure: &Failure{Class: "api", Message: "err", At: t0}}}},
			{"starting and failure", Thread{ID: "t", Conversation: Conversation{Starting: ptr(t0), Failure: &Failure{Class: "api", Message: "err", At: t0}}}},
			{"running and stopped", Thread{ID: "t", Conversation: Conversation{Running: true, RunStartedAt: ptr(t0), Stopped: true}}},
		}
		for _, tc := range cases {
			if vs := CheckThread(&tc.th, &View{}, now, nil); !hasViolation(vs, InvThreadRunStateContradict) {
				t.Errorf("%s: expected %s, got %+v", tc.name, InvThreadRunStateContradict, vs)
			}
		}
	})

	t.Run("pin-contradiction", func(t *testing.T) {
		th := &Thread{ID: "t1", PinnedAt: ptr(t0)}
		v := snoozed(View{}, later, t0)
		if vs := CheckThread(th, &v, now, nil); !hasViolation(vs, InvThreadPinContradict) {
			t.Fatalf("expected %s for pinned+snoozed, got %+v", InvThreadPinContradict, vs)
		}
		// Auto-settle rule not suspended by pin.
		th2 := &Thread{ID: "t2", PinnedAt: ptr(t0), Rules: []Rule{{ID: settleDoneRule, Purpose: PurposeSettle, When: When{Kind: WhenWorkDone}, Then: Then{Kind: KindRuleSettle}, CreatedBy: ByAgent, Status: RuleArmed}}}
		if vs := CheckThread(th2, &View{}, now, nil); !hasViolation(vs, InvThreadPinContradict) {
			t.Fatalf("expected %s for unsuspended auto-settle on pinned thread, got %+v", InvThreadPinContradict, vs)
		}
	})

	t.Run("rule-invalid", func(t *testing.T) {
		// I3: status=suspended with empty SuspendedBy.
		th := &Thread{ID: "t1", Rules: []Rule{{ID: "r1", Purpose: PurposeWatch, Then: Then{Kind: KindRuleNotify}, Status: RuleSuspended}}}
		if vs := CheckThread(th, &View{}, now, nil); !hasViolation(vs, InvThreadRuleInvalid) {
			t.Fatalf("expected %s for I3 mismatch, got %+v", InvThreadRuleInvalid, vs)
		}
		// Orphan change ref.
		th2 := &Thread{ID: "t2", Rules: []Rule{{ID: "r2", Purpose: PurposeWatch, About: "c_missing", Then: Then{Kind: KindRuleNotify}, Status: RuleArmed}}}
		if vs := CheckThread(th2, &View{}, now, nil); !hasViolation(vs, InvThreadRuleInvalid) {
			t.Fatalf("expected %s for orphan rule, got %+v", InvThreadRuleInvalid, vs)
		}
		// Unwatchable PR condition on landed change.
		th3 := &Thread{ID: "t3",
			Changes: []Change{{ID: "c1", Key: "k1", Kind: KindCode, Phase: PhaseLanded, PhaseBy: ByObserved, LandedAt: ptr(t0), Done: &DoneMark{At: t0, By: ByObserved, Reason: "merged"}}},
			Rules:   []Rule{{ID: "w1", Purpose: PurposeWatch, About: "c1", When: When{Kind: WhenCondition, Change: "c1", Type: CondChecks, Status: CondOK}, Then: Then{Kind: KindRuleNotify}, Status: RuleArmed, After: AfterDisarm}},
		}
		if vs := CheckThread(th3, &View{}, now, nil); !hasViolation(vs, InvThreadRuleInvalid) {
			t.Fatalf("expected %s for unwatchable rule, got %+v", InvThreadRuleInvalid, vs)
		}
		// Armed one-shot watch with LastValue=true.
		th4 := &Thread{ID: "t4", Rules: []Rule{{ID: "w2", Purpose: PurposeWatch, When: When{Kind: WhenAt, At: ptr(later)}, Then: Then{Kind: KindRuleNotify}, Status: RuleArmed, After: AfterDisarm, LastValue: true}}}
		if vs := CheckThread(th4, &View{}, now, nil); !hasViolation(vs, InvThreadRuleInvalid) {
			t.Fatalf("expected %s for stuck one-shot watch, got %+v", InvThreadRuleInvalid, vs)
		}
	})

	t.Run("decision-invalid", func(t *testing.T) {
		// Duplicate decision ID across Decisions and HeldAsks.
		mergeReq := &EffectReq{Action: FyrMerge, Args: map[string]any{"pr": testPR, "head": "1234567"}}
		th := withPR(Thread{ID: "t1", Decisions: []Decision{{ID: "d1", Kind: DecisionEffect, Effect: mergeReq}}, HeldAsks: []Decision{{ID: "d1", Kind: DecisionEffect, Effect: mergeReq}}}, PR{State: "OPEN", Checks: ChecksPending, Head: "1234567"})
		if vs := CheckThread(&th, &View{}, now, nil); !hasViolation(vs, InvThreadDecisionInvalid) {
			t.Fatalf("expected %s, got %+v", InvThreadDecisionInvalid, vs)
		}
		// Held merge ask whose PR is green (should have opened).
		th2 := withPR(Thread{ID: "t2", HeldAsks: []Decision{{ID: "d2", Kind: DecisionEffect, Effect: mergeReq}}}, PR{State: "OPEN", Checks: ChecksPass, MergeState: "CLEAN", Head: "1234567"})
		if vs := CheckThread(&th2, &View{}, now, nil); !hasViolation(vs, InvThreadDecisionInvalid) {
			t.Fatalf("expected %s for held ask on green PR, got %+v", InvThreadDecisionInvalid, vs)
		}
	})

	t.Run("change-invalid", func(t *testing.T) {
		// Observed MERGED PR still in working phase without LandedAt or Done.
		th := &Thread{ID: "t1", MainChange: "c_missing", Changes: []Change{{
			ID: "c1", Key: "k1", Kind: KindCode, Phase: PhaseWorking, PhaseBy: ByObserved,
			PR: &PR{URL: testPR, State: "MERGED"},
		}}}
		if vs := CheckThread(th, &View{}, now, nil); !hasViolation(vs, InvThreadChangeInvalid) {
			t.Fatalf("expected %s, got %+v", InvThreadChangeInvalid, vs)
		}
		// Stale park.
		th2 := &Thread{ID: "t2", Changes: []Change{{
			ID: "c2", Key: "k2", Kind: KindCode, Phase: PhaseReview, PhaseBy: ByObserved,
			PR:     &PR{URL: testPR, State: "OPEN", Head: "bbbbbbb"},
			Parked: &Park{At: t0, Seen: "aaaaaaa|"},
		}}}
		if vs := CheckThread(th2, &View{}, now, nil); !hasViolation(vs, InvThreadChangeInvalid) {
			t.Fatalf("expected %s for stale park, got %+v", InvThreadChangeInvalid, vs)
		}
	})

	t.Run("outbox-invalid", func(t *testing.T) {
		th := &Thread{ID: "t1", Conversation: Conversation{Outbox: []OutMsg{{ID: "m1", State: "lost"}}}}
		if vs := CheckThread(th, &View{}, now, nil); !hasViolation(vs, InvThreadOutboxInvalid) {
			t.Fatalf("expected %s, got %+v", InvThreadOutboxInvalid, vs)
		}
	})

	t.Run("feed-invalid", func(t *testing.T) {
		th := &Thread{ID: "t1", Feed: Feed{Queue: []Notice{{Key: "k1", Urgency: Silent}, {Key: "k1", Urgency: Wake}}}}
		if vs := CheckThread(th, &View{}, now, nil); !hasViolation(vs, InvThreadFeedInvalid) {
			t.Fatalf("expected %s, got %+v", InvThreadFeedInvalid, vs)
		}
	})

	t.Run("fyr-tool-approval", func(t *testing.T) {
		// V0.agy-reattach-allowlist: Fyr's own MCP tools must never open an owner approval card.
		th := &Thread{ID: "t1", Decisions: []Decision{{ID: "d1", Kind: DecisionApproval, Assignee: Owner, Tool: "mcp__fyr__fyr_state", CreatedAt: t0}}}
		if vs := CheckThread(th, &View{}, now, nil); !hasViolation(vs, InvThreadFyrToolApproval) {
			t.Fatalf("expected %s for mcp__fyr__*, got %+v", InvThreadFyrToolApproval, vs)
		}
		thEager := &Thread{ID: "t2", Decisions: []Decision{{ID: "d2", Kind: DecisionApproval, Assignee: Owner, Tool: "mcp_fyr_change_list", CreatedAt: t0}}}
		if vs := CheckThread(thEager, &View{}, now, nil); !hasViolation(vs, InvThreadFyrToolApproval) {
			t.Fatalf("expected %s for mcp_fyr_*, got %+v", InvThreadFyrToolApproval, vs)
		}
		// Policy-gated Bash calls (core.Gate) must be denied before reaching owner approvals.
		thGate := &Thread{ID: "t3", Decisions: []Decision{{ID: "d3", Kind: DecisionApproval, Assignee: Owner, Tool: "Bash", Input: map[string]any{"command": "gh pr merge 42"}, CreatedAt: t0}}}
		if vs := CheckThread(thGate, &View{}, now, nil); !hasViolation(vs, InvThreadFyrToolApproval) {
			t.Fatalf("expected %s for policy-gated Bash, got %+v", InvThreadFyrToolApproval, vs)
		}
		// Ordinary tool approval is clean.
		thOK := &Thread{ID: "t4", Decisions: []Decision{{ID: "d4", Kind: DecisionApproval, Assignee: Owner, Tool: "Bash", Input: map[string]any{"command": "make test"}, CreatedAt: t0}}}
		if vs := CheckThread(thOK, &View{}, now, nil); hasViolation(vs, InvThreadFyrToolApproval) {
			t.Fatalf("unexpected %s for ordinary Bash approval: %+v", InvThreadFyrToolApproval, vs)
		}
	})

	t.Run("quota-misclassified", func(t *testing.T) {
		// Quota notice filed under a non-quota failure class.
		th := &Thread{ID: "t1", Conversation: Conversation{Failure: &Failure{Class: "api", Message: "⚠ Individual quota reached. Resets in 1h.", At: t0}}}
		if vs := CheckThread(th, &View{}, now, nil); !hasViolation(vs, InvThreadQuotaMisclass) {
			t.Fatalf("expected %s for wrong failure class, got %+v", InvThreadQuotaMisclass, vs)
		}
		// Properly classified quota wait is clean, including across QuotaResumeBuffer before KindClock fires.
		thOK := &Thread{ID: "t2", Conversation: Conversation{Failure: &Failure{Class: ClassQuota, Message: "⚠ Individual quota reached. Resets in 1h.", At: t0}}}
		if vs := CheckThread(thOK, &View{}, now, nil); hasViolation(vs, InvThreadQuotaMisclass) {
			t.Fatalf("unexpected %s for valid quota wait: %+v", InvThreadQuotaMisclass, vs)
		}
		if vs := CheckThread(thOK, &View{}, t0.Add(time.Hour+5*time.Second), nil); hasViolation(vs, InvThreadQuotaMisclass) {
			t.Fatalf("unexpected %s during quota resume buffer: %+v", InvThreadQuotaMisclass, vs)
		}
		// Exhausted auto-resumes surface as failed without a quota wait violation.
		thExhausted := &Thread{ID: "t3", Conversation: Conversation{Failure: &Failure{Class: ClassQuota, Message: "⚠ Individual quota reached. Resets in 1h.", At: t0}, QuotaResumes: MaxQuotaAutoResumes}}
		if vs := CheckThread(thExhausted, &View{}, now, nil); hasViolation(vs, InvThreadQuotaMisclass) {
			t.Fatalf("unexpected %s for exhausted quota retries: %+v", InvThreadQuotaMisclass, vs)
		}
	})

	t.Run("rule-invalid-dead-when-and-compound-unwatchable", func(t *testing.T) {
		// Dead WhenAt / WhenQuota without timestamp.
		th1 := &Thread{ID: "t1", Rules: []Rule{{ID: "w1", Purpose: PurposeWatch, When: When{Kind: WhenAt}, Then: Then{Kind: KindRuleNotify}, Status: RuleArmed}}}
		if vs := CheckThread(th1, &View{}, now, nil); !hasViolation(vs, InvThreadRuleInvalid) {
			t.Fatalf("expected %s for nil WhenAt, got %+v", InvThreadRuleInvalid, vs)
		}
		th2 := &Thread{ID: "t2", Rules: []Rule{{ID: "w2", Purpose: PurposeWatch, When: When{Kind: WhenQuota, Provider: "claude"}, Then: Then{Kind: KindRuleNotify}, Status: RuleArmed}}}
		if vs := CheckThread(th2, &View{}, now, nil); !hasViolation(vs, InvThreadRuleInvalid) {
			t.Fatalf("expected %s for nil WhenQuota.At, got %+v", InvThreadRuleInvalid, vs)
		}
		// Compound settle rule wrapping an unwatchable PR condition on a landed change.
		th3 := &Thread{ID: "t3",
			Changes: []Change{{ID: "c1", Key: "k1", Kind: KindCode, Phase: PhaseLanded, PhaseBy: ByObserved, LandedAt: ptr(t0), Done: &DoneMark{At: t0, By: ByObserved, Reason: "merged"}}},
			Rules:   []Rule{{ID: "s1", Purpose: PurposeSettle, About: "c1", When: All(When{Kind: WhenCondition, Change: "c1", Type: CondChecks, Status: CondOK}, When{Kind: WhenIdle}), Then: Then{Kind: KindRuleSettle}, Status: RuleArmed, After: AfterDisarm}},
		}
		if vs := CheckThread(th3, &View{}, now, nil); !hasViolation(vs, InvThreadRuleInvalid) {
			t.Fatalf("expected %s for compound unwatchable rule, got %+v", InvThreadRuleInvalid, vs)
		}
	})

	t.Run("change-invalid-non-open-pr-condition-any-phaseby", func(t *testing.T) {
		th := &Thread{ID: "t1", Changes: []Change{{
			ID: "c1", Key: "k1", Kind: KindCode, Phase: PhaseAbandoned, PhaseBy: ByOwner,
			PR:         &PR{URL: testPR, State: "CLOSED"},
			Conditions: []Condition{{Type: CondMerge, Status: CondFail, Reason: "stale"}},
		}}}
		if vs := CheckThread(th, &View{}, now, nil); !hasViolation(vs, InvThreadChangeInvalid) {
			t.Fatalf("expected %s for non-OPEN PR condition with PhaseBy=owner, got %+v", InvThreadChangeInvalid, vs)
		}
	})

	t.Run("serialization-drift", func(t *testing.T) {
		// Clean thread has zero round-trip drift.
		th := &Thread{ID: "t1", CreatedAt: t0, Changes: []Change{{
			ID: "c1", Key: "k1", Kind: KindCode, Phase: PhaseLanded, PhaseBy: ByObserved,
			LandedAt: ptr(t0), Done: &DoneMark{At: t0, By: ByObserved, Reason: "merged"},
			PR:         &PR{URL: testPR, State: "MERGED"},
			Conditions: []Condition{}, // non-nil empty slice; V0.rule-digest-stable keeps digest stable
		}}}
		if vs := CheckSerialization(th, &View{User: Owner, Entity: "t1"}, now, nil); len(vs) != 0 {
			t.Fatalf("unexpected serialization drift on clean thread: %+v", vs)
		}
	})
}

func TestThreadInvariantsTransitionViolations(t *testing.T) {
	now := t0.Add(5 * time.Minute)

	t.Run("archived-mutated", func(t *testing.T) {
		before := &Thread{ID: "t1", ArchivedAt: ptr(t0)}
		after := &Thread{ID: "t1", ArchivedAt: ptr(t0), Conversation: Conversation{Running: true, RunStartedAt: ptr(now)}}
		vs := CheckTransition(before, &View{}, after, &View{}, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, Result{}, now, nil)
		if !hasViolation(vs, InvThreadArchivedMutated) {
			t.Fatalf("expected %s, got %+v", InvThreadArchivedMutated, vs)
		}
	})

	t.Run("settled-run-started", func(t *testing.T) {
		before := &Thread{ID: "t1", Settle: Settle{At: ptr(t0)}}
		after := &Thread{ID: "t1", Settle: Settle{At: ptr(t0)}, Conversation: Conversation{Running: true, RunStartedAt: ptr(now)}}
		vs := CheckTransition(before, &View{}, after, &View{}, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, Result{}, now, nil)
		if !hasViolation(vs, InvThreadSettledRunStarted) {
			t.Fatalf("expected %s, got %+v", InvThreadSettledRunStarted, vs)
		}
	})

	t.Run("revive-incomplete", func(t *testing.T) {
		before := &Thread{ID: "t1", Settle: Settle{At: ptr(t0)}}
		after := &Thread{ID: "t1", Settle: Settle{At: ptr(t0)}}
		vs := CheckTransition(before, &View{}, after, &View{}, Input{Source: FromOwner, Kind: "thread.send"}, Result{}, now, nil)
		if !hasViolation(vs, InvThreadReviveIncomplete) {
			t.Fatalf("expected %s, got %+v", InvThreadReviveIncomplete, vs)
		}
		// Agent send moving owner seenAt.
		bv := &View{SeenAt: t0}
		av := &View{SeenAt: now}
		vs2 := CheckTransition(&Thread{ID: "t1"}, bv, &Thread{ID: "t1"}, av, Input{Source: FromAgent, Kind: "thread.send"}, Result{}, now, nil)
		if !hasViolation(vs2, InvThreadReviveIncomplete) {
			t.Fatalf("expected %s when agent moves seenAt, got %+v", InvThreadReviveIncomplete, vs2)
		}
	})

	t.Run("rights-violated", func(t *testing.T) {
		before := &Thread{ID: "t1", Title: "Owner name", TitleBy: ByOwner}
		after := &Thread{ID: "t1", Title: "Agent name", TitleBy: ByAgent}
		vs := CheckTransition(before, &View{}, after, &View{}, Input{Source: FromAgent, Kind: KindThreadRename}, Result{}, now, nil)
		if !hasViolation(vs, InvThreadRightsViolated) {
			t.Fatalf("expected %s, got %+v", InvThreadRightsViolated, vs)
		}
	})

	t.Run("counters-regressed", func(t *testing.T) {
		before := &Thread{ID: "t1", CreatedAt: t0, Conversation: Conversation{Running: true, RunStartedAt: ptr(t0), RunEnds: 3}}
		after := &Thread{ID: "t1", CreatedAt: t0, Conversation: Conversation{Running: false, RunEnds: 2}}
		vs := CheckTransition(before, &View{}, after, &View{}, Input{Source: Observer(ObsProcess), Kind: KindRunEnded}, Result{}, now, nil)
		if !hasViolation(vs, InvThreadCountersRegressed) {
			t.Fatalf("expected %s, got %+v", InvThreadCountersRegressed, vs)
		}
	})

	t.Run("poll-stole-seen", func(t *testing.T) {
		before := withPR(Thread{ID: "t1", ActivityAt: t0}, PR{State: "OPEN", Checks: ChecksPending})
		after := withPR(Thread{ID: "t1", ActivityAt: now}, PR{State: "OPEN", Checks: ChecksPass})
		v := &View{SeenAt: t0.Add(time.Minute)}
		vs := CheckTransition(&before, v, &after, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: pr(PR{State: "OPEN", Checks: ChecksPass})}, Result{}, now, nil)
		if !hasViolation(vs, InvThreadPollStoleSeen) {
			t.Fatalf("expected %s, got %+v", InvThreadPollStoleSeen, vs)
		}
	})

	t.Run("rule-loop", func(t *testing.T) {
		th := &Thread{ID: "t1"}
		res := Result{Events: []Event{{Type: "rule.loop", Data: map[string]any{"rule": "r1"}}}}
		vs := CheckTransition(th, &View{}, th, &View{}, Input{Source: Observer(ObsClock), Kind: KindClock}, res, now, nil)
		if !hasViolation(vs, InvThreadRuleLoop) {
			t.Fatalf("expected %s, got %+v", InvThreadRuleLoop, vs)
		}
	})

	t.Run("void-merge-ask-closed-as-gate", func(t *testing.T) {
		th := &Thread{ID: "t1"}
		res := Result{Events: []Event{{Type: "decision.closed", Data: map[string]any{"id": "d1", "gate": true, "reason": "the PR is not open"}}}}
		vs := CheckTransition(th, &View{}, th, &View{}, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved}, res, now, nil)
		if !hasViolation(vs, InvThreadDecisionInvalid) {
			t.Fatalf("expected %s for void ask closed as gate, got %+v", InvThreadDecisionInvalid, vs)
		}
	})
}

// Regression tests for the real transition bugs uncovered by the thread invariants.
func TestThreadInvariantUncoveredFixes(t *testing.T) {
	now := t0.Add(10 * time.Minute)

	t.Run("rule.settle clears snooze on view", func(t *testing.T) {
		th0 := withPR(Thread{ID: "t1", CreatedAt: t0}, PR{State: "OPEN", Checks: ChecksPass})
		v0 := &View{User: Owner, Entity: "t1"}
		th, v, _, err := Reduce(&th0, v0, Input{Source: FromOwner, Kind: "thread.settleWhenMerged"}, t0.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		th, v, _, err = Reduce(th, v, Input{Source: FromOwner, Kind: "thread.snooze", Args: Args{"until": now.Add(time.Hour).Format(time.RFC3339)}}, t0.Add(2*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		th, v, res, err := Reduce(th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: pr(PR{State: "MERGED", Checks: ChecksPass})}, now)
		if err != nil {
			t.Fatal(err)
		}
		if th.Settle.At == nil {
			t.Fatal("expected thread to settle on PR merge")
		}
		if u, _ := SnoozeOf(v); u != nil {
			t.Fatalf("rule.settle left snooze armed until %v", u)
		}
		if vs := CheckThread(th, v, now, nil); len(vs) != 0 {
			t.Fatalf("violations after rule.settle: %+v (events=%+v)", vs, res.Events)
		}
	})

	t.Run("send.failed clears lost message from outbox", func(t *testing.T) {
		th := &Thread{ID: "t1", CreatedAt: t0}
		v := &View{User: Owner, Entity: "t1"}
		th, v, _, err := Reduce(th, v, Input{Source: Observer(ObsProcess), Kind: KindMessageSent, Args: MessageSentArgs{ID: "m1"}}, t0.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if len(th.Outbox) != 1 {
			t.Fatalf("expected outbox=1, got %+v", th.Outbox)
		}
		th, v, _, err = Reduce(th, v, Input{Source: Observer(ObsProcess), Kind: KindSendFailed, Args: SendFailedArgs{
			Message: "Could not start the agent",
			Log:     []Event{{Type: "message.state", Data: map[string]any{"id": "m1", "state": "lost"}}},
		}}, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(th.Outbox) != 0 {
			t.Fatalf("send.failed left lost message in outbox: %+v", th.Outbox)
		}
	})

	t.Run("archive clears starting, settleAsk, and settle rules", func(t *testing.T) {
		th := &Thread{ID: "t1", CreatedAt: t0, Conversation: Conversation{Starting: ptr(t0)}, SettleAsk: &SettleAsk{At: t0, Why: "done"}}
		SettleWhenDone(th, ByAgent, t0)
		v := &View{User: Owner, Entity: "t1"}
		th, v, _, err := Reduce(th, v, Input{Source: FromOwner, Kind: "thread.archive"}, now)
		if err != nil {
			t.Fatal(err)
		}
		if vs := CheckThread(th, v, now, nil); len(vs) != 0 {
			t.Fatalf("violations after archive: %+v", vs)
		}
	})

	t.Run("settle clears starting", func(t *testing.T) {
		th := &Thread{ID: "t1", CreatedAt: t0, Conversation: Conversation{Starting: ptr(t0)}}
		v := &View{User: Owner, Entity: "t1"}
		th, v, _, err := Reduce(th, v, Input{Source: FromOwner, Kind: "thread.settle"}, now)
		if err != nil {
			t.Fatal(err)
		}
		if vs := CheckThread(th, v, now, nil); len(vs) != 0 {
			t.Fatalf("violations after settle: %+v", vs)
		}
	})

	t.Run("rule.arm and settle.request rejected on settled or archived threads", func(t *testing.T) {
		at := now.Add(time.Hour)
		for _, tc := range []struct {
			name string
			th   *Thread
		}{
			{"settled", &Thread{ID: "t1", CreatedAt: t0, Settle: Settle{At: ptr(t0)}}},
			{"archived", &Thread{ID: "t2", CreatedAt: t0, ArchivedAt: ptr(t0)}},
		} {
			if _, _, _, err := Reduce(tc.th, &View{}, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w1", When: When{Kind: WhenAt, At: &at}, Then: "settle"}}, now); err == nil {
				t.Errorf("%s: expected rule.arm to fail", tc.name)
			}
			if _, _, _, err := Reduce(tc.th, &View{}, Input{Source: FromAgent, Kind: KindSettleRequest, Args: Args{"why": "done"}}, now); err == nil {
				t.Errorf("%s: expected settle.request to fail", tc.name)
			}
		}
	})

	t.Run("send does not trigger WhenIdle settle rule while starting", func(t *testing.T) {
		due := t0.Add(time.Minute)
		th := &Thread{ID: "t1", CreatedAt: t0, Conversation: Conversation{Failure: &Failure{At: t0, Message: "err"}}}
		v := &View{User: Owner, Entity: "t1"}
		th, v, _, err := Reduce(th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w1", When: When{Kind: WhenAt, At: &due}, Then: "settle"}}, t0)
		if err != nil {
			t.Fatal(err)
		}
		th, v, _, err = Reduce(th, v, Input{Source: FromOwner, Kind: "thread.send", Args: Args{"text": "keep going"}}, now)
		if err != nil {
			t.Fatal(err)
		}
		if th.Settle.At != nil {
			t.Fatal("thread settled during thread.send while starting")
		}
		if th.Starting == nil {
			t.Fatal("expected Starting to remain set")
		}
		if vs := CheckThread(th, v, now, nil); len(vs) != 0 {
			t.Fatalf("violations: %+v", vs)
		}
	})

	t.Run("settle and archive clear machineWait and ignore late machineWait", func(t *testing.T) {
		th := &Thread{ID: "t1", CreatedAt: t0, Conversation: Conversation{MachineWait: &MachineWait{Since: t0, Why: "memory critical"}}}
		v := &View{User: Owner, Entity: "t1"}
		th, v, _, err := Reduce(th, v, Input{Source: FromOwner, Kind: "thread.settle"}, now)
		if err != nil {
			t.Fatal(err)
		}
		if th.MachineWait != nil {
			t.Fatalf("settle left machineWait set: %+v", th.MachineWait)
		}
		// Late machineWait while settled is ignored.
		th, v, _, err = Reduce(th, v, Input{Source: Observer(ObsProcess), Kind: KindMachineWait, Args: MachineWaitArgs{Why: "memory critical"}}, now.Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if th.MachineWait != nil {
			t.Fatalf("settled thread acquired machineWait: %+v", th.MachineWait)
		}
		if vs := CheckThread(th, v, now.Add(time.Second), nil); len(vs) != 0 {
			t.Fatalf("violations: %+v", vs)
		}
	})

	t.Run("compound unwatchable settle rule drops when PR merges", func(t *testing.T) {
		th0 := withPR(Thread{ID: "t1", CreatedAt: t0}, PR{State: "OPEN", Checks: ChecksPending})
		v0 := &View{User: Owner, Entity: "t1"}
		th, v, _, err := Reduce(&th0, v0, Input{
			Source: FromAgent, Kind: KindRuleArm,
			Args: RuleArmArgs{ID: "w_settle_ci", When: When{Kind: WhenCondition, Change: th0.Changes[0].ID, Type: CondChecks, Status: CondOK}, Then: "settle"},
		}, t0.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		th, v, _, err = Reduce(th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: pr(PR{State: "MERGED", Checks: ChecksPass})}, now)
		if err != nil {
			t.Fatal(err)
		}
		if FindRule(th, "w_settle_ci") != nil {
			t.Fatalf("compound unwatchable settle rule survived merge: %+v", th.Rules)
		}
		if vs := CheckThread(th, v, now, nil); len(vs) != 0 {
			t.Fatalf("violations: %+v", vs)
		}
	})
}

// TestThreadInvariantsUnderRandomStateMachine drives random sequences of owner
// commands, agent tools, observers, rules, PR updates, restarts, and clock
// ticks through core.Reduce and verifies EVERY pure state and transition
// invariant after every single step.
func TestThreadInvariantsUnderRandomStateMachine(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for run := range 400 {
		now := t0
		th, v, _, err := Reduce(&Thread{ID: fmt.Sprintf("t_%d", run), Owner: Owner, Visibility: "private", CreatedAt: now}, nil,
			Input{Source: FromOwner, Kind: "thread.create", Args: Args{"cwd": "/repo", "text": "start work"}}, now)
		if err != nil {
			t.Fatalf("run %d create: %v", run, err)
		}
		if vs := CheckThread(th, v, now, nil); len(vs) != 0 {
			t.Fatalf("run %d create violations: %+v", run, vs)
		}
		msgSeq := 0
		decSeq := 0
		ruleSeq := 0
		for step := range 80 {
			now = now.Add(time.Duration(1+rng.Intn(30)) * time.Minute)
			var in Input
			switch rng.Intn(30) {
			case 0: // run.started (only when active and not running)
				if th.ArchivedAt != nil || th.Settle.At != nil || th.Running {
					continue
				}
				in = Input{Source: Observer(ObsProcess), Kind: KindRunStarted}
			case 1: // run.ended (ok, api fail, or quota fail)
				if !th.Running {
					continue
				}
				var f *Failure
				switch rng.Intn(4) {
				case 0:
					f = &Failure{Class: "api", Message: "API 500", At: now}
				case 1:
					f = &Failure{Class: ClassQuota, Message: "Individual quota reached. Resets in 45m", At: now}
				}
				in = Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: RunEndedArgs{Failure: f, Data: map[string]any{}}}
			case 2: // process.lost
				if th.ArchivedAt != nil {
					continue
				}
				in = Input{Source: Observer(ObsProcess), Kind: KindProcessLost, Args: "exit 1"}
			case 3: // message.sent + message.state
				if th.ArchivedAt != nil || th.Settle.At != nil {
					continue
				}
				if len(th.Outbox) > 0 && rng.Intn(2) == 0 {
					states := []string{"queued", "read", "cancelled", "lost"}
					in = Input{Source: Observer(ObsProcess), Kind: KindMessageState, Args: MessageStateArgs{ID: th.Outbox[0].ID, State: states[rng.Intn(len(states))]}}
				} else {
					msgSeq++
					in = Input{Source: Observer(ObsProcess), Kind: KindMessageSent, Args: MessageSentArgs{ID: fmt.Sprintf("m%d", msgSeq), First: !th.Running && rng.Intn(2) == 0, Text: "hi"}}
				}
			case 4: // task observed
				if th.ArchivedAt != nil {
					continue
				}
				st := TaskRunning
				if rng.Intn(2) == 0 {
					st = TaskCompleted
				}
				in = Input{Source: Observer(ObsProcess), Kind: KindTask, Args: TaskArgs{Task: Task{ID: "task1", Description: "sub", Status: st}}}
			case 5: // process-bound decision opened (only while running)
				if !th.Running {
					continue
				}
				decSeq++
				in = Input{Source: Observer(ObsProcess), Kind: KindDecisionOpened, Args: Decision{ID: fmt.Sprintf("d%d", decSeq), Kind: DecisionAsk, Assignee: Owner, Questions: []Question{{Question: "Q?"}}}}
			case 6: // async ask via tool
				if th.ArchivedAt != nil {
					continue
				}
				decSeq++
				in = Input{Source: FromAgent, Kind: KindAsk, Args: AskArgs{ID: fmt.Sprintf("da%d", decSeq), Questions: []Question{{Question: fmt.Sprintf("Q%d?", decSeq)}}}}
			case 7: // decision withdrawn
				if len(th.Decisions) == 0 {
					continue
				}
				in = Input{Source: Observer(ObsProcess), Kind: KindDecisionGone, Args: th.Decisions[0].ID}
			case 8: // PR observed
				states := []string{"OPEN", "OPEN", "MERGED", "CLOSED"}
				checks := []Checks{ChecksPass, ChecksFail, ChecksPending}
				heads := []string{"1111111aaaa", "2222222bbbb"}
				mstates := []string{"CLEAN", "BLOCKED", "UNKNOWN"}
				p := pr(PR{
					State:      states[rng.Intn(len(states))],
					Checks:     checks[rng.Intn(len(checks))],
					Head:       heads[rng.Intn(len(heads))],
					MergeState: mstates[rng.Intn(len(mstates))],
					CheckedAt:  ptr(now),
				})
				in = Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: p}
			case 9: // agent merge request
				if th.ArchivedAt != nil {
					continue
				}
				decSeq++
				in = Input{Source: FromAgent, Kind: KindChangeRequest, Args: ChangeRequest{ID: fmt.Sprintf("dm%d", decSeq), Change: testPR, Ask: AskMerge, Reason: "ship"}}
			case 10: // agent settle_request
				if th.ArchivedAt != nil {
					continue
				}
				in = Input{Source: FromAgent, Kind: KindSettleRequest, Args: Args{"why": "done", "auto": rng.Intn(2) == 0}}
			case 11: // agent watch arm
				if th.ArchivedAt != nil {
					continue
				}
				ruleSeq++
				atTime := now.Add(time.Duration(5+rng.Intn(60)) * time.Minute)
				then := "notify"
				if rng.Intn(3) == 0 {
					then = "settle"
				}
				in = Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: fmt.Sprintf("w_%d", ruleSeq), When: When{Kind: WhenAt, At: &atTime}, Then: then, Note: "check"}}
			case 12: // agent run_request
				if th.ArchivedAt != nil {
					continue
				}
				in = Input{Source: FromAgent, Kind: KindRunRequest, Args: RunRequestArgs{Command: "echo hi", Why: "check", Optional: rng.Intn(2) == 0}}
			case 13: // agent rename
				in = Input{Source: FromAgent, Kind: KindThreadRename, Args: map[string]any{"title": fmt.Sprintf("Agent title %d", step)}}
			case 14: // unresponsive / responsive / restarted
				switch rng.Intn(3) {
				case 0:
					in = Input{Source: Observer(ObsProcess), Kind: KindUnresponsive, Args: UnresponsiveArgs{Since: now.Add(-time.Minute), Reason: "silent"}}
				case 1:
					in = Input{Source: Observer(ObsProcess), Kind: KindResponsive}
				default:
					in = Input{Source: Observer(ObsProcess), Kind: KindRestarted, Args: RestartedArgs{Why: "watchdog"}}
				}
			case 15: // machine wait on/off
				why := ""
				if rng.Intn(2) == 0 {
					why = "memory critical"
				}
				in = Input{Source: Observer(ObsProcess), Kind: KindMachineWait, Args: MachineWaitArgs{Why: why}}
			case 16: // handover done / failed
				if th.Handover == nil {
					continue
				}
				if rng.Intn(2) == 0 {
					in = Input{Source: Observer(ObsProcess), Kind: KindHandoverDone, Args: HandoverDoneArgs{Profile: th.Handover.To, Model: "m2", SessionID: "s2"}}
				} else {
					in = Input{Source: Observer(ObsProcess), Kind: KindHandoverFailed, Args: "boom"}
				}
			case 17: // clock tick
				in = Input{Source: Observer(ObsClock), Kind: KindClock}
			case 18: // host.restarted
				in = Input{Source: Observer(ObsHost), Kind: KindHostRestarted, Args: RestartArgs{Kept: rng.Intn(3) == 0}}
			case 19: // agent thread.send (another thread)
				in = Input{Source: FromAgent, From: "t_other", Kind: "thread.send", Args: Args{"text": "ping from peer"}}
			default: // owner commands from Registry
				cmds := []string{
					"thread.send", "thread.answer", "thread.cancelAsk", "thread.interrupt",
					"thread.retry", "thread.dismiss", "thread.dismissPR", "thread.settle",
					"thread.settleWhenDone", "thread.settleWhenMerged", "thread.unsettle",
					"thread.snooze", "thread.snoozeWhenDone", "thread.unsnooze",
					"thread.pin", "thread.unpin", "thread.archive", "thread.unarchive",
					"thread.markSeen", "thread.markUnseen", "thread.rename",
					"thread.setView", "thread.handover", "thread.setModel",
					KindChangeEnd, KindChangeDone, KindChangePark, KindChangeUnpark,
					KindWakeOnQuota, KindCancelWake, KindCancelWait,
				}
				name := cmds[rng.Intn(len(cmds))]
				args := Args{
					"text":     "hello",
					"title":    fmt.Sprintf("Owner title %d", step),
					"model":    "sonnet",
					"profile":  "other",
					"on":       rng.Intn(2) == 0,
					"until":    now.Add(time.Hour).Format(time.RFC3339),
					"at":       now.Add(time.Hour).Format(time.RFC3339),
					"provider": "claude",
					"window":   "5 h",
					"allow":    rng.Intn(2) == 0,
				}
				if len(th.Decisions) > 0 {
					d := th.Decisions[0]
					args["decisionId"] = d.ID
					ans := map[string]any{}
					for _, q := range d.Questions {
						ans[q.Question] = "yes"
					}
					args["answers"] = ans
				}
				if len(th.Changes) > 0 {
					args["change"] = th.Changes[0].ID
				}
				in = Input{Source: FromOwner, Kind: name, Args: args}
			}

			beforeT, beforeV := CloneThread(th), cloneView(v)
			nextT, nextV, res, err := Reduce(th, v, in, now)
			if err != nil {
				// Pure Reduce must never mutate caller inputs on error.
				if th.Running != beforeT.Running || (th.ArchivedAt == nil) != (beforeT.ArchivedAt == nil) || !v.SeenAt.Equal(beforeV.SeenAt) {
					t.Fatalf("run %d step %d (%s): failed Reduce mutated input", run, step, in.Kind)
				}
				continue
			}
			if vs := CheckTransition(beforeT, beforeV, nextT, nextV, in, res, now, nil); len(vs) != 0 {
				t.Fatalf("run %d step %d (%s/%s): transition violations: %+v", run, step, in.Kind, in.Source, vs)
			}
			if vs := CheckThread(nextT, nextV, now, nil); len(vs) != 0 {
				t.Fatalf("run %d step %d (%s/%s): state violations: %+v", run, step, in.Kind, in.Source, vs)
			}
			if vs := CheckSerialization(nextT, nextV, now, nil); len(vs) != 0 {
				t.Fatalf("run %d step %d (%s/%s): serialization violations: %+v", run, step, in.Kind, in.Source, vs)
			}
			if in.Kind == KindClock {
				if ov := OverdueRule(nextT, nextV, now); ov != "" {
					t.Fatalf("run %d step %d: overdue rule remained after clock: %s", run, step, ov)
				}
			}
			th, v = nextT, nextV
		}
	}
}

// A merge result that arrives after the PR was observed merged must not
// leave a mergeable condition on the non-OPEN PR.
func TestMergeRefusedAfterMergedLeavesNoCondition(t *testing.T) {
	now := t0.Add(10 * time.Minute)
	th := &Thread{ID: "t1"}
	url := "https://github.com/acme/x/pull/1"
	c := ChangePRObserved(th, PR{URL: url, Repo: "acme/x", Number: 1, State: "MERGED", Branch: "b"}, now)
	mergeRefused(th, url, "not merged: timed out", now)
	if k := condition(c, CondMerge); k != nil {
		t.Fatalf("merged PR carries a mergeable condition: %+v", k)
	}
}

func TestRestartDropsConditionsOfNonOpenPR(t *testing.T) {
	now := t0.Add(10 * time.Minute)
	th := &Thread{ID: "t1"}
	url := "https://github.com/acme/x/pull/1"
	c := ChangePRObserved(th, PR{URL: url, Repo: "acme/x", Number: 1, State: "MERGED", Branch: "b"}, now)
	setCondition(c, CondMerge, CondFail, "Merge refused: x", ByObserved, now)
	if _, err := reduceRestarted(Rec{T: th}, Input{Args: RestartArgs{Kept: true}}, now); err != nil {
		t.Fatal(err)
	}
	if k := condition(c, CondMerge); k != nil {
		t.Fatalf("condition kept: %+v", k)
	}
}

// V0.rule-digest-stable: the restart drops the stale conditions of a PR that is
// no longer open and re-anchors the built-in rule; after the thread is saved and
// loaded, the rule must not read the change as moved.
func TestRestartKeepsBuiltinsRuleQuietAfterSaveAndLoad(t *testing.T) {
	now := t0.Add(10 * time.Minute)
	th := &Thread{ID: "t1"}
	c := ChangePRObserved(th, PR{URL: "https://github.com/acme/x/pull/1", Repo: "acme/x", Number: 1, State: "MERGED", Branch: "b"}, now)
	armBuiltins(th, c, now)
	setCondition(c, CondMerge, CondFail, "Merge refused: x", ByObserved, now)
	th, v, _, err := Reduce(th, nil, Input{Source: Observer(ObsClock), Kind: KindClock, Thread: th.ID}, now)
	if err != nil {
		t.Fatal(err)
	}
	th, v, _, err = Reduce(th, v, Input{Source: Observer(ObsHost), Kind: KindHostRestarted, Thread: th.ID, Args: RestartArgs{}}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(th.Changes[0].Conditions) != 0 {
		t.Fatalf("conditions kept: %+v", th.Changes[0].Conditions)
	}
	b, err := json.Marshal(th)
	if err != nil {
		t.Fatal(err)
	}
	var back Thread
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if ov := OverdueRule(&back, v, now.Add(time.Minute)); ov != "" {
		t.Fatalf("after a save and load: %s", ov)
	}
}
