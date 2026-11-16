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
