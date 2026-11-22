package push

import "testing"

// V0.persist-runtime-state: pushes waiting for a native device that is not
// listening survive a restart and are sent once when it listens.
func TestNativeQueueSurvivesRestart(t *testing.T) {
	p, _ := newPusher(t, nil)
	ep, err := p.NewNative("owner", "", "Fyr.app")
	if err != nil {
		t.Fatal(err)
	}
	p.Notify("owner", Payload{Title: "t1", ThreadID: "t1", State: "asking"})

	p2 := &Pusher{DB: p.DB} // the restarted daemon
	if err := p2.Init(); err != nil {
		t.Fatal(err)
	}
	ch, stop := p2.Listen(ep)
	defer stop()
	if got := recv(t, ch); got.ThreadID != "t1" || got.State != "asking" {
		t.Fatalf("after restart: %+v", got)
	}
	none(t, ch)

	p3 := &Pusher{DB: p.DB}
	p3.Init()
	ch3, stop3 := p3.Listen(ep)
	defer stop3()
	none(t, ch3) // sent once: not again after another restart
}
