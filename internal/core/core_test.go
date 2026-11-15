package core

import (
	"math/rand"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)

func at(min int) time.Time       { return t0.Add(time.Duration(min) * time.Minute) }
func ptr(t time.Time) *time.Time { return &t }

const testPR = "https://github.com/o/r/pull/1"

// withPR gives th a main PR (on its code change, #32) without moving ActivityAt.
func withPR(th Thread, p PR) Thread {
	if p.URL == "" {
		p.URL = testPR
	}
	act := th.ActivityAt
	PRObserved(&th, p, t0)
	th.ActivityAt = act
	return th
}

// pr is p with the test PR's URL.
func pr(p PR) PR { p.URL = testPR; return p }

func TestAttentionTable(t *testing.T) {
	ask := []Decision{{ID: "d1", Kind: "ask", CreatedAt: at(5)}}
	fail := &Failure{Class: "api", Message: "API error", At: at(5)}
	openFail := PR{State: "OPEN", Checks: "fail"}
	cases := []struct {
		name string
		th   Thread
		v    View
		want State
	}{
		{"new thread never seen is done", Thread{ActivityAt: at(1)}, View{}, Done},
		{"seen after activity", Thread{ActivityAt: at(1)}, View{SeenAt: at(2)}, Seen},
		{"activity after seen is done", Thread{ActivityAt: at(3)}, View{SeenAt: at(2)}, Done},
		{"running is working", Thread{Conversation: Conversation{Running: true}, ActivityAt: at(3)}, View{}, Working},
		{"asking beats running", Thread{Conversation: Conversation{Running: true}, Decisions: ask}, View{SeenAt: at(9)}, Asking},
		{"failed stays after a visit", Thread{Conversation: Conversation{Failure: fail}}, View{SeenAt: at(9)}, Failed},
		{"asking beats failed", Thread{Conversation: Conversation{Failure: fail}, Decisions: ask}, View{}, Asking},
		{"blocked PR when idle", withPR(Thread{}, openFail), View{SeenAt: at(9)}, Blocked},
		{"blocked PR hidden while working", withPR(Thread{Conversation: Conversation{Running: true}}, openFail), View{}, Working},
		{"merged PR does not block", withPR(Thread{}, PR{State: "MERGED", Checks: "fail"}), View{SeenAt: at(9)}, Seen},
		{"settled beats asking", Thread{Settle: Settle{At: ptr(at(1))}, Decisions: ask}, View{}, Settled},
		{"archived beats all", Thread{ArchivedAt: ptr(at(1)), Settle: Settle{At: ptr(at(1))}}, View{}, Archived},
		{"snoozed hides done", Thread{ActivityAt: at(3)}, snoozed(View{}, at(60), at(4)), Snoozed},
		{"snooze expired", Thread{ActivityAt: at(3)}, snoozed(View{SeenAt: at(4)}, at(6), at(4)), Seen},
		{"new question wakes snooze", Thread{Decisions: ask}, snoozed(View{}, at(60), at(4)), Asking},
		{"old question stays snoozed", Thread{Decisions: ask}, snoozed(View{}, at(60), at(6)), Snoozed},
		{"new failure wakes snooze", Thread{Conversation: Conversation{Failure: fail}}, snoozed(View{}, at(60), at(4)), Failed},
	}
	for _, c := range cases {
		got := Compute(&c.th, &c.v, at(10))
		if got.State != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got.State, c.want)
		}
	}
}

func apply(t *testing.T, name string, th *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
	t.Helper()
	act := Registry[name]
	if r := act.Available(th, v, now); r != "" {
		return nil, errUnavailable(r)
	}
	return act.Apply(th, v, a, now)
}

type errUnavailable string

func (e errUnavailable) Error() string { return string(e) }

func TestSettleWhileRunningSettlesWhenDone(t *testing.T) {
	th, v := &Thread{}, &View{}
	RunStarted(th, at(0))
	if _, err := apply(t, "thread.settle", th, v, nil, at(1)); err != nil {
		t.Fatal(err)
	}
	if th.Settle.At != nil || !SettlesWhenDone(th) {
		t.Fatal("settle on a running thread must defer")
	}
	rules(th, at(1))
	RunEnded(th, nil, at(2))
	rules(th, at(2))
	if Compute(th, v, at(3)).State != Settled {
		t.Fatal("thread did not settle when the run ended")
	}
}

func TestFailedRunDoesNotSettle(t *testing.T) {
	th, v := &Thread{}, &View{}
	RunStarted(th, at(0))
	apply(t, "thread.settle", th, v, nil, at(1))
	RunEnded(th, &Failure{Class: "api", Message: "x", At: at(2)}, at(2))
	rules(th, at(2))
	if got := Compute(th, v, at(3)).State; got != Failed {
		t.Fatalf("got %s, want failed", got)
	}
}

