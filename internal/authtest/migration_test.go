package authtest_test

// Decision 0030 Part 4.4, Part 5: the owner is never locked out. The test
// starts from authtest.Live, the install as it runs today, and applies each
// slice's state change in order. After every step the same checks run. Each
// slice extends `steps` (and `checks` when a slice adds a way in); a slice
// that breaks it is not done.

import (
	"os"
	"path/filepath"
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

var steps = []step{
	{"slice 0: today (V0.130, observe)", func(t *testing.T, l *authtest.Live) {}},
	// Slice 1 adds: the new front and the old host, then the old front and the
	// new host, a mixed-version pair.
	// Slice 2 adds: the stored modes, `fyr auth enforce` on browser, then
	// `fyr auth observe` back.
	// Slices 3 to 6 add: the roster, the peer link beside the whois link, the
	// invite, removal, the LAN.
}

func TestMigrationKeepsTheOwnerSignedIn(t *testing.T) {
	l := live(t)
	for _, s := range steps {
		s.apply(t, l)
		t.Run(s.name, func(t *testing.T) { checks(t, l) })
		if t.Failed() {
			return // the later steps start from a state that is already wrong
		}
	}
}

func checks(t *testing.T, l *authtest.Live) {
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
