package authtest_test

// Decision 0030 slice 5 (V0.160): removal, rotation, and the old link ends.

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/pem"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/authtest"
	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/peerlink"
)

func idOf(t *testing.T, n *authtest.MeshNode) string {
	t.Helper()
	k, err := identity.Load(n.Fyrd.Data)
	if err != nil {
		t.Fatal(err)
	}
	return k.ID()
}

func allPaired(t *testing.T, ns ...*authtest.MeshNode) {
	t.Helper()
	for _, n := range ns {
		var want []string
		for _, o := range ns {
			if o != n {
				want = append(want, o.Name)
			}
		}
		waitOthers(t, n, want...)
	}
}

// The compromised-install rows of 4.3: after Remove on a member, the removed
// install's link is refused everywhere within one sync, its open stream closes
// within a second, and the other members learn it.
func TestRemovingACompromisedInstall(t *testing.T) {
	m := mesh(t, authtest.MeshSpec{Name: "a", UserID: 1}, authtest.MeshSpec{Name: "b", UserID: 1}, authtest.MeshSpec{Name: "c", UserID: 1})
	a, b, c := m.Node("a"), m.Node("b"), m.Node("c")
	allPaired(t, a, b, c)
	actor := "v=1; kind=install; id=front; how=link"
	if st, _ := peerDo(t, c, a, "GET", "/api/profiles", actor, nil); st != 200 {
		t.Fatalf("c at a before the removal: %d", st)
	}
	// c has a stream open at a
	cKey, _ := identity.Load(c.Fyrd.Data)
	aKey, _ := identity.Load(a.Fyrd.Data)
	d := &peerlink.Dialer{Key: cKey}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://peer/api/stream", nil)
	req.Header.Set("Fyr-Actor", strings.ReplaceAll(actor, "id=front;", "id="+cKey.ID()+";"))
	resp, err := d.Client(aKey.ID(), net.JoinHostPort("127.0.0.1", strconv.Itoa(a.PeerPort))).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rd := bufio.NewReader(resp.Body)
	if line, _ := rd.ReadString('\n'); !strings.HasPrefix(line, "event: hello") {
		t.Fatalf("stream: %q", line)
	}
	closed := make(chan struct{})
	go func() {
		for {
			if _, err := rd.ReadString('\n'); err != nil {
				close(closed)
				return
			}
		}
	}()

	if out, err := a.Fyrd.Fyr("host", "remove", idOf(t, c)); err != nil || !strings.Contains(out, "removed") {
		t.Fatalf("fyr host remove: %v %s", err, out)
	}
	select {
	case <-closed:
	case <-time.After(1500 * time.Millisecond):
		t.Error("the removed install's open stream stayed open")
	}
	if st, _ := peerDo(t, c, a, "GET", "/api/profiles", actor, nil); st != 403 {
		t.Errorf("c at a after the removal: %d (want 403)", st)
	}
	// b learns it in one sync, and c is refused there too
	if _, err := b.Fyrd.WaitRoster(30*time.Second, func(r *authtest.Roster) bool { return strings.Join(r.Others(), ",") == "a" }); err != nil {
		t.Fatal(err)
	}
	if st, _ := peerDo(t, c, b, "GET", "/api/profiles", actor, nil); st != 403 {
		t.Errorf("c at b after the sync: %d (want 403)", st)
	}
	// the others still follow each other
	if st, _ := peerDo(t, a, b, "GET", "/api/profiles", actor, nil); st != 200 {
		t.Errorf("a at b: %d", st)
	}
}

