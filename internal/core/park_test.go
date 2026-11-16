package core

import (
	"strings"
	"testing"
	"time"
)

// parkThread is a thread whose only change is the agent's, waiting for the
// owner (the live case of V0.84: a GitHub App only the owner can make).
func parkThread(t *testing.T, now time.Time) *Thread {
	t.Helper()
	th := &Thread{ID: "t1"}
	OpenChange(th, ChangeOpen{Key: "hosting:1463-rulesets", Kind: KindInfra, Title: "Rulesets"}, ByAgent, now)
	owner, label := "owner", "needs a GitHub App with Administration write"
	th, _, _, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeStatus, Thread: "t1",
		Args: ChangeStatusArgs{Change: "hosting:1463-rulesets", Update: ChangeUpdate{Needs: &owner, Label: &label}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if th.Changes[0].Blocking() == "" {
		t.Fatal("setup: the change does not block")
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
	x, _, _, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeStatus, Thread: "t1",
		Args: ChangeStatusArgs{Change: "hosting:1463-rulesets", Update: u}}, now)
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
	if r := c.Blocking(); r != "" {
		t.Fatalf("a parked change blocks: %q", r)
	}
	if r := ChangeBlocking(th); r != "" {
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
	// Undo: it blocks again.
	y, err := park(t, th, FromOwner, KindChangeUnpark, now)
	if err != nil {
		t.Fatal(err)
	}
	if y.Changes[0].Parked != nil || y.Changes[0].Blocking() == "" {
		t.Fatalf("undo: %+v", y.Changes[0])
	}
	if _, err := park(t, y, FromAgent, KindChangeUnpark, now); err == nil {
		t.Fatal("the agent sent the owner's undo")
	}
	// A change that needs nobody has nothing to park.
	z := status(t, y, ChangeUpdate{Needs: new(string)}, now)
	if _, err := park(t, z, FromOwner, KindChangePark, now); err == nil {
		t.Fatal("parked a change that does not need the owner")
	}
}

// V0.84 (3): the park holds only for what the owner saw. A new reason, a
// new condition, a new phase or a new revision brings it back by itself,
// and parked is cleared so the agent and the client see it.
func TestParkExpiresOnANewFact(t *testing.T) {
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	later := now.Add(time.Minute)
	parked := func() *Thread {
		th, err := park(t, parkThread(t, now), FromOwner, KindChangePark, now)
		if err != nil {
			t.Fatal(err)
		}
		return th
	}
	other := "needs the App and a token"
	for name, step := range map[string]func(*Thread) *Thread{
		"reason": func(th *Thread) *Thread { return status(t, th, ChangeUpdate{Label: &other}, later) },
		"condition": func(th *Thread) *Thread {
			return status(t, th, ChangeUpdate{Conditions: []Condition{{Type: "apply", Status: CondFail, Reason: "403"}}}, later)
		},
		"phase": func(th *Thread) *Thread { return status(t, th, ChangeUpdate{Phase: PhaseReview}, later) },
		"revision": func(th *Thread) *Thread {
			x, _, _, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeRevise, Thread: "t1",
				Args: ChangeReviseArgs{Change: "hosting:1463-rulesets", Revision: Revision{ID: "abc123"}}}, later)
			if err != nil {
				t.Fatal(err)
			}
			return x
		},
	} {
		th := step(parked())
		c := &th.Changes[0]
		if c.Parked != nil {
			t.Errorf("%s: still parked", name)
		}
		if c.Blocking() == "" {
			t.Errorf("%s: does not block again", name)
		}
		if a := Compute(th, &View{SeenAt: later.Add(time.Hour)}, later); a.Section != NeedsYou {
			t.Errorf("%s: the thread is not back in Needs you: %+v", name, a)
		}
	}
	// The same report again is no new fact: the park holds.
	th := parked()
	same := "needs a GitHub App with Administration write"
	owner := "owner"
	th = status(t, th, ChangeUpdate{Needs: &owner, Label: &same}, later)
	if th.Changes[0].Parked == nil || th.Changes[0].Blocking() != "" {
		t.Fatalf("the same report unparked it: %+v", th.Changes[0])
	}
}

// V0.84 (2): a failed condition can be parked too, and the thread with
// another blocking change still needs the owner.
func TestParkOnlyItsChange(t *testing.T) {
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	th := parkThread(t, now)
	OpenChange(th, ChangeOpen{Key: "doc:x", Title: "Doc"}, ByAgent, now)
	th, _, _, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeStatus, Thread: "t1",
		Args: ChangeStatusArgs{Change: "doc:x", Update: ChangeUpdate{Conditions: []Condition{{Type: "lint", Status: CondFail, Reason: "2 errors"}}}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	th, err = park(t, th, FromOwner, KindChangePark, now)
	if err != nil {
		t.Fatal(err)
	}
	if r := ChangeBlocking(th); !strings.Contains(r, "2 errors") {
		t.Fatalf("the other change no longer blocks: %q", r)
	}
	th, _, _, err = Reduce(th, &View{}, Input{Source: FromOwner, Kind: KindChangePark, Thread: "t1", Args: map[string]any{"change": "doc:x"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if r := ChangeBlocking(th); r != "" {
		t.Fatalf("blocked with both parked: %q", r)
	}
}
