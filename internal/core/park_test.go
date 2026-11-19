package core

import (
	"strings"
	"testing"
	"time"
)

// parkThread is a thread whose only change is the agent's, failed and
// waiting for the owner (the live case of V0.84: a GitHub App only the
// owner can make). The thread never ran here, so the failure is the
// owner's at once (V0.101).
func parkThread(t *testing.T, now time.Time) *Thread {
	t.Helper()
	th := &Thread{ID: "t1"}
	OpenChange(th, ChangeOpen{Key: "hosting:1463-rulesets", Kind: KindInfra, Title: "Rulesets"}, ByAgent, now)
	label := "needs a GitHub App with Administration write"
	th, _, _, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeUpdate, Thread: "t1",
		Args: ChangeUpdateArgs{Change: "hosting:1463-rulesets", Update: ChangeUpdate{Label: &label, Conditions: []Condition{{Type: "apply", Status: CondFail, Reason: "no App"}}}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if n := ChangeNeed(th, &th.Changes[0]); n == nil || n.Cause != CauseFailure {
		t.Fatalf("setup: the change is no cause: %+v", n)
	}
	return th
}

func park(t *testing.T, th *Thread, src Source, kind string, now time.Time) (*Thread, error) {
	t.Helper()
	x, _, _, err := Reduce(th, &View{}, Input{Source: src, Kind: kind, Thread: "t1", Args: map[string]any{"change": "hosting:1463-rulesets"}}, now)
	return x, err
}

func status(t *testing.T, th *Thread, u ChangeUpdate, now time.Time) *Thread {
	t.Helper()
	x, _, _, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeUpdate, Thread: "t1",
		Args: ChangeUpdateArgs{Change: "hosting:1463-rulesets", Update: u}}, now)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

// V0.84 (1), (2): the owner parks a change that needs them; it stops
// blocking, the thread leaves Needs you, and the agent sees it. An agent
// cannot park, and a change that needs nobody cannot be parked.
func TestOwnerParksAChange(t *testing.T) {
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	th := parkThread(t, now)
	if a := Compute(th, &View{SeenAt: now.Add(time.Hour)}, now); a.Section != NeedsYou {
		t.Fatalf("setup: %+v", a)
	}
	if _, err := park(t, th, FromAgent, KindChangePark, now); err == nil {
		t.Fatal("the agent parked a change with the owner's command")
	}
	x, err := park(t, th, FromOwner, KindChangePark, now)
	if err != nil {
		t.Fatal(err)
	}
	th = x
	c := &th.Changes[0]
	if c.Parked == nil || !c.Parked.At.Equal(now) {
		t.Fatalf("not parked: %+v", c.Parked)
	}
	if r := needText(th); r != "" {
		t.Fatalf("the thread is blocked: %q", r)
	}
	if a := Compute(th, &View{SeenAt: now.Add(time.Hour)}, now); a.Section == NeedsYou {
		t.Fatalf("the thread stays in Needs you: %+v", a)
	}
	if txt := NoticeText(th.Feed.Queue); !strings.Contains(txt, "Not now") {
		t.Fatalf("the agent is not told: %q", txt)
	}
	st, err := StateText(th, "changes", now)
	if err != nil || !strings.Contains(st, "the owner parked it") {
		t.Fatalf("fyr_state does not say it: %q %v", st, err)
	}
	if _, err := park(t, th, FromOwner, KindChangePark, now); err == nil {
		t.Fatal("parked twice")
	}
	// Undo: it needs the owner again.
	y, err := park(t, th, FromOwner, KindChangeUnpark, now)
	if err != nil {
		t.Fatal(err)
	}
	if y.Changes[0].Parked != nil || ChangeNeed(y, &y.Changes[0]) == nil {
		t.Fatalf("undo: %+v", y.Changes[0])
	}
	if _, err := park(t, y, FromAgent, KindChangeUnpark, now); err == nil {
		t.Fatal("the agent sent the owner's undo")
	}
	// A change that needs nobody has nothing to park.
	z := status(t, y, ChangeUpdate{Conditions: []Condition{{Type: "apply", Status: CondOK}}}, now)
	if _, err := park(t, z, FromOwner, KindChangePark, now); err == nil {
		t.Fatal("parked a change that does not need the owner")
	}
}

// V0.84 (3), V0.101 (2): the park holds until the head changes (here the
// newest revision) or a new failure type appears; a new reason, label or
// phase does not bring it back, and parked is cleared when it comes back.
func TestParkExpiresOnANewHead(t *testing.T) {
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	later := now.Add(time.Minute)
	parked := func() *Thread {
		th, err := park(t, parkThread(t, now), FromOwner, KindChangePark, now)
		if err != nil {
			t.Fatal(err)
		}
		return th
	}
	for name, step := range map[string]func(*Thread) *Thread{
		"failure type": func(th *Thread) *Thread {
			return status(t, th, ChangeUpdate{Conditions: []Condition{{Type: "health", Status: CondFail, Reason: "403"}}}, later)
		},
		"revision": func(th *Thread) *Thread {
			x, _, _, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeUpdate, Thread: "t1",
				Args: ChangeUpdateArgs{Change: "hosting:1463-rulesets", Revision: &Revision{ID: "abc123"}}}, later)
			if err != nil {
				t.Fatal(err)
			}
			// A new revision sets the conditions back to pending; the agent
			// reports the failure again on it.
			return status(t, x, ChangeUpdate{Conditions: []Condition{{Type: "apply", Status: CondFail, Reason: "no App"}}}, later)
		},
	} {
		th := step(parked())
		c := &th.Changes[0]
		if c.Parked != nil {
			t.Errorf("%s: still parked", name)
		}
		if a := Compute(th, &View{SeenAt: later.Add(time.Hour)}, later); a.Section != NeedsYou {
			t.Errorf("%s: the thread is not back in Needs you: %+v", name, a)
		}
	}
	other := "needs the App and a token"
	for name, u := range map[string]ChangeUpdate{
		"label":  {Label: &other},
		"reason": {Conditions: []Condition{{Type: "apply", Status: CondFail, Reason: "403"}}},
		"phase":  {Phase: PhaseReview},
	} {
		th := status(t, parked(), u, later)
		if th.Changes[0].Parked == nil || ChangeNeed(th, &th.Changes[0]) != nil {
			t.Errorf("%s: back before the head changed: %+v", name, th.Changes[0].Parked)
		}
	}
}

// V0.84 (2): a failed condition can be parked, and the thread with
// another failed change still needs the owner.
func TestParkOnlyItsChange(t *testing.T) {
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	th := parkThread(t, now)
	OpenChange(th, ChangeOpen{Key: "doc:x", Title: "Doc"}, ByAgent, now)
	th, _, _, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeUpdate, Thread: "t1",
		Args: ChangeUpdateArgs{Change: "doc:x", Update: ChangeUpdate{Conditions: []Condition{{Type: "lint", Status: CondFail, Reason: "2 errors"}}}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	th, err = park(t, th, FromOwner, KindChangePark, now)
	if err != nil {
		t.Fatal(err)
	}
	if r := needText(th); !strings.Contains(r, "2 errors") {
		t.Fatalf("the other change no longer needs the owner: %q", r)
	}
	th, _, _, err = Reduce(th, &View{}, Input{Source: FromOwner, Kind: KindChangePark, Thread: "t1", Args: map[string]any{"change": "doc:x"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if r := needText(th); r != "" {
		t.Fatalf("blocked with both parked: %q", r)
	}
}