// 4.3 (rotation): b rotates its key while c is offline; a follows it by sync, c
// by the statement b's certificate carries when it comes back; the old key is
// refused everywhere.
func TestARotatedKeyIsFollowedByAPeerThatWasOffline(t *testing.T) {
	m := mesh(t, authtest.MeshSpec{Name: "a", UserID: 1}, authtest.MeshSpec{Name: "b", UserID: 1}, authtest.MeshSpec{Name: "c", UserID: 1})
	a, b, c := m.Node("a"), m.Node("b"), m.Node("c")
	allPaired(t, a, b, c)
	oldID := idOf(t, b)
	c.Fyrd.StopKeep() // c is offline

	out, err := b.Fyrd.Fyr("rotate-key")
	if err != nil || !strings.Contains(out, "moves from "+oldID) {
		t.Fatalf("fyr rotate-key: %v\n%s", err, out)
	}
	if !b.Fyrd.WaitExit(20 * time.Second) { // fyrd exits 75; the launcher restarts it
		t.Fatalf("b did not restart\n%s", b.Fyrd.Log())
	}
	if err := b.Fyrd.Restart(); err != nil {
		t.Fatal(err)
	}
	newID := idOf(t, b)
	if newID == oldID {
		t.Fatal("the key did not change")
	}
	// a, online, follows by sync
	if _, err := a.Fyrd.WaitRoster(30*time.Second, func(r *authtest.Roster) bool {
		for _, mm := range r.Members {
			if mm.Name == "b" && mm.ID == newID {
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatal(err)
	}
	// c comes back knowing only the old id
	if err := c.Fyrd.Restart(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Fyrd.WaitRoster(30*time.Second, func(r *authtest.Roster) bool {
		for _, mm := range r.Members {
			if mm.Name == "b" && mm.ID == newID {
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("c does not follow b's new key: %v", err)
	}
	// the old key is a stranger now
	oldKey := loadOldKey(t, b.Fyrd.Data)
	aKey, _ := identity.Load(a.Fyrd.Data)
	d := &peerlink.Dialer{Key: oldKey}
	resp, err := d.Client(aKey.ID(), net.JoinHostPort("127.0.0.1", strconv.Itoa(a.PeerPort))).Get("https://peer/api/profiles")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("the old key at a: %d (want 403)", resp.StatusCode)
	}
	if st, _ := peerDo(t, b, a, "GET", "/api/profiles", "v=1; kind=install; id=front; how=link", nil); st != 200 {
		t.Errorf("the new key at a: %d", st)
	}
}

func loadOldKey(t *testing.T, dir string) *identity.Key {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, identity.File+".old"))
	if err != nil {
		t.Fatal(err)
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		t.Fatal("the old key file is not PEM")
	}
	priv := ed25519.NewKeyFromSeed(blk.Bytes)
	return &identity.Key{Private: priv, Public: priv.Public().(ed25519.PublicKey)}
}

// 5.1 and 3.6: the peer way in's enforce removes the old host listeners (the
// loopback host port here); the peer link keeps working; `fyr auth observe`
// brings them back.
func TestPeerEnforceRemovesTheOldListenersAndObserveRestoresThem(t *testing.T) {
	m := mesh(t, authtest.MeshSpec{Name: "a", UserID: 1, HostListen: true}, authtest.MeshSpec{Name: "b", UserID: 1})
	a, b := m.Node("a"), m.Node("b")
	allPaired(t, a, b)
	old := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(a.HostPort))
	up := func() bool {
		r, err := authtest.Do(old, authtest.Request{Path: "/api/state"})
		return err == nil && r.Status == 200
	}
	waitUp := func(want bool) {
		t.Helper()
		end := time.Now().Add(10 * time.Second)
		for up() != want {
			if time.Now().After(end) {
				t.Fatalf("the old host port is up=%v, want %v\n%s", up(), want, a.Fyrd.Log())
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	waitUp(true) // observe: the old way works
	if out, err := a.Fyrd.Fyr("auth", "enforce", "peer"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	waitUp(false)
	if st, _ := peerDo(t, b, a, "GET", "/api/profiles", "v=1; kind=install; id=front; how=link", nil); st != 200 {
		t.Errorf("the peer link after enforce: %d", st)
	}
	// in enforce a device nobody signed in, and the legacy principal, are refused over the link
	if st, _ := peerDo(t, b, a, "GET", "/api/profiles", "v=1; kind=device; id=dev_nobody; how=local", nil); st != 401 {
		t.Errorf("an unknown device over the link in enforce: %d (want 401)", st)
	}
	if st, _ := peerDo(t, b, a, "GET", "/api/profiles", "v=1; kind=device; id=legacy; how=legacy", nil); st != 401 {
		t.Errorf("the legacy principal over the link in enforce: %d (want 401)", st)
	}
	if out, err := a.Fyrd.Fyr("auth", "observe", "peer"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	waitUp(true)
	if st, _ := peerDo(t, b, a, "GET", "/api/profiles", "v=1; kind=device; id=legacy; how=legacy", nil); st != 200 {
		t.Errorf("the legacy principal after the way back: %d", st)
	}
	// the stored mode survives a restart: enforce again, restart, the port stays down
	a.Fyrd.Fyr("auth", "enforce", "peer")
	if err := a.Fyrd.Restart(); err != nil {
		t.Fatal(err)
	}
	if up() {
		t.Error("the old host port came back after a restart in enforce")
	}
}

// 3.9: `fyrd selftest` loads the install key, the roster and the device table, so
// a build that cannot read them is not switched to.
func TestSelftestReadsTheKeyTheRosterAndTheDevices(t *testing.T) {
	t.Parallel()
	bin, err := authtest.Binary("fyrd")
	if err != nil {
		t.Fatal(err)
	}
	run := func(setup func(dir string)) (string, error) {
		dir, rm, err := authtest.NewDataDir()
		if err != nil {
			t.Fatal(err)
		}
		defer rm()
		setup(dir)
		return selftest(t, bin, dir)
	}
	if out, err := run(func(string) {}); err != nil || !strings.Contains(out, "passed") {
		t.Fatalf("a fresh install: %v\n%s", err, out)
	}
	for name, c := range map[string]struct {
		file, want string
	}{
		"a broken roster":       {"roster.json", "reading the roster"},
		"a broken install key":  {identity.File, "reading the install key"},
		"a broken device table": {"", "reading the devices"},
	} {
		out, err := run(func(dir string) {
			// the install exists first, as a snapshot of a live one does
			if o, e := selftest(t, bin, dir); e != nil {
				t.Fatalf("%v\n%s", e, o)
			}
			if c.file != "" {
				os.WriteFile(filepath.Join(dir, c.file), []byte("this is not it"), 0o600)
			} else {
				authtest.BreakDevices(t, dir)
			}
		})
		if err == nil || !strings.Contains(out, c.want) {
			t.Errorf("selftest on %s: %v\n%s (want a failure that says %q)", name, err, out, c.want)
		}
	}
}

// Step-up (3.4): a session that was just made cannot widen trust; the owner
// socket can.
func TestAFreshSessionCannotWidenTrust(t *testing.T) {
	t.Parallel()
	f, err := authtest.Start(authtest.Options{Name: "stepup", Extra: map[string]any{"stepUpMinutes": 5}})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Stop()
	link, err := f.OwnerOnce()
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := authtest.Redeem(f.URL(), link)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/front/auth/add", "/api/front/auth/approve"} {
		r, err := authtest.Do(f.URL(), authtest.Request{Method: "POST", Path: path, Cookie: cookie, Header: map[string]string{"Origin": f.URL()}, Body: strings.NewReader(`{"code":"ABCD2345"}`)})
		if err != nil || r.Status != 403 || !strings.Contains(string(r.Body), "too new") {
			t.Errorf("%s with a fresh session: %v %d %s", path, err, r.Status, r.Body)
		}
	}
	if _, err := f.OwnerCall("POST", "/device/add", map[string]string{"name": "x"}); err != nil {
		t.Errorf("the owner socket was asked for a step-up: %v", err)
	}
	// reading and ending trust need none
	if r, _ := authtest.Do(f.URL(), authtest.Request{Path: "/api/front/auth/state", Cookie: cookie}); r.Status != 200 {
		t.Errorf("state: %d", r.Status)
	}
	_ = auth.Cookie
}

// selftest runs `fyrd selftest` on a data folder with a client page and no tailscaled.
func selftest(t *testing.T, bin, dir string) (string, error) {
	t.Helper()
	web := filepath.Join(dir, "web")
	os.MkdirAll(web, 0o700)
	os.WriteFile(filepath.Join(web, "index.html"), []byte("<title>Fyr</title>"), 0o600)
	cfg := filepath.Join(dir, "config.json")
	os.WriteFile(cfg, []byte(`{"tailscaleSocket":"`+filepath.Join(dir, "no.sock")+`"}`), 0o600)
	out, err := authtest.Command(bin, "selftest", "-data", dir, "-config", cfg, "-web", web).CombinedOutput()
	return string(out), err
}
