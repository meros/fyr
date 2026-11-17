package core

import (
	"errors"
	"testing"
	"time"
)

// Pin: the owner pins and unpins; the agent cannot.
func TestPinIsTheOwners(t *testing.T) {
	th, v := &Thread{ID: "t_1", ActivityAt: ct0}, &View{SeenAt: ct0}
	if r := Registry["thread.unpin"].Available(th, v, ct0); r != "Not pinned" {
		t.Fatalf("unpin of an unpinned thread: %q", r)
	}
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.pin"}, ct0)
	if th.PinnedAt == nil || !th.PinnedAt.Equal(ct0) {
		t.Fatalf("pinnedAt %v", th.PinnedAt)
	}
	if r := Registry["thread.pin"].Available(th, v, ct0); r != "Already pinned" {
		t.Fatalf("pin of a pinned thread: %q", r)
	}
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.unpin"}, ct0.Add(time.Minute))
	if th.PinnedAt != nil {
		t.Fatal("unpin left the pin")
	}
	_, _, _, err := Reduce(th, v, Input{Source: FromAgent, Kind: "thread.pin", Thread: "t_1"}, ct0)
	var r *Rejection
	if !errors.As(err, &r) || th.PinnedAt != nil {
		t.Fatalf("the agent pinned: %v", err)
	}
}

// Snooze, settle and archive remove the pin; unsnooze, unsettle and a new
// run do not bring it back.
func TestHidingDropsThePin(t *testing.T) {
	pinned := func() (*Thread, *View) {
		th, v := &Thread{ID: "t_1", ActivityAt: ct0}, &View{SeenAt: ct0}
		step(t, th, v, Input{Source: FromOwner, Kind: "thread.pin"}, ct0)
		return th, v
	}
	th, v := pinned()
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.snooze", Args: Args{"until": ct0.Add(time.Hour).Format(time.RFC3339)}}, ct0)
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.unsnooze"}, ct0)
	if th.PinnedAt != nil {
		t.Fatal("snooze kept the pin")
	}

	th, v = pinned()
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.settle"}, ct0)
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.unsettle"}, ct0)
	if th.PinnedAt != nil {
		t.Fatal("settle kept the pin")
	}

	// Settle when done: the pin stays while the run goes on and leaves when
	// the thread settles.
	th, v = pinned()
	RunStarted(th, ct0)
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.settle"}, ct0)
	if th.PinnedAt == nil {
		t.Fatal("a deferred settle dropped the pin before it settled")
	}
	RunEnded(th, nil, ct0.Add(time.Minute))
	rules(th, ct0.Add(time.Minute))
	if th.Settle.At == nil || th.PinnedAt != nil {
		t.Fatalf("settled %v, pinned %v", th.Settle.At, th.PinnedAt)
	}

	th, v = pinned()
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.archive"}, ct0)
	if th.PinnedAt != nil {
		t.Fatal("archive kept the pin")
	}
}

// V0.106: a pinned thread never settles by itself. The agent's settle_when
// and its watch with then "settle" stay recorded but do not fire while the
// pin holds; unpin drops them. The owner's Settle still settles it, and
// removes the pin.
func TestPinnedNeverAutoSettles(t *testing.T) {
	th, v := &Thread{ID: "t_1", ActivityAt: ct0}, &View{SeenAt: ct0}
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.pin"}, ct0)
	RunStarted(th, ct0)
	step(t, th, v, Input{Source: FromAgent, Kind: KindSettleWhen, Thread: "t_1", Args: Args{"condition": "done"}}, ct0)
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Thread: "t_1", Args: RuleArmArgs{ID: "w_idle", When: When{Kind: WhenIdleFor, Ms: 60_000}, Then: "settle"}}, ct0)
	RunEnded(th, nil, ct0.Add(time.Minute))
	step(t, th, v, Input{Source: Observer(ObsClock), Kind: KindClock}, ct0.Add(time.Hour))
	if th.Settle.At != nil || th.PinnedAt == nil {
		t.Fatalf("a pinned thread settled by itself: settled %v, pinned %v", th.Settle.At, th.PinnedAt)
	}
	if !SettlesWhenDone(th) || FindRule(th, "w_idle") == nil {
		t.Fatalf("the agent's settle rules are not kept while pinned: %+v", th.Rules)
	}
	if r := Registry["thread.settle"].Available(th, v, ct0.Add(time.Hour)); r != "" {
		t.Fatalf("the owner cannot settle a pinned thread with the agent's rule armed: %q", r)
	}

	// Unpin drops the agent's settle rules: nothing settles the thread later.
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.unpin"}, ct0.Add(time.Hour))
	step(t, th, v, Input{Source: Observer(ObsClock), Kind: KindClock}, ct0.Add(2*time.Hour))
	if th.Settle.At != nil || Settling(th) {
		t.Fatalf("unpin kept the agent's settle rules: settled %v, %+v", th.Settle.At, th.Rules)
	}

	// The owner's Settle press settles a pinned thread and drops the pin,
	// also when it waits for the run to end.
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.pin"}, ct0.Add(2*time.Hour))
	RunStarted(th, ct0.Add(2*time.Hour))
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.settle"}, ct0.Add(2*time.Hour))
	RunEnded(th, nil, ct0.Add(3*time.Hour))
	step(t, th, v, Input{Source: Observer(ObsClock), Kind: KindClock}, ct0.Add(3*time.Hour))
	if th.Settle.At == nil || th.PinnedAt != nil {
		t.Fatalf("the owner's settle: settled %v, pinned %v", th.Settle.At, th.PinnedAt)
	}
}

// V0.162: a bulk settle (Settle merged, ⇧E, the phone's Settle N) skips a
// pinned thread: it is refused, nothing changes, the pin stays. A running
// pinned thread gets no "settle when done" either. The owner's Settle on
// the thread itself still settles it.
func TestBulkSettleSkipsAPinnedThread(t *testing.T) {
	for _, running := range []bool{false, true} {
		th, v := &Thread{ID: "t_1", ActivityAt: ct0}, &View{SeenAt: ct0}
		step(t, th, v, Input{Source: FromOwner, Kind: "thread.pin"}, ct0)
		if running {
			RunStarted(th, ct0)
		}
		_, _, _, err := Reduce(th, v, Input{Source: FromOwner, Kind: "thread.settle", Thread: "t_1", Args: Args{"now": true}}, ct0)
		var r *Rejection
		if !errors.As(err, &r) || r.Code != RejectConflict {
			t.Fatalf("running %v: a bulk settle of a pinned thread: %v", running, err)
		}
		if th.PinnedAt == nil || th.Settle.At != nil || Settling(th) {
			t.Fatalf("running %v: pinned %v, settled %v, rules %+v", running, th.PinnedAt, th.Settle.At, th.Rules)
		}
	}
	th, v := &Thread{ID: "t_1", ActivityAt: ct0}, &View{SeenAt: ct0}
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.pin"}, ct0)
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.settle"}, ct0)
	if th.Settle.At == nil || th.PinnedAt != nil {
		t.Fatalf("the owner's own Settle: settled %v, pinned %v", th.Settle.At, th.PinnedAt)
	}
}
