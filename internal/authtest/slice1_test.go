package authtest_test

// Decision 0030 slice 1 (V0.156): the principal on every hop, as the live
// process shows it. The walk over every route of the front's mux is in
// internal/front (it needs the mux).

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/authtest"
)

// A browser-facing listener takes no Fyr-Actor: a page or a process could
// write it. 400 on the loopback port and through Serve's socket.
func TestFyrActorOnABrowserListenerIs400(t *testing.T) {
	l := live(t)
	hdr := map[string]string{"Fyr-Actor": "v=1; kind=install; id=AAAA-BBBB-CCCC-DDDD; how=link"}
	r, err := authtest.Do(l.Laptop.URL(), authtest.Request{Path: "/api/front/state", Cookie: l.Local, Header: hdr})
	if err != nil || r.Status != 400 {
		t.Errorf("loopback: %v %d (want 400)", err, r.Status)
	}
	r, err = authtest.Do(l.Phones.URL, authtest.Request{Path: "/api/front/state", Cookie: l.Tailnet, Header: hdr})
	if err != nil || r.Status != 400 {
		t.Errorf("through Serve: %v %d (want 400)", err, r.Status)
	}
}

// serve.sock's peer is tailscaled, which is root. fyrd's own user is not: a
// local process of that user could write the login header. Without the
// harness's allowance (config serveSocketUsers) the emulator is closed out and
// mints nothing.
func TestServeSocketClosedToAnotherUser(t *testing.T) {
	l := live(t)
	f, err := authtest.Start(authtest.Options{Name: "strict", Tailscale: l.TS})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Stop()
	s, err := l.TS.ServeFor(l.Phone, f.Sock(auth.ServeSocket))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := authtest.Do(s.URL, authtest.Request{Path: "/api/front/state"})
	if err != nil || r.Status != http.StatusBadGateway || r.Session() != "" {
		t.Fatalf("a peer that is not root: %v %d session %q (want 502 and none)", err, r.Status, r.Session())
	}
	if !f.WaitLog("serve socket refused a connection", 5*time.Second) {
		t.Errorf("the refusal is not logged:\n%s", f.Log())
	}
}

// 3.3: an owner command names who gave it. The event of a command sent
// through the front carries the device and the front's install.
func TestCommandEventNamesTheDevice(t *testing.T) {
	l := live(t)
	body := `{"commandId":"audit-1","action":"thread.create","args":{"cwd":"` + t.TempDir() + `","text":"audit"}}`
	r, err := authtest.Do(l.Laptop.URL(), authtest.Request{Method: "POST", Path: "/api/h/" + l.ID + "/command", Cookie: l.Local,
		Header: map[string]string{"Content-Type": "application/json"}, Body: strings.NewReader(body)})
	if err != nil || r.Status != 200 {
		t.Fatalf("create: %v %d %s", err, r.Status, r.Body)
	}
	var rep struct{ ThreadID string }
	json.Unmarshal(r.Body, &rep)
	if rep.ThreadID == "" {
		t.Fatalf("no thread in %s", r.Body)
	}
	ev, err := l.Laptop.Get("/api/h/"+l.ID+"/threads/"+rep.ThreadID+"/events?limit=50", l.Local)
	if err != nil || ev.Status != 200 {
		t.Fatalf("events: %v %d", err, ev.Status)
	}
	var out struct {
		Events []struct {
			Type string
			Data map[string]any
		}
	}
	json.Unmarshal(ev.Body, &out)
	for _, e := range out.Events {
		if e.Type == "command" && e.Data["commandId"] == "audit-1" {
			if e.Data["actor"] != "device:"+l.LocalID || e.Data["front"] != l.ID {
				t.Fatalf("the command event names %v / %v (want device:%s / %s)", e.Data["actor"], e.Data["front"], l.LocalID, l.ID)
			}
			return
		}
	}
	t.Fatalf("no command event for the audit command in %s", ev.Body)
}

// Migration, slice 1 (see TestMigrationKeepsTheOwnerSignedIn): a new front
// keeps following a host of an older build, which ignores Fyr-Actor.
func TestNewFrontFollowsAnOlderHost(t *testing.T) {
	l := live(t)
	l.Rec.SetStripActor(true)
	r, err := l.Laptop.Get(l.Laptop.HostPath(l.Local, "server", "/profiles"), l.Local)
	if err != nil || r.Status != 200 {
		t.Fatalf("a call through the front to an older host: %v %d %s", err, r.Status, r.Body)
	}
	if hosts, err := l.WaitHostsOnline(l.Local, 10*time.Second, "server"); err != nil || !hosts["server"] {
		t.Errorf("the older host is not followed: %v %v", hosts, err)
	}
}

// Observe mode: a browser with no session is still served, and named on the
// hop as the legacy principal (counted, refused by a later slice's enforce).
func TestNoSessionIsTheLegacyPrincipalOnTheHop(t *testing.T) {
	l := live(t)
	if _, err := l.WaitHostsOnline(l.Local, 15*time.Second, "server"); err != nil {
		t.Fatal(err)
	}
	mark := l.Rec.Mark()
	if _, err := l.Laptop.Get(l.Laptop.HostPath("", "server", "/commands"), ""); err != nil {
		t.Fatal(err)
	}
	for _, s := range l.Rec.Since(mark) {
		if strings.Contains(s.Path, "/api/commands") {
			if got := s.Header.Get("Fyr-Actor"); !strings.Contains(got, "kind=device; id=legacy; how=legacy") {
				t.Fatalf("the host was told %q", got)
			}
			return
		}
	}
	t.Fatalf("the call never reached the host: %v", l.Rec.Since(mark))
}
