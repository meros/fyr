package core

import (
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
