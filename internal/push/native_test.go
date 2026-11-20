package push

import (
	"encoding/json"
	"testing"
	"time"
)

func recv(t *testing.T, ch <-chan []byte) Payload {
	t.Helper()
	select {
	case b, ok := <-ch:
		if !ok {
			t.Fatal("the stream closed")
		}
		var pl Payload
		json.Unmarshal(b, &pl)
		return pl
	case <-time.After(time.Second):
		t.Fatal("no push")
	}
	return Payload{}
}

func none(t *testing.T, ch <-chan []byte) {
	t.Helper()
	select {
	case b, ok := <-ch:
		if ok {
			t.Fatalf("unexpected push %s", b)
		}
	default:
	}
}

// V0.mac-notify: Fyr.app is a device like any other: its pushes wait until
// it listens, the newest per thread; they follow its own settings; and its
// endpoint lasts across the app's restarts.
func TestNativeDevice(t *testing.T) {
	p, out := newPusher(t, nil)
	p.Subscribe("owner", sub("https://push/phone"), "phone")
	ep, err := p.NewNative("owner", "", "Fyr.app")
	if err != nil || !IsNative(ep) || !p.Known(ep) {
		t.Fatalf("NewNative: %q %v", ep, err)
	}
	if again, _ := p.NewNative("owner", ep, "Fyr.app"); again != ep {
		t.Fatalf("a known endpoint is kept: got %q", again)
	}
	if other, _ := p.NewNative("owner", "native:gone", "Fyr.app"); other == "native:gone" || !p.Known(other) {
		t.Fatalf("an unknown endpoint gets a new one: %q", other)
	}

	// Not listening: kept, and a thread's newer push replaces its older one.
	for _, s := range []string{"working", "asking"} {
		if n := p.Notify("owner", Payload{Title: "t1", ThreadID: "t1", State: s}); n != 3 {
			t.Fatalf("delivered %d, want 3 (phone and both native devices)", n)
		}
	}
	p.Notify("owner", Payload{Title: "t2", ThreadID: "t2", State: "failed"})
	for _, s := range *out {
		if IsNative(s.endpoint) {
			t.Fatalf("a native device went to the push service: %s", s.endpoint)
		}
	}
	ch, stop := p.Listen(ep)
	if a, b := recv(t, ch), recv(t, ch); a.State != "asking" || b.ThreadID != "t2" {
		t.Fatalf("waiting pushes: %+v %+v", a, b)
	}
	none(t, ch)

	// Listening: at once, and its own settings apply.
	p.Notify("owner", Payload{Title: "t3", ThreadID: "t3", State: "asking"})
	if got := recv(t, ch); got.ThreadID != "t3" {
		t.Fatalf("live push: %+v", got)
	}
	p.SetPrefs(ep, Prefs{Finished: false})
	p.SetHidden(ep, Hidden{Hosts: []string{"lab"}})
	p.Notify("owner", Payload{Title: "t4", ThreadID: "t4", State: "done"})
	p.Notify("owner", Payload{Title: "t5", ThreadID: "t5", State: "asking", Origin: &Origin{Host: "lab"}})
	none(t, ch)

	// A second listener takes over; removing the device ends the stream.
	ch2, stop2 := p.Listen(ep)
	if _, ok := <-ch; ok {
		t.Fatal("the first listener's stream stays open")
	}
	stop()
	p.Unsubscribe(ep)
	if _, ok := <-ch2; ok {
		t.Fatal("the stream stays open after unsubscribe")
	}
	stop2()
	if p.Known(ep) {
		t.Fatal("still stored after unsubscribe")
	}
}

// A browser cannot store a native endpoint through /push/subscribe: it
// needs keys, which a native device has none of.
func TestSubscribeNeedsKeys(t *testing.T) {
	p, _ := newPusher(t, nil)
	if err := p.Subscribe("owner", Subscription{Endpoint: "native:abc"}, "x"); err == nil {
		t.Fatal("a keyless subscription was stored")
	}
}
