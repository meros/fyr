package authtest_test

// Decision 0030 Part 4.4, Part 5: the owner is never locked out. The test
// starts from authtest.Live, the install as it runs today, and applies each
// slice's state change in order. After every step the same checks run. Each
// slice extends `steps` (and `checks` when a slice adds a way in); a slice
// that breaks it is not done.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/authtest"
)

// step is one slice's change to the live install. Slice 0 changes nothing:
// it pins today.
type step struct {
	name  string
	apply func(t *testing.T, l *authtest.Live)
}

// migMac is the Mac slice 3 starts, which slice 4 uses.
var migMac *authtest.Mac

var steps = []step{
	{"slice 0: today (V0.130, observe)", func(t *testing.T, l *authtest.Live) {}},
	// Slice 1: the principal on every hop. A new front with an older host (the
	// recorder drops Fyr-Actor, as an older build ignores it) is checked in
	// TestNewFrontFollowsAnOlderHost; a front without Fyr-Actor against a new
	// host is TestPeerHandlerServesALegacyFront (internal/server).
	{"slice 1: Fyr-Actor on every hop", func(t *testing.T, l *authtest.Live) {
		l.Rec.SetStripActor(false)
	}},
	// Slice 2: sessions decide. The phone signs in once while observing (its
	// node is approved, 5.2), then the owner enforces the browser way: the
	// fixture's two cookies, the phone with no cookie and fyr open all still
	// get in (checks run after the step), and a browser with no session does not.
	{"slice 2: fyr auth enforce browser", func(t *testing.T, l *authtest.Live) {
		if r, err := authtest.Do(l.Phones.URL, authtest.Request{Path: "/api/state"}); err != nil || r.Status != 200 {
			t.Fatalf("the phone in observe: %v %d", err, r.Status)
		}
		if out, err := l.Laptop.Fyr("auth", "enforce"); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if r, err := l.Laptop.Get("/api/state", ""); err != nil || r.Status != 401 {
			t.Fatalf("enforce, a browser with no session: %v %d (want 401)", err, r.Status)
		}
	}},
	{"slice 2: fyr auth observe restores the old ways", func(t *testing.T, l *authtest.Live) {
		if out, err := l.Laptop.Fyr("auth", "observe"); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if r, err := l.Laptop.Get("/api/state", ""); err != nil || r.Status != 200 {
			t.Fatalf("observe again, a browser with no session: %v %d (want 200)", err, r.Status)
		}
	}},
	// Slice 3: the laptop restarts with a peer port, as a release restarts it,
	// and the owner's Mac (the same tailnet user, Serve up on both) pairs with it
	// by condition 1 with no press. The laptop's front follows the Mac over the
	// peer link and its static host as before; the cookies, the phone and fyr open
	// all still work (checks run after the step).
	{"slice 3: a peer port; the Mac pairs by condition 1", func(t *testing.T, l *authtest.Live) {
		mac, err := l.EnablePeers()
		if err != nil {
			t.Fatal(err)
		}
		migMac = mac
		if _, err := l.Laptop.WaitRoster(30*time.Second, func(r *authtest.Roster) bool { return strings.Contains(strings.Join(r.Others(), ","), "mac") }); err != nil {
			t.Fatal(err)
		}
		if _, err := mac.Fyrd.WaitRoster(30*time.Second, func(r *authtest.Roster) bool { return strings.Contains(strings.Join(r.Others(), ","), "laptop") }); err != nil {
			t.Fatal(err)
		}
		if hosts, err := l.WaitHostsOnline(l.Local, 30*time.Second, "laptop", "server", "mac"); err != nil || !hosts["mac"] || !hosts["server"] {
			t.Fatalf("the front follows %v (%v): want laptop, server and mac\n%s", hosts, err, l.Laptop.Log())
		}
	}},
	// Slice 4: the owner's invite. A tagged install (meros-server) cannot pair by
	// itself; `fyr pair` on the laptop opens an invite, the box joins with it, the
	// Mac learns the box by vouching. Pairing only adds: the cookies, the phone, fyr
	// open and the static host all still work (checks run after the step).
	{"slice 4: a tagged install joins by invite", func(t *testing.T, l *authtest.Live) {
		box, err := l.AddTaggedInstall(migMac)
		if err != nil {
			t.Fatal(err)
		}
		out, err := l.Laptop.Fyr("pair")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		var code string
		for _, f := range strings.Fields(out) {
			if strings.HasPrefix(f, "fyr1-") {
				code = f
			}
		}
		r, err := l.Laptop.Roster()
		if err != nil {
			t.Fatal(err)
		}
		var port int
		for _, m := range r.Members {
			if m.Self {
				port = m.Port
			}
		}
		if out, err := box.Fyrd.Fyr("pair", code, "-addr", "127.0.0.1:"+strconv.Itoa(port)); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		for name, f := range map[string]*authtest.Fyrd{"laptop": l.Laptop, "mac": migMac.Fyrd} {
			if _, err := f.WaitRoster(30*time.Second, func(r *authtest.Roster) bool { return strings.Contains(strings.Join(r.Others(), ","), "box") }); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}
		if hosts, err := l.WaitHostsOnline(l.Local, 30*time.Second, "laptop", "server", "mac", "box"); err != nil || !hosts["box"] {
			t.Fatalf("the front follows %v (%v)\n%s", hosts, err, l.Laptop.Log())
		}
	}},
	// Slice 5: removal, rotation, and the old link ends. The owner removes the box
	// (a lost server): the laptop's front stops following it and the Mac learns it.
	// The Mac rotates its key and restarts: the laptop follows the new key. The peer
	// way in is enforced and put back by hand. The cookies, the phone, fyr open and
	// the static host all keep working (checks run after the step).
	{"slice 5: remove, rotate, peer enforce and the way back", func(t *testing.T, l *authtest.Live) {
		r, err := l.Laptop.Roster()
		if err != nil {
			t.Fatal(err)
		}
		var boxID string
		for _, m := range r.Members {
			if m.Name == "box" {
				boxID = m.ID
			}
		}
		if out, err := l.Laptop.Fyr("host", "remove", boxID); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		end := time.Now().Add(30 * time.Second)
		for hostsOf(l)["box"] {
			if time.Now().After(end) {
				t.Fatalf("the front still follows the removed box: %v", hostsOf(l))
			}
			time.Sleep(50 * time.Millisecond)
		}
		if _, err := migMac.Fyrd.WaitRoster(30*time.Second, func(r *authtest.Roster) bool { return !strings.Contains(strings.Join(r.Others(), ","), "box") }); err != nil {
			t.Fatalf("the Mac did not learn the removal: %v", err)
		}
		// the Mac rotates its key
		out, err := migMac.Fyrd.Fyr("rotate-key")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if !migMac.Fyrd.WaitExit(20 * time.Second) {
			t.Fatal("the Mac did not restart")
		}
		if err := migMac.Fyrd.Restart(); err != nil {
			t.Fatal(err)
		}
		if hosts, err := l.WaitHostsOnline(l.Local, 30*time.Second, "laptop", "server", "mac"); err != nil || !hosts["mac"] {
			t.Fatalf("the front does not follow the Mac on its new key: %v %v\n%s", hosts, err, l.Laptop.Log())
		}
		// the peer way in, by hand, and back
		if out, err := l.Laptop.Fyr("auth", "enforce", "peer"); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if hosts, err := l.WaitHostsOnline(l.Local, 30*time.Second, "laptop", "server", "mac"); err != nil || !hosts["mac"] {
			t.Fatalf("in peer enforce the front does not follow the Mac: %v %v", hosts, err)
		}
		if out, err := l.Laptop.Fyr("auth", "observe", "peer"); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}},
	// Slice 5b: two-way links. The owner's tailnet policy lets the laptop reach the Mac
	// and not the Mac reach the laptop: the Mac's dials to the laptop's peer port go into
	// a black hole, and the Mac restarts. The laptop's front still follows the Mac, and the
	// Mac's front now sees the laptop, over the one link the laptop opened. Everything
	// existing keeps working (checks run after the step).
	{"slice 5b: the Mac cannot dial the laptop; one link carries both ways", func(t *testing.T, l *authtest.Live) {
		r, err := l.Laptop.Roster()
		if err != nil {
			t.Fatal(err)
		}
		var port int
		for _, m := range r.Members {
			if m.Self {
				port = m.Port
			}
		}
		if err := l.BlockDial("mac", l.Self.Name, port); err != nil {
			t.Fatal(err)
		}
		if err := migMac.Fyrd.PinPeerPort(); err != nil { // a real install's peerPort is fixed
			t.Fatal(err)
		}
		if err := migMac.Fyrd.Restart(); err != nil {
			t.Fatal(err)
		}
		link, err := migMac.Fyrd.OwnerOnce()
		if err != nil {
			t.Fatal(err)
		}
		macCookie, err := authtest.Redeem(migMac.Fyrd.URL(), link)
		if err != nil {
			t.Fatal(err)
		}
		end := time.Now().Add(30 * time.Second)
		var ml authtest.LinkOf
		for {
			if x, ok := authtest.HostLink(migMac.Fyrd, macCookie, "laptop"); ok && x.State == "up" {
				ml = x
				break
			}
			if time.Now().After(end) {
				rr, _ := migMac.Fyrd.Get("/api/state", macCookie)
				t.Fatalf("the Mac's front does not see the laptop (it cannot dial it): %+v\n%.600s\n%s", ml, rr.Body, migMac.Fyrd.Log())
			}
			time.Sleep(50 * time.Millisecond)
		}
		if ml.Opened != "them" {
			t.Errorf("the Mac's link to the laptop was opened %q (the laptop dials: want them)", ml.Opened)
		}
		if hosts, err := l.WaitHostsOnline(l.Local, 30*time.Second, "laptop", "server", "mac"); err != nil || !hosts["mac"] {
			t.Fatalf("the laptop's front lost the Mac: %v %v", hosts, err)
		}
	}},
	// Slice 6 adds: the LAN.
}

