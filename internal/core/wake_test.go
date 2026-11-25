package core

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// V0.wakeups-via-watch (3): an agent's timed watch is a wake-up the owner
// sees and cancels; the agent is told.
func TestWakeIsShownAndTheOwnerCancelsIt(t *testing.T) {
	th, v := &Thread{ID: "t_1", ActivityAt: ct0}, &View{}
	at := ct0.Add(6 * time.Hour)
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_1", When: When{Kind: WhenAt, At: &at}, Then: "notify", Note: "Check the deploy\nthen report"}}, ct0)
	// A watch on CI is not a wake-up.
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_2", When: When{Kind: WhenIdleFor, Ms: 86_400_000}, Then: "notify"}}, ct0)
	ws := Wakes(th)
	if len(ws) != 1 || ws[0].Rule != "w_1" || !ws[0].At.Equal(at) || !strings.HasPrefix(ws[0].Note, "Check the deploy") {
		t.Fatalf("wakes %+v", ws)
	}
	if r := Registry[KindCancelWake].Available(th, v, ct0); r != "" {
		t.Fatalf("cancel off: %s", r)
	}
	step(t, th, v, Input{Source: FromOwner, Kind: KindCancelWake, Args: Args{"rule": "w_1"}}, ct0.Add(time.Minute))
	if len(Wakes(th)) != 0 || FindRule(th, "w_2") == nil {
		t.Fatalf("rules after the cancel: %+v", th.Rules)
	}
	told := false
	for _, n := range th.Feed.Queue {
		told = told || strings.Contains(n.What, "The owner cancelled your wake-up w_1")
	}
	if !told {
		t.Fatalf("the agent is not told: %+v", th.Feed.Queue)
	}
	if r := Registry[KindCancelWake].Available(th, v, ct0); r == "" {
		t.Fatal("cancel on with no wake-up")
	}
}

// A wake-up that fires is gone without a cancel notice.
func TestWakeFiresWithoutCancelNotice(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	at := ct0.Add(time.Hour)
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_1", When: When{Kind: WhenAt, At: &at}, Then: "notify", Note: "go"}}, ct0)
	res := step(t, th, v, clockIn(), at.Add(time.Second))
	if fired(res, "w_1") != 1 || len(Wakes(th)) != 0 {
		t.Fatalf("not fired: %+v", res.Events)
	}
	for _, n := range th.Feed.Queue {
		if strings.Contains(n.What, "cancelled") {
			t.Fatalf("a fired wake-up reads as cancelled: %+v", n)
		}
	}
}

// V0.quota-reset-wake: an agent's quota watch shows as a wake-up with its
// window, sets the thread's Waiting reason, notifies on owner cancel, and
// wakes the agent when the reset passes.
func TestQuotaWakeShownCancelledAndFires(t *testing.T) {
	now := time.Date(2026, 11, 20, 14, 0, 0, 0, time.Local)
	at := time.Date(2026, 11, 20, 18, 0, 0, 0, time.Local)
	th, v := &Thread{ID: "t_1", Profile: "claude", ActivityAt: now}, &View{SeenAt: now}
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{
		ID:   "w_q1",
		When: When{Kind: WhenQuota, Provider: "claude", Window: "5 h", At: &at},
		Then: "notify",
		Note: "Resume the build",
	}}, now)
	ws := Wakes(th)
	if len(ws) != 1 || ws[0].Rule != "w_q1" || ws[0].Provider != "claude" || ws[0].Window != "5 h" || !ws[0].At.Equal(at) {
		t.Fatalf("wakes %+v", ws)
	}
	if got := WatchWaitText(th, now); got != "wakes 18:00, when the 5 h window resets" {
		t.Fatalf("WatchWaitText %q", got)
	}
	if a := Compute(th, v, now); a.State != Waiting || a.Section != WorkingSec || a.Reason != "wakes 18:00, when the 5 h window resets" {
		t.Fatalf("attention %+v", a)
	}
	// Owner cancel notifies the agent and mentions the window.
	step(t, th, v, Input{Source: FromOwner, Kind: KindCancelWake, Args: Args{"rule": "w_q1"}}, now.Add(time.Minute))
	if len(Wakes(th)) != 0 {
		t.Fatalf("not cancelled: %+v", Wakes(th))
	}
	told := false
	for _, n := range th.Feed.Queue {
		told = told || (strings.Contains(n.What, "The owner cancelled your wake-up w_q1") && strings.Contains(n.What, "5 h window resets"))
	}
	if !told {
		t.Fatalf("agent not notified of cancel: %+v", th.Feed.Queue)
	}

	// Arm again and fire at the reset.
	th.Feed.Queue = nil
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{
		ID:   "w_q2",
		When: When{Kind: WhenQuota, Provider: "claude", Window: "5 h", At: &at},
		Then: "notify",
		Note: "Resume the build",
	}}, now)
	res := step(t, th, v, clockIn(), at)
	if fired(res, "w_q2") != 1 || len(Wakes(th)) != 0 {
		t.Fatalf("not fired: %+v", res.Events)
	}
	if len(th.Feed.Queue) == 0 || !strings.Contains(th.Feed.Queue[0].What, "Resume the build") {
		t.Fatalf("wake notice missing: %+v", th.Feed.Queue)
	}
}

