package core

import (
	"errors"
	"testing"
	"time"
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

// SPEC V0.trans-host-handover, decision 0039: HandoverDone with HostNotice
// queues a Next notice so a resumed cross-host session learns about the host
// move on its next turn without waking an idle agent.
func TestHandoverDoneQueuesHostNotice(t *testing.T) {
	k := Kinds[KindHandoverDone]
	th := Thread{Profile: "a"}
	res, err := k.Reduce(Rec{T: &th, V: &View{}}, Input{
		Kind: KindHandoverDone,
		Args: HandoverDoneArgs{Profile: "b", Model: "m", SessionID: "s1", HostNotice: "Host handover: moved from host-a to host-b."},
	}, at(1))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Effects) != 0 {
		t.Fatalf("unexpected effects (must not wake idle resumed agent): %+v", res.Effects)
	}
	if len(th.Feed.Queue) != 1 || th.Feed.Queue[0].Urgency != Next || th.Feed.Queue[0].What != "Host handover: moved from host-a to host-b." {
		t.Fatalf("feed queue: %+v", th.Feed.Queue)
	}
}

// SPEC V0.trans-host-handover, decision 0039: thread.handedOver records the
// target link, appends a handover divider event, and settles the source thread.
func TestThreadHandedOverSettlesSourceAndLinks(t *testing.T) {
	th := &Thread{ID: "t_src", Profile: "claude", Conversation: Conversation{Failure: &Failure{Class: "api", Message: "err"}}}
	v := &View{User: Owner, Entity: "t_src"}
	next, nextV, res, err := Reduce(th, v, Input{
		Source: FromOwner,
		Kind:   KindThreadHandedOver,
		Thread: "t_src",
		Args: Args{
			"fromHostId":   "HOST-A",
			"fromHostName": "host-a",
			"toHostId":     "HOST-B",
			"toHostName":   "host-b",
			"toThreadId":   "t_dst",
			"toProfile":    "claude-b",
			"toModel":      "opus",
			"toCwd":        "/b/fyr",
			"rung":         "resume",
		},
	}, at(5))
	if err != nil {
		t.Fatal(err)
	}
	if next.HandedOverTo == nil || next.HandedOverTo.HostID != "HOST-B" || next.HandedOverTo.ThreadID != "t_dst" {
		t.Fatalf("handedOverTo: %+v", next.HandedOverTo)
	}
	if next.Settle.At == nil || next.Failure != nil || !nextV.SeenAt.Equal(at(5)) {
		t.Fatalf("expected settled source: settle=%v failure=%v seen=%v", next.Settle.At, next.Failure, nextV.SeenAt)
	}
	if len(res.Events) != 1 || res.Events[0].Type != "handover" {
		t.Fatalf("events: %+v", res.Events)
	}
	if _, _, _, err := Reduce(th, v, Input{Source: FromOwner, Kind: KindThreadHandedOver, Thread: "t_src", Args: Args{}}, at(5)); err == nil {
		t.Fatal("expected error without toHostId/toThreadId")
	}
}

// SPEC V0.trans-host-handover, decision 0039: thread.handedIn creates the
// target thread with its copied conversation history and handedOverFrom link.
func TestThreadHandedInCreatesTargetWithHistory(t *testing.T) {
	src := &Thread{
		Title:          "Fix auth",
		Cwd:            "/b/fyr",
		Profile:        "claude",
		HandedOverFrom: &HandoverLink{HostID: "HOST-A", HostName: "host-a", ThreadID: "t_src"},
	}
	v := &View{User: Owner, Entity: "t_dst", SeenAt: at(2)}
	hist := []Event{{Type: "user", Data: map[string]any{"text": "hi"}}}
	next, _, res, err := Reduce(&Thread{ID: "t_dst"}, nil, Input{
		Source: Observer(ObsHost),
		Kind:   KindThreadHandedIn,
		Thread: "t_dst",
		Args:   Adopted{Thread: src, View: v, History: hist, At: []time.Time{at(1)}},
	}, at(2))
	if err != nil {
		t.Fatal(err)
	}
	if next.ID != "t_dst" || next.HandedOverFrom == nil || next.HandedOverFrom.HostID != "HOST-A" {
		t.Fatalf("target thread: %+v", next)
	}
	if len(res.Events) != 1 || res.Events[0].Type != "user" {
		t.Fatalf("copied history: %+v", res.Events)
	}
}
