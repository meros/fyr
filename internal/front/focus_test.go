// SPEC 0.5 V0.85: the front forwards each presence beat's thread to the
// host that runs it, and tells the previous host when the device moves on.
package front

import (
	"slices"
	"testing"
)

func TestV085PresenceForwardsFocusToTheThreadsHost(t *testing.T) {
	a, b := newHost(t, "a"), newHost(t, "b")
	fs := newFront(t, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	online(t, fs.URL, 2)
	idA := create(t, fs.URL, "a", "on-a")
	idB := create(t, fs.URL, "b", "on-b")

	beat := func(host, thread string) {
		t.Helper()
		if code, _ := post(t, fs.URL+"/api/front/presence", `{"host":"`+host+`","threadId":"`+thread+`","device":"phone"}`); code != 204 {
			t.Fatalf("presence %q: %d", thread, code)
		}
	}
	beat(testID("b"), idB)
	eventually(t, "b focused", func() bool { return slices.Contains(b.focused(), "phone "+idB+" on") })
	if len(a.focused()) != 0 {
		t.Fatalf("a heard of b's thread: %v", a.focused())
	}
	beat(testID("b"), idB) // each beat renews the focus
	eventually(t, "b renewed", func() bool { return len(b.focused()) == 2 })

	beat(testID("a"), idA) // the device moves to a's thread
	eventually(t, "b off, a on", func() bool {
		return slices.Contains(b.focused(), "phone "+idB+" off") && slices.Contains(a.focused(), "phone "+idA+" on")
	})
	beat("", "") // nothing shown: a goes off, nothing new is focused
	eventually(t, "a off", func() bool { return slices.Contains(a.focused(), "phone "+idA+" off") })
	if n := len(b.focused()); n != 3 {
		t.Fatalf("b: %v", b.focused())
	}
}
