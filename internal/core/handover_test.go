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

// V0.handover-first-message-starting: a handover that is followed by Fyr's
// first message to the new session holds the thread "starting", so a settle
// Rule that waits for the work to be done does not settle it in the same
// commit and the message does not start a run on a settled thread
// (thread-settled-run-started).
func TestHandoverDoneWithFirstMessageDoesNotSettle(t *testing.T) {
	now := at(1)
	th := &Thread{ID: "t1", Profile: "a", Conversation: Conversation{Handover: &Handover{}}}
	SettleWhenDone(th, ByOwner, at(0))
	in := Input{Source: Observer(ObsProcess), Kind: KindHandoverDone, Thread: "t1",
		Args: HandoverDoneArgs{Profile: "b", Model: "m", FirstMessage: true}}
	after, v, _, err := Reduce(th, &View{User: Owner, Entity: "t1"}, in, now)
	if err != nil {
		t.Fatal(err)
	}
	if after.Settle.At != nil {
		t.Fatalf("thread settled while the first message is on its way: %+v", after.Settle)
	}
	// The manager's message.sent then starts the run.
	sent := Input{Source: Observer(ObsProcess), Kind: KindMessageSent, Thread: "t1", Args: MessageSentArgs{ID: "m1"}}
	run := Input{Source: Observer(ObsProcess), Kind: KindRunStarted, Thread: "t1"}
	for _, x := range []Input{sent, run} {
		before := after
		var res Result
		after, v, res, err = Reduce(before, v, x, now)
		if err != nil {
			t.Fatal(err)
		}
		if vs := CheckTransition(before, v, after, v, x, res, now, nil); len(vs) > 0 {
			t.Fatalf("%s: %+v", x.Kind, vs)
		}
	}
	if !after.Running || after.Settle.At != nil {
		t.Fatalf("running=%v settle=%v", after.Running, after.Settle.At)
	}
}

// V0.handover-revives-settled: handing over a settled thread revives it so
// handover.done with FirstMessage: true never leaves Starting set on a settled
// thread (thread-settled-contradiction / thread-settled-run-started), and a
// settled thread cannot stay settled while a handover runs.
func TestHandoverOnSettledThreadRevives(t *testing.T) {
	now := at(10)
	th := &Thread{ID: "t1", Profile: "a", Settle: Settle{At: ptr(at(5))}}
	v := &View{User: Owner, Entity: "t1", SeenAt: at(5)}

	// Owner hands over the settled thread.
	hoIn := Input{Source: FromOwner, Kind: "thread.handover", Thread: "t1", Args: Args{"profile": "b", "model": "m"}}
	after, afterV, res, err := Reduce(th, v, hoIn, now)
	if err != nil {
		t.Fatalf("thread.handover on settled thread: %v", err)
	}
	if after.Settle.At != nil {
		t.Fatalf("expected thread.handover to clear settle, still set at %v", after.Settle.At)
	}
	if after.Handover == nil || after.Handover.To != "b" {
		t.Fatalf("expected Handover to be set, got %+v", after.Handover)
	}
	if vs := CheckTransition(th, v, after, afterV, hoIn, res, now, nil); len(vs) > 0 {
		t.Fatalf("thread.handover transition violations: %+v", vs)
	}
	if vs := CheckThread(after, afterV, now, nil); len(vs) > 0 {
		t.Fatalf("thread.handover state violations: %+v", vs)
	}

	// Handover finishes with a first message (rung 2/3).
	doneIn := Input{Source: Observer(ObsProcess), Kind: KindHandoverDone, Thread: "t1",
		Args: HandoverDoneArgs{Profile: "b", Model: "m", SessionID: "s2", FirstMessage: true}}
	after2, afterV2, res2, err := Reduce(after, afterV, doneIn, now.Add(time.Second))
	if err != nil {
		t.Fatalf("handover.done: %v", err)
	}
	if after2.Starting == nil || after2.Settle.At != nil {
		t.Fatalf("expected active Starting thread after handover.done, starting=%v settle=%v", after2.Starting, after2.Settle.At)
	}
	if vs := CheckTransition(after, afterV, after2, afterV2, doneIn, res2, now.Add(time.Second), nil); len(vs) > 0 {
		t.Fatalf("handover.done transition violations: %+v", vs)
	}
	if vs := CheckThread(after2, afterV2, now.Add(time.Second), nil); len(vs) > 0 {
		t.Fatalf("handover.done state violations: %+v", vs)
	}

	// Defense in depth: handover.done on an asleep thread never sets Starting.
	settled := &Thread{ID: "t2", Profile: "a", Settle: Settle{At: ptr(at(5))}}
	sv := &View{User: Owner, Entity: "t2", SeenAt: at(5)}
	done2 := Input{Source: Observer(ObsProcess), Kind: KindHandoverDone, Thread: "t2",
		Args: HandoverDoneArgs{Profile: "b", Model: "m", SessionID: "s2", FirstMessage: true}}
	sAfter, sAfterV, sRes, err := Reduce(settled, sv, done2, now)
	if err != nil {
		t.Fatalf("handover.done on settled thread: %v", err)
	}
	if sAfter.Starting != nil {
		t.Fatalf("handover.done must not set Starting on a settled thread")
	}
	if vs := CheckTransition(settled, sv, sAfter, sAfterV, done2, sRes, now, nil); len(vs) > 0 {
		t.Fatalf("transition violations: %+v", vs)
	}
	if vs := CheckThread(sAfter, sAfterV, now, nil); len(vs) > 0 {
		t.Fatalf("state violations: %+v", vs)
	}
}

