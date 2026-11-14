package core

import (
	"math/rand"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)

func at(min int) time.Time       { return t0.Add(time.Duration(min) * time.Minute) }
func ptr(t time.Time) *time.Time { return &t }

func TestAttentionTable(t *testing.T) {
	ask := []Decision{{ID: "d1", Kind: "ask", CreatedAt: at(5)}}
	fail := &Failure{Class: "api", Message: "API error", At: at(5)}
	openFail := &PR{State: "OPEN", Checks: "fail"}
	cases := []struct {
		name string
		th   Thread
		v    View
		want State
	}{
		{"new thread never seen is done", Thread{ActivityAt: at(1)}, View{}, Done},
		{"seen after activity", Thread{ActivityAt: at(1)}, View{SeenAt: at(2)}, Seen},
		{"activity after seen is done", Thread{ActivityAt: at(3)}, View{SeenAt: at(2)}, Done},
		{"running is working", Thread{Running: true, ActivityAt: at(3)}, View{}, Working},
		{"asking beats running", Thread{Running: true, Decisions: ask}, View{SeenAt: at(9)}, Asking},
		{"failed stays after a visit", Thread{Failure: fail}, View{SeenAt: at(9)}, Failed},
		{"asking beats failed", Thread{Failure: fail, Decisions: ask}, View{}, Asking},
		{"blocked PR when idle", Thread{PR: openFail}, View{SeenAt: at(9)}, Blocked},
		{"blocked PR hidden while working", Thread{PR: openFail, Running: true}, View{}, Working},
		{"merged PR does not block", Thread{PR: &PR{State: "MERGED", Checks: "fail"}}, View{SeenAt: at(9)}, Seen},
		{"settled beats asking", Thread{SettledAt: ptr(at(1)), Decisions: ask}, View{}, Settled},
		{"archived beats all", Thread{ArchivedAt: ptr(at(1)), SettledAt: ptr(at(1))}, View{}, Archived},
		{"snoozed hides done", Thread{ActivityAt: at(3)}, View{SnoozeUntil: ptr(at(60)), SnoozedAt: ptr(at(4))}, Snoozed},
		{"snooze expired", Thread{ActivityAt: at(3)}, View{SnoozeUntil: ptr(at(6)), SnoozedAt: ptr(at(4)), SeenAt: at(4)}, Seen},
		{"new question wakes snooze", Thread{Decisions: ask}, View{SnoozeUntil: ptr(at(60)), SnoozedAt: ptr(at(4))}, Asking},
		{"old question stays snoozed", Thread{Decisions: ask}, View{SnoozeUntil: ptr(at(60)), SnoozedAt: ptr(at(6))}, Snoozed},
		{"new failure wakes snooze", Thread{Failure: fail}, View{SnoozeUntil: ptr(at(60)), SnoozedAt: ptr(at(4))}, Failed},
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
	if th.SettledAt != nil || !th.SettleWhenDone {
		t.Fatal("settle on a running thread must defer")
	}
	RunEnded(th, nil, at(2))
	if Compute(th, v, at(3)).State != Settled {
		t.Fatal("thread did not settle when the run ended")
	}
}

func TestFailedRunDoesNotSettle(t *testing.T) {
	th, v := &Thread{}, &View{}
	RunStarted(th, at(0))
	apply(t, "thread.settle", th, v, nil, at(1))
	RunEnded(th, &Failure{Class: "api", Message: "x", At: at(2)}, at(2))
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
	th, v := &Thread{PR: &PR{State: "OPEN", Checks: "pass"}}, &View{}
	if _, err := apply(t, "thread.settleWhenMerged", th, v, nil, at(1)); err != nil {
		t.Fatal(err)
	}
	PRObserved(th, PR{State: "MERGED", Checks: "pass"}, at(2))
	if Compute(th, v, at(3)).State != Settled {
		t.Fatal("merge did not settle")
	}
}

func TestPollingDoesNotMarkDone(t *testing.T) {
	th, v := &Thread{ActivityAt: at(1), PR: &PR{State: "OPEN", Checks: "pending"}}, &View{SeenAt: at(2)}
	PRObserved(th, PR{State: "OPEN", Checks: "pass"}, at(3))
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
				checks := []string{"pass", "fail", "pending"}
				PRObserved(th, PR{State: states[rng.Intn(3)], Checks: checks[rng.Intn(3)]}, now)
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
			if th.Failure != nil && th.ArchivedAt == nil && th.SettledAt == nil && len(th.Decisions) == 0 && a.State != Failed && a.State != Snoozed {
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
