package front

import (
	"slices"
	"testing"
)

// SPEC V0.132 (host order): the owner's host order is set on one front and
// stored on every host; every front that follows those hosts shows it, and
// no command goes to its first host by default (V0.old-routes-end).
func TestHostOrderIsTheOwnersAcrossFronts(t *testing.T) {
	a, b := newSettingsHost(t, "a"), newSettingsHost(t, "b")
	hosts := []HostConfig{{Name: "a", URL: a.hs.URL}, {Name: "b", URL: b.hs.URL}}
	fa, fb := newFront(t, hosts...), newFront(t, hosts...)
	online(t, fa.URL, 2)
	online(t, fb.URL, 2)
	if s := getState(t, fb.URL); s.Hosts[0]["name"] != "a" {
		t.Fatalf("before any order, the config's: %v", s.Hosts[0]["name"])
	}
	if code, rep := post(t, fa.URL+"/api/front/command", `{"commandId":"o1","action":"front.hostOrder","args":{"ids":["id-b","id-a"]}}`); code != 200 || rep["ok"] != true {
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
	// The first host is not a default: a command that names no host reaches none.
	if code, rep := post(t, fb.URL+"/api/command", `{"commandId":"p1","action":"provider.create","args":{"kind":"endpoint","name":"Z","baseURL":"https://z.example"}}`); code != 404 {
		t.Fatalf("a command without a host: %d %v", code, rep)
	}
	if len(b.reg.Providers()) != 0 || len(a.reg.Providers()) != 0 {
		t.Fatalf("a command without a host reached one: a %d b %d", len(a.reg.Providers()), len(b.reg.Providers()))
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
	if code, rep := post(t, fa.URL+"/api/front/command", `{"commandId":"o1","action":"front.hostOrder","args":{"ids":["id-b","id-a"]}}`); code != 200 {
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

// V0.host-ids (decision 0031 F17): the order is kept by install id, so a host
// that is renamed keeps its place; an order set by name before the ids still
// ranks the hosts.
func TestHostOrderIsKeptByIDAcrossARename(t *testing.T) {
	a, b := newSettingsHostID(t, "a", "AAAA-0000-0000-0000"), newSettingsHostID(t, "b", "BBBB-0000-0000-0000")
	hosts := []HostConfig{{Name: "a", URL: a.hs.URL}, {Name: "b", URL: b.hs.URL}}
	fa := newFront(t, hosts...)
	online(t, fa.URL, 2)
	eventually(t, "both ids", func() bool {
		s := getState(t, fa.URL)
		return s.Hosts[0]["id"] != nil && s.Hosts[1]["id"] != nil && s.Hosts[0]["id"] != "" && s.Hosts[1]["id"] != ""
	})
	if code, rep := post(t, fa.URL+"/api/front/command", `{"commandId":"o1","action":"front.hostOrder","args":{"ids":["BBBB-0000-0000-0000","AAAA-0000-0000-0000"]}}`); code != 200 || rep["ok"] != true {
		t.Fatalf("front.hostOrder: %d %v", code, rep)
	}
	for _, h := range []*settingsHost{a, b} {
		o := h.eng.HostOrder()
		if !slices.Equal(o.IDs, []string{"BBBB-0000-0000-0000", "AAAA-0000-0000-0000"}) || !slices.Equal(o.Names, []string{"b", "a"}) {
			t.Fatalf("a host keeps %+v", o)
		}
	}
	// The owner renames b in a front's config: its id keeps it first there; by
	// name alone it would fall to the end.
	fr := newFront(t, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b2", URL: b.hs.URL})
	eventually(t, "b2 first", func() bool {
		s := getState(t, fr.URL)
		return len(s.Hosts) == 2 && s.Hosts[0]["name"] == "b2" && s.Hosts[1]["name"] == "a"
	})
}

// V0.host-ids: an order a front before the ids set (names only) still ranks.
func TestHostOrderByNameStillRanks(t *testing.T) {
	o := hostOrderOf(map[string]any{"names": []any{"b", "a"}, "at": "2026-11-19T10:00:00Z"})
	if !slices.Equal(o.Names, []string{"b", "a"}) || o.IDs != nil || o.At.IsZero() {
		t.Fatalf("names only: %+v", o)
	}
	o = hostOrderOf(map[string]any{"names": []any{"b", "a"}, "ids": []any{"B", "A"}, "at": "2026-11-19T10:00:00Z"})
	if !slices.Equal(o.IDs, []string{"B", "A"}) {
		t.Fatalf("with ids: %+v", o)
	}
}