// V0.handover-revives-settled: neither thread.settle nor a settle Rule may
// settle a thread while a handover is in progress.
func TestSettleWaitsForHandover(t *testing.T) {
	now := at(10)
	th := withPR(Thread{
		ID:           "t1",
		Profile:      "a",
		Conversation: Conversation{Handover: &Handover{To: "b", At: now}},
	}, PR{State: "OPEN", Checks: ChecksPass, Head: "aaaaaaa", CheckedAt: ptr(now)})
	v := &View{User: Owner, Entity: "t1", SeenAt: now}

	if got := Registry["thread.settle"].Available(&th, v, now); got != "Wait for the handover to finish" {
		t.Fatalf("thread.settle during handover: %q, want %q", got, "Wait for the handover to finish")
	}

	// Arm "settle when merged", then merge the PR during the handover: the rule
	// must wait for the handover to end before settling.
	if !SettleWhenMerged(&th, ByOwner, now) {
		t.Fatal("SettleWhenMerged failed")
	}
	merged := Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Thread: "t1",
		Args: pr(PR{State: "MERGED", Head: "aaaaaaa", MergedAt: ptr(now.Add(time.Second)), CheckedAt: ptr(now.Add(time.Second))})}
	after, afterV, res, err := Reduce(&th, v, merged, now.Add(time.Second))
	if err != nil {
		t.Fatalf("pr.observed: %v", err)
	}
	if after.Settle.At != nil {
		t.Fatalf("settle rule settled thread mid-handover: %+v", after.Settle)
	}
	if vs := CheckTransition(&th, v, after, afterV, merged, res, now.Add(time.Second), nil); len(vs) > 0 {
		t.Fatalf("transition violations: %+v", vs)
	}
	if vs := CheckThread(after, afterV, now.Add(time.Second), nil); len(vs) > 0 {
		t.Fatalf("state violations: %+v", vs)
	}

	// Once handover completes without a first message (rung 1 resume), the rule settles it.
	doneIn := Input{Source: Observer(ObsProcess), Kind: KindHandoverDone, Thread: "t1",
		Args: HandoverDoneArgs{Profile: "b", Model: "m", SessionID: "s2"}}
	after2, afterV2, _, err := Reduce(after, afterV, doneIn, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("handover.done: %v", err)
	}
	if after2.Settle.At == nil {
		t.Fatal("expected settle rule to fire once handover finished without a first message")
	}
	if vs := CheckThread(after2, afterV2, now.Add(2*time.Second), nil); len(vs) > 0 {
		t.Fatalf("post-settle violations: %+v", vs)
	}
}