func TestAnswerRequiresEveryQuestion(t *testing.T) {
	th, v := &Thread{}, &View{}
	DecisionOpened(th, Decision{ID: "d", Kind: "ask", CreatedAt: at(0), Questions: []Question{{Question: "A?"}, {Question: "B?"}}})
	if _, err := apply(t, "thread.answer", th, v, Args{"decisionId": "d", "answers": map[string]any{"A?": "x"}}, at(1)); err == nil {
		t.Fatal("a partial answer was accepted")
	}
	eff, err := apply(t, "thread.answer", th, v, Args{"decisionId": "d", "answers": map[string]any{"A?": "x", "B?": "y"}}, at(1))
	if err != nil || len(eff) != 1 || eff[0].Answers["B?"] != "y" || len(th.Decisions) != 0 {
		t.Fatalf("answer failed: %v %+v", err, eff)
	}
}

func TestSendBlockedWhileAsking(t *testing.T) {
	th, v := &Thread{}, &View{}
	DecisionOpened(th, Decision{ID: "d", Kind: "ask", CreatedAt: at(0)})
	if _, err := apply(t, "thread.send", th, v, Args{"text": "hi"}, at(1)); err == nil {
		t.Fatal("send while asking must be unavailable")
	}
}

func TestPRMergeSettlesWhenAsked(t *testing.T) {
	th0 := withPR(Thread{}, PR{State: "OPEN", Checks: "pass"})
	th, v := &th0, &View{}
	if _, err := apply(t, "thread.settleWhenMerged", th, v, nil, at(1)); err != nil {
		t.Fatal(err)
	}
	ChangePRObserved(th, pr(PR{State: "MERGED", Checks: "pass"}), at(2))
	rules(th, at(2))
	if Compute(th, v, at(3)).State != Settled {
		t.Fatal("merge did not settle")
	}
}

func TestPollingDoesNotMarkDone(t *testing.T) {
	th0 := withPR(Thread{ActivityAt: at(1)}, PR{State: "OPEN", Checks: "pending"})
	th, v := &th0, &View{SeenAt: at(2)}
	ChangePRObserved(th, pr(PR{State: "OPEN", Checks: "pass"}), at(3))
	if got := Compute(th, v, at(4)).State; got != Seen {
		t.Fatalf("a passing check moved the thread to %s", got)
	}
}

// TestInvariantsUnderRandomCommands runs random sequences of commands and
// observations and checks the invariants from the spec after every step:
//
//	L3  an archived thread has no live work
//	I10 every thread has exactly one state, and failed is never hidden by a visit
//	--  asking threads are always in Needs you unless settled, archived or snoozed
//	--  every Apply leaves state unchanged when it returns an error
func TestInvariantsUnderRandomCommands(t *testing.T) {
	names := []string{}
	for n := range Registry {
		if n != "thread.create" {
			names = append(names, n)
		}
	}
	rng := rand.New(rand.NewSource(1))
	for run := 0; run < 300; run++ {
		th, v := &Thread{ID: "t"}, &View{}
		now := t0
		for step := 0; step < 60; step++ {
			now = now.Add(time.Duration(rng.Intn(120)) * time.Minute)
			switch rng.Intn(8) {
			case 0: // the host starts a run, but only for a send-able thread
				if th.ArchivedAt == nil && !th.Running {
					RunStarted(th, now)
				}
			case 1:
				if th.Running {
					var f *Failure
					if rng.Intn(3) == 0 {
						f = &Failure{Class: "api", Message: "x", At: now}
					}
					RunEnded(th, f, now)
				}
			case 2:
				if th.Running {
					DecisionOpened(th, Decision{ID: "d", Kind: "ask", CreatedAt: now, Questions: []Question{{Question: "Q"}}})
				}
			case 3:
				ProcessLost(th, "process", "exit", now)
			case 4:
				states := []string{"OPEN", "MERGED", "CLOSED"}
				checks := []Checks{"pass", "fail", "pending"}
				PRObserved(th, pr(PR{State: states[rng.Intn(3)], Checks: checks[rng.Intn(3)]}), now)
			default:
				n := names[rng.Intn(len(names))]
				act := Registry[n]
				if act.Available(th, v, now) != "" {
					continue
				}
				args := Args{"text": "x", "title": "x", "model": "m", "until": now.Add(time.Hour).Format(time.RFC3339),
					"decisionId": "d", "answers": map[string]any{"Q": "a"}, "allow": true}
				before, beforeV := *th, *v
				if _, err := act.Apply(th, v, args, now); err != nil {
					if th.Running != before.Running || th.ArchivedAt != before.ArchivedAt || v.SeenAt != beforeV.SeenAt {
						t.Fatalf("run %d: %s failed but changed state", run, n)
					}
				}
			}
			if th.ArchivedAt != nil && (th.Running || len(th.Decisions) > 0) {
				t.Fatalf("run %d step %d: L3 broken: archived thread has live work", run, step)
			}
			a := Compute(th, v, now)
			if th.Failure != nil && th.ArchivedAt == nil && th.Settle.At == nil && len(th.Decisions) == 0 && a.State != Failed && a.State != Snoozed {
				t.Fatalf("run %d step %d: failure hidden as %s", run, step, a.State)
			}
			if len(th.Decisions) > 0 && a.Section != NeedsYou && a.State != Settled && a.State != Archived && a.State != Snoozed {
				t.Fatalf("run %d step %d: asking thread in %s", run, step, a.Section)
			}
		}
	}
}

