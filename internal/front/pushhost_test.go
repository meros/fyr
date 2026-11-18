package front

import (
	"strings"
	"testing"
)

// SPEC V0.140: with several hosts a push says which host the thread runs on,
// first in its body; with one host it says nothing more.
func TestFrontPushNamesTheHost(t *testing.T) {
	a, b := fullHost(t, "a", "pa"), fullHost(t, "b", "pb")
	p := &fakePush{}
	fs := startFront(t, Options{Push: p, NotifyDelay: 1}, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	online(t, fs.URL, 2)
	id := create(t, fs.URL, "b", "on-b")
	ask(b, id, "Which colour?")
	eventually(t, "a push", func() bool { return len(p.payloads()) == 1 })
	if got := p.payloads()[0].Body; got != "b · 1 question · Which colour?" {
		t.Fatalf("body %q", got)
	}
	// V0.207: the push names its host, so a device that hides it is left out.
	if o := p.payloads()[0].Origin; o == nil || o.Host != "b" {
		t.Fatalf("origin %+v", o)
	}

	c := fullHost(t, "c", "pc")
	p1 := &fakePush{}
	f1 := startFront(t, Options{Push: p1, NotifyDelay: 1}, HostConfig{Name: "c", URL: c.hs.URL})
	online(t, f1.URL, 1)
	id1 := create(t, f1.URL, "c", "on-c")
	ask(c, id1, "Which colour?")
	eventually(t, "a push", func() bool { return len(p1.payloads()) == 1 })
	if got := p1.payloads()[0].Body; strings.HasPrefix(got, "c · ") {
		t.Fatalf("one host named: %q", got)
	}
}
