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
		{"running", Thread{Running: true}, "Wait for the run to end"},
		{"asking", Thread{Decisions: []Decision{{ID: "d", Kind: "ask"}}}, "Answer the open question first"},
		{"archived", Thread{ArchivedAt: ptr(at(1))}, "The thread is archived"},
		{"going", Thread{Handover: &Handover{To: "b"}}, "A handover is already going"},
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