func TestTitleFrom(t *testing.T) {
	if TitleFrom("  fix the bug\nmore") != "fix the bug" {
		t.Fatal("first line")
	}
	long := TitleFrom(string(make([]rune, 0)) + stringsRepeat("å", 200))
	if len([]rune(long)) != 80 {
		t.Fatalf("title length %d", len([]rune(long)))
	}
}

func stringsRepeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

// SPEC 13.1 `e`: on a thread without a current run and with an open PR,
// settle arms "settle when merged"; bulk settle (Shift+E, U11) settles now.
func TestSettleWithOpenPRArmsSettleWhenMerged(t *testing.T) {
	th0 := withPR(Thread{}, PR{State: "OPEN", Checks: "pass"})
	th, v := &th0, &View{}
	if _, err := apply(t, "thread.settle", th, v, nil, at(1)); err != nil {
		t.Fatal(err)
	}
	if th.Settle.At != nil || !SettlesWhenMerged(th) {
		t.Fatalf("e with an open PR must arm settle when merged, got settled=%v armed=%v", th.Settle.At != nil, SettlesWhenMerged(th))
	}
	th1 := withPR(Thread{}, PR{State: "OPEN"})
	th2, v2 := &th1, &View{}
	if _, err := apply(t, "thread.settle", th2, v2, Args{"now": true}, at(1)); err != nil {
		t.Fatal(err)
	}
	if th2.Settle.At == nil {
		t.Fatal("bulk settle must settle now")
	}
}

// SPEC 12.3, 0.5 V0.12: the gate denies gh pr merge and names the Fyr tool.
func TestGate(t *testing.T) {
	deny := []string{"gh pr merge 12 --squash", "cd x && gh pr merge 12", "gh  pr merge", "(gh pr merge 1)"}
	allow := []string{"gh pr view 12", "gh pr create --fill", "echo gh pr merged", "git merge main", "ghx pr merge"}
	for _, c := range deny {
		if r := Gate("Bash", map[string]any{"command": c}); r == "" {
			t.Errorf("allowed %q", c)
		}
	}
	for _, c := range allow {
		if r := Gate("Bash", map[string]any{"command": c}); r != "" {
			t.Errorf("denied %q", c)
		}
	}
	if Gate("Read", map[string]any{"command": "gh pr merge"}) != "" {
		t.Error("gate applies to Bash only")
	}
}

// SPEC 5.10: approving an effect Decision yields an effect for Fyr to run.
func TestApproveEffectDecision(t *testing.T) {
	th, v := &Thread{}, &View{}
	req := &EffectReq{Action: "pr.merge", Args: map[string]any{"pr": "u", "method": "squash"}}
	DecisionOpened(th, Decision{ID: "e1", Kind: "effect", Effect: req, CreatedAt: at(0)})
	eff, err := apply(t, "thread.answer", th, v, Args{"decisionId": "e1", "allow": true}, at(1))
	if err != nil || len(eff) != 1 || eff[0].Kind != "effect" || !eff[0].Allow || eff[0].Request.Action != "pr.merge" {
		t.Fatalf("got %+v %v", eff, err)
	}
	if _, err := apply(t, "thread.answer", th, v, Args{"decisionId": "e1", "allow": true}, at(2)); err == nil {
		t.Fatal("a second approve must fail: the decision is gone")
	}
}

// SPEC 5.10: an effect Decision does not need the agent process, so it
// survives a process loss; ask and approval Decisions do not.
func TestProcessLostKeepsEffectDecisions(t *testing.T) {
	th := &Thread{}
	RunStarted(th, at(0))
	DecisionOpened(th, Decision{ID: "a", Kind: "ask", CreatedAt: at(1)})
	DecisionOpened(th, Decision{ID: "e", Kind: "effect", CreatedAt: at(1)})
	ProcessLost(th, "process", "exit", at(2))
	if len(th.Decisions) != 1 || th.Decisions[0].ID != "e" || th.Failure == nil {
		t.Fatalf("decisions %+v failure %v", th.Decisions, th.Failure)
	}
	th2 := &Thread{}
	DecisionOpened(th2, Decision{ID: "e", Kind: "effect", CreatedAt: at(1)})
	ProcessLost(th2, "process", "exit", at(2))
	if th2.Failure != nil || len(th2.Decisions) != 1 {
		t.Fatal("an idle thread with only an effect Decision lost nothing and must not fail")
	}
}