// V0.quota-reset-wake: the owner's thread.wakeOnQuota clears a quota failure,
// moves the thread to Waiting, follows a moved reset time (without postponing
// on a new window jump), and cancels quietly without scolding the agent.
func TestOwnerWakeOnQuotaClearsFailureAndFollowsMovedReset(t *testing.T) {
	now := ct0
	at := ct0.Add(2 * time.Hour)
	th, v := &Thread{ID: "t_1", Profile: "claude", ActivityAt: now, Conversation: Conversation{Failure: &Failure{Class: "api", Message: "Quota reached", At: now}}}, &View{SeenAt: now}
	step(t, th, v, Input{Source: FromOwner, Kind: KindWakeOnQuota, Args: Args{
		"id":       "w_o1",
		"provider": "claude",
		"window":   "5 h",
		"at":       at.UTC().Format(time.RFC3339),
	}}, now)
	if th.Failure != nil {
		t.Fatalf("failure not cleared: %+v", th.Failure)
	}
	if a := Compute(th, v, now); a.State != Waiting || a.Section != WorkingSec {
		t.Fatalf("attention after arm: %+v", a)
	}
	ws := Wakes(th)
	if len(ws) != 1 || ws[0].By != ByOwner || !strings.Contains(ws[0].Note, "Quota reached") {
		t.Fatalf("owner wake: %+v", ws)
	}
	// A small drift in ResetsAt moves the wake.
	moved := at.Add(5 * time.Minute)
	step(t, th, v, Input{Source: Observer(ObsQuota), Kind: KindQuotaSync, Args: QuotaSyncArgs{
		Updates: []QuotaUpdate{{Provider: "claude", Window: "5 h", ResetsAt: moved}},
	}}, now.Add(10*time.Minute))
	if ws := Wakes(th); len(ws) != 1 || !ws[0].At.Equal(moved) {
		t.Fatalf("did not follow moved reset: %+v", ws)
	}
	// A jump > 1h (a brand new 5h window) does NOT postpone the armed wake.
	nextWindow := moved.Add(5 * time.Hour)
	step(t, th, v, Input{Source: Observer(ObsQuota), Kind: KindQuotaSync, Args: QuotaSyncArgs{
		Updates: []QuotaUpdate{{Provider: "claude", Window: "5 h", ResetsAt: nextWindow}},
	}}, now.Add(20*time.Minute))
	if ws := Wakes(th); len(ws) != 1 || !ws[0].At.Equal(moved) {
		t.Fatalf("new window jumped armed wake: %+v", ws)
	}
	// Owner cancel of an owner-armed wake sends no agent notice.
	step(t, th, v, Input{Source: FromOwner, Kind: KindCancelWake, Args: Args{"rule": "w_o1"}}, now.Add(30*time.Minute))
	if len(Wakes(th)) != 0 || len(th.Feed.Queue) != 0 {
		t.Fatalf("owner cancel left wakes=%+v queue=%+v", Wakes(th), th.Feed.Queue)
	}
}

