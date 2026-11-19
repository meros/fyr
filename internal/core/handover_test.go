package core

import (
	"errors"
	"testing"
)

// SPEC 0.5 V0.45: a handover is the owner's action, only when nothing is live.
func TestHandoverAvailability(t *testing.T) {
	a := Registry["thread.handover"]
	cases := []struct {
		name string
		th   Thread
		want string
	}{
		{"idle", Thread{Profile: "a"}, ""},
		{"running", Thread{Conversation: Conversation{Running: true}}, "Wait for the run to end"},
		{"asking", Thread{Decisions: []Decision{{ID: "d", Kind: "ask"}}}, "Answer the open question first"},
		{"archived", Thread{ArchivedAt: ptr(at(1))}, "The thread is archived"},
		{"going", Thread{Conversation: Conversation{Handover: &Handover{To: "b"}}}, "A handover is already going"},
	}
	for _, c := range cases {
		if got := a.Available(&c.th, &View{}, at(2)); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestHandoverApply(t *testing.T) {
	a := Registry["thread.handover"]
	th := Thread{Profile: "a"}
	if _, err := a.Apply(&th, &View{}, Args{"profile": "a"}, at(1)); !errors.Is(err, ErrBadArgs) {
		t.Fatalf("own profile: %v", err)
	}
	if _, err := a.Apply(&th, &View{}, Args{}, at(1)); !errors.Is(err, ErrBadArgs) {
		t.Fatalf("no profile: %v", err)
	}
	effs, err := a.Apply(&th, &View{}, Args{"profile": "b", "model": "m"}, at(1))
	if err != nil || len(effs) != 1 || effs[0].Kind != "handover" || effs[0].Profile != "b" || effs[0].Model != "m" {
		t.Fatalf("effects %+v %v", effs, err)
	}
	if th.Handover == nil || th.Handover.To != "b" || th.Profile != "a" {
		t.Fatalf("thread %+v", th)
	}
	// Sends wait while it goes.
	if r := Registry["thread.send"].Available(&th, &View{}, at(2)); r != "Wait for the handover to finish" {
		t.Fatalf("send: %q", r)
	}
}

// V0.model-choice: Hand over carries the effort picked for the target; without one the thread keeps its own.
func TestHandoverEffort(t *testing.T) {
	a := Registry["thread.handover"]
	th := Thread{Profile: "a", Effort: "max"}
	effs, err := a.Apply(&th, &View{}, Args{"profile": "b", "model": "m", "effort": "high"}, at(1))
	if err != nil || effs[0].Effort == nil || *effs[0].Effort != "high" || th.Handover.Effort == nil {
		t.Fatalf("effects %+v %v", effs, err)
	}
	th2 := Thread{Profile: "a", Effort: "max"}
	effs, _ = a.Apply(&th2, &View{}, Args{"profile": "b"}, at(1))
	if effs[0].Effort != nil {
		t.Fatalf("no effort asked, effect has %q", *effs[0].Effort)
	}
}

// V0.model-choice: a finished handover sets the thread's effort when one was picked, and keeps it otherwise.
func TestHandoverDoneEffort(t *testing.T) {
	k := Kinds[KindHandoverDone]
	high := "high"
	th := Thread{Profile: "a", Effort: "max"}
	if _, err := k.Reduce(Rec{T: &th, V: &View{}}, Input{Kind: KindHandoverDone, Args: HandoverDoneArgs{Profile: "b", Model: "m", Effort: &high}}, at(1)); err != nil || th.Effort != "high" || th.Profile != "b" {
		t.Fatalf("%v %+v", err, th)
	}
	th2 := Thread{Profile: "a", Effort: "max"}
	if _, err := k.Reduce(Rec{T: &th2, V: &View{}}, Input{Kind: KindHandoverDone, Args: HandoverDoneArgs{Profile: "b", Model: "m"}}, at(1)); err != nil || th2.Effort != "max" {
		t.Fatalf("%v %+v", err, th2)
	}
}