func TestMigrationKeepsTheOwnerSignedIn(t *testing.T) {
	l := live(t)
	for i, s := range steps {
		s.apply(t, l)
		t.Run(s.name, func(t *testing.T) { checks(t, l, i) })
		if t.Failed() {
			return // the later steps start from a state that is already wrong
		}
	}
}

// checks runs after step n; a slice's checks run from its step on.
func checks(t *testing.T, l *authtest.Live, n int) {
	t.Helper()
	// 1. The fixture's two cookies still get the API on the laptop's loopback
	//    port (the laptop's browser) ...
	for name, c := range map[string]string{"local": l.Local, "tailnet": l.Tailnet} {
		r, err := l.Laptop.Get("/api/state", c)
		if err != nil || r.Status != 200 {
			t.Errorf("the %s cookie on loopback: %v %d", name, err, r.Status)
		}
	}
	// ... and through Serve (the phone): a session that is known is not
	// minted again, which is how the front shows it recognised the cookie.
	for name, c := range map[string]string{"local": l.Local, "tailnet": l.Tailnet} {
		r, err := authtest.Do(l.Phones.URL, authtest.Request{Path: "/api/state", Cookie: c})
		if err != nil || r.Status != 200 || r.Session() != "" {
			t.Errorf("the %s cookie through Serve: %v %d (minted %q; want 200, a known session)", name, err, r.Status, r.Session())
		}
	}
	// 2. The phone's path with no cookie: the owner's node signs in and the
	//    session it gets works.
	r, err := authtest.Do(l.Phones.URL, authtest.Request{Path: "/api/state"})
	if err != nil || r.Status != 200 || r.Session() == "" {
		t.Fatalf("the phone through Serve with no cookie: %v %d session %q", err, r.Status, r.Session())
	}
	if r2, err := authtest.Do(l.Phones.URL, authtest.Request{Path: "/api/state", Cookie: r.Session()}); err != nil || r2.Status != 200 {
		t.Errorf("the new phone session: %v %d", err, r2.Status)
	}
	// 3. The front still follows its host.
	if hosts, err := l.WaitHostsOnline(l.Local, 15*time.Second, "laptop", "server"); err != nil || !hosts["server"] {
		t.Errorf("the front does not follow its host: %v %v", hosts, err)
	}
	// 4. `fyr open` over owner.sock mints a session.
	if session := fyrOpen(t, l); session == "" {
		t.Error("fyr open minted no session")
	} else if r, err := l.Laptop.Get("/api/state", session); err != nil || r.Status != 200 {
		t.Errorf("the session fyr open minted: %v %d", err, r.Status)
	}
	// 4b. From slice 1: what the front sent its host names who acted, and
	//     carries no browser credential.
	if n >= 1 {
		mark := l.Rec.Mark()
		if _, err := l.Laptop.Get("/api/commands?host=server", l.Local); err != nil { // forwarded to the server host as this device
			t.Fatalf("a call to the server host: %v", err)
		}
		var sawDevice bool
		for _, s := range l.Rec.Since(mark) {
			if !s.Has("Fyr-Actor") {
				t.Errorf("%s %s went to the host with no Fyr-Actor", s.Method, s.Path)
			}
			for _, h := range []string{"Cookie", "Authorization", "Tailscale-User-Login", "X-Forwarded-For", "Origin"} {
				if s.Has(h) {
					t.Errorf("%s %s went to the host with %s", s.Method, s.Path, h)
				}
			}
			sawDevice = sawDevice || strings.Contains(s.Header.Get("Fyr-Actor"), "kind=device; id="+l.LocalID)
		}
		if !sawDevice {
			t.Errorf("the host never saw the laptop's device %s", l.LocalID)
		}
	}
	// 5. After enforce, `fyr auth observe` restores the old ways: added by
	//    slice 2, which also makes the first way in enforceable.
}

// fyrOpen runs the real `fyr open` against the laptop's data folder, with a
// "browser" that writes the link it was given to a file, and redeems the
// link as that browser would.
func fyrOpen(t *testing.T, l *authtest.Live) string {
	t.Helper()
	bin, err := authtest.Binary("fyr")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "link")
	browser := filepath.Join(dir, "browser")
	if err := os.WriteFile(browser, []byte("#!/bin/sh\necho \"$1\" > '"+out+".tmp' && mv '"+out+".tmp' '"+out+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := authtest.Command(bin, "open", "-data", l.Laptop.Data)
	cmd.Env = append(cmd.Env, "FYR_BROWSER="+browser) // the one FYR_ variable a test sets on purpose, for fyr and not for fyrd
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fyr open: %v\n%s", err, b)
	}
	var link string
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if b, err := os.ReadFile(out); err == nil {
			link = strings.TrimSpace(string(b))
			break
		}
	}
	if link == "" {
		t.Fatal("fyr open did not hand its browser a link")
	}
	session, err := authtest.Redeem(l.Laptop.URL(), link)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func hostsOf(l *authtest.Live) map[string]bool {
	h, _ := l.HostsOnline(l.Local)
	return h
}
