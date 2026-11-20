package core

import (
	"strings"
	"testing"
	"time"
)

// V0.watch-waits: a finished thread whose agent has an armed watch is
// Waiting in Working, the watch its reason; when the watch fires and the
// run ends with none left, it is back in Seen.
func TestArmedWatchIsWaitingInWorking(t *testing.T) {
	th, v := &Thread{ID: "t_1", ActivityAt: ct0, Changes: []Change{{ID: "c_1", Title: "quiet sales", PR: &PR{Number: 1009, State: "MERGED"}}}}, &View{SeenAt: ct0}
	if a := Compute(th, v, ct0); a.Section != SeenSec {
		t.Fatalf("no watch: %+v", a)
	}
	at := ct0.Add(30 * time.Hour)
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_1", When: When{Kind: WhenAt, At: &at}, Then: "notify", Note: "a long note for the agent itself"}}, ct0)
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_2", When: When{Kind: WhenReached, Change: "c_1", Phase: PhaseLive}, Then: "notify"}}, ct0)
	a := Compute(th, v, ct0)
	want := "wakes " + at.Local().Format("2 Jan 15:04") + " · waits for PR #1009 to be live"
	if a.State != Waiting || a.Section != WorkingSec || a.Reason != want {
		t.Fatalf("armed: %+v, want reason %q", a, want)
	}
	if strings.Contains(a.Reason, "note") {
		t.Fatalf("the agent's note is in the reason: %q", a.Reason)
	}
	// A settle watch of the agent's is not work.
	th2 := &Thread{ID: "t_2", ActivityAt: ct0}
	step(t, th2, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_3", When: When{Kind: WhenAt, At: &at}, Then: "settle"}}, ct0)
	if a := Compute(th2, v, ct0); a.Section != SeenSec {
		t.Fatalf("settle watch: %+v", a)
	}
	// Both gone: Seen again.
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleRemove, Args: "w_1"}, ct0)
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleRemove, Args: "w_2"}, ct0)
	if a := Compute(th, v, ct0); a.Section != SeenSec {
		t.Fatalf("removed: %+v", a)
	}
}

// Today shows the time alone; past two the rest are counted.
func TestWatchWaitTextToday(t *testing.T) {
	now := time.Date(2026, 11, 20, 1, 0, 0, 0, time.Local)
	th := &Thread{}
	for i, h := range []int{6, 8, 9} {
		at := time.Date(2026, 11, 20, h, 0, 0, 0, time.Local)
		th.Rules = append(th.Rules, Rule{ID: string(rune('a' + i)), Purpose: PurposeWatch, CreatedBy: ByAgent, Status: RuleArmed, When: When{Kind: WhenAt, At: &at}})
	}
	if s := WatchWaitText(th, now); s != "wakes 06:00 · wakes 08:00 · +1 more" {
		t.Fatalf("%q", s)
	}
}