// SPEC 0.5 V0.2, 6.4: markSeen up to what the client showed; a later result stays done.
func TestMarkSeenUpTo(t *testing.T) {
	th, v := &Thread{ActivityAt: at(5)}, &View{}
	apply(t, "thread.markSeen", th, v, Args{"upTo": at(3).Format(time.RFC3339Nano)}, at(6))
	if Compute(th, v, at(6)).State != Done {
		t.Fatal("a result newer than upTo was marked seen")
	}
	apply(t, "thread.markSeen", th, v, Args{"upTo": at(5).Format(time.RFC3339Nano)}, at(6))
	if Compute(th, v, at(6)).State != Seen {
		t.Fatal("upTo equal to activityAt must mark seen")
	}
	th2, v2 := &Thread{ActivityAt: at(5)}, &View{}
	apply(t, "thread.markSeen", th2, v2, Args{"upTo": at(90).Format(time.RFC3339Nano)}, at(6))
	if !v2.SeenAt.Equal(at(6)) {
		t.Fatalf("upTo in the future must clamp to now, got %v", v2.SeenAt)
	}
}

// SPEC 0.5 V0.16: a thread whose turn ended while its agents run is waiting,
// in Working; it becomes done when they end.
func TestBackgroundTasksMakeWaiting(t *testing.T) {
	th, v := &Thread{ActivityAt: at(1)}, &View{SeenAt: at(0)}
	TaskObserved(th, Task{ID: "a1", Description: "Design mobile", Status: "running"}, at(1))
	if a := Compute(th, v, at(2)); a.State != Waiting || a.Section != WorkingSec || a.Reason != "1 agent" {
		t.Fatalf("got %+v", a)
	}
	TaskObserved(th, Task{ID: "a1", Status: "completed", Activity: "Delivered"}, at(3))
	if a := Compute(th, v, at(4)); a.State != Done {
		t.Fatalf("got %+v", a)
	}
	if th.Tasks[0].Description != "Design mobile" || th.Tasks[0].Activity != "Delivered" {
		t.Fatalf("update lost fields: %+v", th.Tasks[0])
	}
	RunStarted(th, at(5))
	TaskObserved(th, Task{ID: "a2", Status: "running"}, at(5))
	if a := Compute(th, v, at(6)); a.State != Working {
		t.Fatal("a live turn is working, not waiting")
	}
	ProcessLost(th, "process", "exit", at(7))
	for _, k := range th.Tasks {
		if k.Status == "running" {
			t.Fatal("a running task survived the process")
		}
	}
}

// SPEC 0.5 V0.36: a merged or closed PR can be dismissed while the thread goes
// on; polling or a later link never brings it back; a new PR attaches as usual.
func TestDismissPR(t *testing.T) {
	a := Registry["thread.dismissPR"]
	if a == nil {
		t.Fatal("no thread.dismissPR action")
	}
	th := &Thread{}
	v := &View{}
	PRObserved(th, PR{URL: "https://github.com/o/r/pull/15", State: "OPEN"}, at(1))
	if why := a.Available(th, v, at(1)); why == "" {
		t.Fatal("an open PR can be dismissed")
	}
	PRObserved(th, PR{URL: "https://github.com/o/r/pull/15", State: "MERGED"}, at(2))
	if why := a.Available(th, v, at(2)); why != "" {
		t.Fatalf("a merged PR cannot be dismissed: %s", why)
	}
	if _, err := a.Apply(th, v, nil, at(3)); err != nil {
		t.Fatal(err)
	}
	if th.MainPR() != nil {
		t.Fatal("the PR stayed")
	}
	PRObserved(th, PR{URL: "https://github.com/o/r/pull/15", State: "MERGED"}, at(4))
	if th.MainPR() != nil {
		t.Fatal("polling brought the dismissed PR back")
	}
	PRObserved(th, PR{URL: "https://github.com/o/r/pull/16", State: "OPEN"}, at(5))
	if th.MainPR() == nil || th.MainPR().URL != "https://github.com/o/r/pull/16" {
		t.Fatalf("a new PR did not attach: %+v", th.MainPR())
	}
	if a.Available(&Thread{}, v, at(6)) == "" {
		t.Fatal("a thread without a PR offers Dismiss PR")
	}
}