// V0.review-correctness (5): a quota wake that is suspended (the thread was
// settled) follows a moved reset too, so it does not resume with the
// earlier time and fire before the quota is back.
func TestQuotaSyncMovesASuspendedWake(t *testing.T) {
	now := ct0
	at := ct0.Add(2 * time.Hour)
	th, v := &Thread{ID: "t_1", Profile: "claude", ActivityAt: now}, &View{SeenAt: now}
	step(t, th, v, Input{Source: FromOwner, Kind: KindWakeOnQuota, Args: Args{
		"id": "w_s1", "provider": "claude", "window": "5 h", "at": at.UTC().Format(time.RFC3339),
	}}, now)
	r := FindRule(th, "w_s1")
	if r == nil {
		t.Fatal("no wake")
	}
	r.Status = RuleSuspended
	moved := at.Add(5 * time.Minute)
	step(t, th, v, Input{Source: Observer(ObsQuota), Kind: KindQuotaSync, Args: QuotaSyncArgs{
		Updates: []QuotaUpdate{{Provider: "claude", Window: "5 h", ResetsAt: moved}},
	}}, now.Add(10*time.Minute))
	got := FindRule(th, "w_s1")
	var quotaAt *time.Time
	var find func(w When)
	find = func(w When) {
		if w.Kind == WhenQuota {
			quotaAt = w.At
		}
		for _, x := range append(slices.Clone(w.All), w.Any...) {
			find(x)
		}
	}
	find(got.When)
	if quotaAt == nil || !quotaAt.Equal(moved) {
		t.Fatalf("suspended wake kept %v, want %v", quotaAt, moved)

	}
}

// V0.waits-strip: every armed watch of the agent's is a wait the owner sees and
// cancels one by one; the agent is told.
func TestWaitsAreShownAndCancelled(t *testing.T) {
	th, v := &Thread{ID: "t_1", ActivityAt: ct0}, &View{}
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_idle", When: When{Kind: WhenIdleFor, Ms: 1_800_000}, Then: "notify"}}, ct0)
	ws := Waits(th, ct0)
	if len(ws) != 1 || ws[0].Rule != "w_idle" || ws[0].Text != "wakes after 30 min idle" {
		t.Fatalf("waits %+v", ws)
	}
	if r := Registry[KindCancelWait].Available(th, v, ct0); r != "" {
		t.Fatalf("cancel off: %s", r)
	}
	step(t, th, v, Input{Source: FromOwner, Kind: KindCancelWait, Args: Args{"rule": "w_idle"}}, ct0.Add(time.Minute))
	if len(Waits(th, ct0)) != 0 {
		t.Fatalf("rules after the cancel: %+v", th.Rules)
	}
	told := false
	for _, n := range th.Feed.Queue {
		told = told || strings.Contains(n.What, "The owner cancelled your watch w_idle")
	}
	if !told {
		t.Fatalf("the agent is not told: %+v", th.Feed.Queue)
	}
	if r := Registry[KindCancelWait].Available(th, v, ct0); r == "" {
		t.Fatal("cancel on with no wait")
	}
}

// V0.snooze-until-wake: a wait on a time carries the time, so the owner can
// snooze the thread until then; a wait on anything else has none.
func TestWaitOnATimeCarriesIt(t *testing.T) {
	th, v := &Thread{ID: "t_1", ActivityAt: ct0}, &View{}
	at := ct0.Add(20 * time.Hour)
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_idle", When: When{Kind: WhenIdleFor, Ms: 1_800_000}, Then: "notify"}}, ct0)
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_at", When: When{Kind: WhenAt, At: &at}, Then: "notify"}}, ct0)
	ws := Waits(th, ct0)
	if len(ws) != 2 || ws[0].Rule != "w_at" || ws[0].At == nil || !ws[0].At.Equal(at) || ws[1].At != nil {
		t.Fatalf("waits %+v", ws)
	}
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.snooze", Args: Args{"until": ws[0].At.Format(time.RFC3339)}}, ct0.Add(time.Minute))
	if until, _ := SnoozeOf(v); until == nil || !until.Equal(at) {
		t.Fatalf("snoozed until %v, want %v", until, at)
	}
	if len(Waits(th, ct0)) != 2 {
		t.Fatalf("the snooze dropped a wait: %+v", th.Rules)
	}
}
