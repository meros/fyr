package core

import "testing"

// V0.settle-offer-your-move: a settle offer puts the thread in Your move once
// its run ended, as the last cause; a settle clears it.
func TestSettleOfferIsYourMove(t *testing.T) {
	th := &Thread{ID: "t", Title: "x"}
	th.Running = true
	th, _, _, err := Reduce(th, nil, Input{Source: FromAgent, Kind: KindSettleRequest, Thread: th.ID, Args: map[string]any{"why": "PR #12 merged"}}, at(1))
	if err != nil {
		t.Fatal(err)
	}
	v := &View{SeenAt: at(9)}
	if a := Compute(th, v, at(10)); a.State != Working {
		t.Fatalf("running: %+v", a)
	}
	th.Running = false
	if a := Compute(th, v, at(10)); a.State != Blocked || a.Section != NeedsYou || a.Reason != "Settle: PR #12 merged" {
		t.Fatalf("offer: %+v", a)
	}
	// A failure is the owner's move first.
	th.Failure = &Failure{Message: "boom", At: at(2)}
	if a := Compute(th, v, at(10)); a.State != Failed {
		t.Fatalf("failure first: %+v", a)
	}
	th.Failure = nil
	if got := SettleOfferText(&SettleAsk{}); got != "Settle: the agent is done" {
		t.Fatalf("no why: %q", got)
	}
	settleNow(th, at(11))
	if a := Compute(th, v, at(12)); a.State != Settled {
		t.Fatalf("settled: %+v", a)
	}
}
