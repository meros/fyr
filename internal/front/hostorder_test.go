package front

import (
	"slices"
	"testing"
)

// SPEC V0.132 (host order): the owner's host order is set on one front and
// stored on every host; every front that follows those hosts shows it, and
// its first host is the default for a command that names none.
func TestHostOrderIsTheOwnersAcrossFronts(t *testing.T) {
	a, b := newSettingsHost(t, "a"), newSettingsHost(t, "b")
	hosts := []HostConfig{{Name: "a", URL: a.hs.URL}, {Name: "b", URL: b.hs.URL}}
	fa, fb := newFront(t, hosts...), newFront(t, hosts...)
	online(t, fa.URL, 2)
	online(t, fb.URL, 2)
	if s := getState(t, fb.URL); s.Hosts[0]["name"] != "a" {
		t.Fatalf("before any order, the config's: %v", s.Hosts[0]["name"])
	}
	if code, rep := post(t, fa.URL+"/api/command", `{"commandId":"o1","action":"front.hostOrder","args":{"names":["b","a"]}}`); code != 200 || rep["ok"] != true {
		t.Fatalf("front.hostOrder: %d %v", code, rep)
	}
	for _, h := range []*settingsHost{a, b} {
		if got := h.eng.HostOrder().Names; !slices.Equal(got, []string{"b", "a"}) {
			t.Fatalf("a host keeps %v", got)
		}
	}
	for _, f := range []string{fa.URL, fb.URL} {
		eventually(t, "b first on "+f, func() bool { s := getState(t, f); return s.Hosts[0]["name"] == "b" && s.Hosts[1]["name"] == "a" })
	}
	// The first host is the default: a command that names none goes to b, also from the other front.
	if code, rep := post(t, fb.URL+"/api/command", `{"commandId":"p1","action":"provider.create","args":{"kind":"endpoint","name":"Z","baseURL":"https://z.example"}}`); code != 200 {
		t.Fatalf("create: %d %v", code, rep)
	}
	if len(b.reg.Providers()) != 1 || len(a.reg.Providers()) != 0 {
		t.Fatalf("the default host is not b: a %d b %d", len(a.reg.Providers()), len(b.reg.Providers()))
	}
}

// SPEC V0.132: a host that was offline when the order changed gets it when
// it is back; a front started later reads it from its hosts.
func TestHostOrderReachesAHostThatWasOffline(t *testing.T) {
	a, b := newSettingsHost(t, "a"), newSettingsHost(t, "b")
	hosts := []HostConfig{{Name: "a", URL: a.hs.URL}, {Name: "b", URL: b.hs.URL}}
	fa := newFront(t, hosts...)
	online(t, fa.URL, 2)
	b.setDown(true)
	eventually(t, "b offline", func() bool { return getState(t, fa.URL).Hosts[1]["online"] == false })
	if code, rep := post(t, fa.URL+"/api/command", `{"commandId":"o1","action":"front.hostOrder","args":{"names":["b","a"]}}`); code != 200 {
		t.Fatalf("front.hostOrder with b offline: %d %v", code, rep)
	}
	if len(b.eng.HostOrder().Names) != 0 {
		t.Fatal("offline b got the order")
	}
	b.setDown(false)
	eventually(t, "b catches up", func() bool { return slices.Equal(b.eng.HostOrder().Names, []string{"b", "a"}) })
	fc := newFront(t, hosts...)
	eventually(t, "a new front reads the order", func() bool { s := getState(t, fc.URL); return len(s.Hosts) == 2 && s.Hosts[0]["name"] == "b" })
}
