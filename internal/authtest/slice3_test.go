package authtest_test

// Decision 0030 slice 3 (V0.158): the roster, the pinned link, the tailnet pairs.
// Real fyrds on a fake tailnet: each has its own tailscaled, a Serve endpoint
// that the others reach over HTTPS with a test CA, and a peer port on loopback.

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/authtest"
	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/peerlink"
)

func mesh(t *testing.T, specs ...authtest.MeshSpec) *authtest.Mesh {
	t.Helper()
	t.Parallel()
	m, err := authtest.NewMesh(specs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

func waitOthers(t *testing.T, n *authtest.MeshNode, want ...string) *authtest.Roster {
	t.Helper()
	r, err := n.Fyrd.WaitRoster(30*time.Second, func(r *authtest.Roster) bool {
		got := strings.Join(r.Others(), ",")
		for _, w := range want {
			if !strings.Contains(got, w) {
				return false
			}
		}
		return len(r.Others()) == len(want)
	})
	if err != nil {
		t.Fatalf("%s: %v", n.Name, err)
	}
	return r
}

// waitRounds waits until the install has run n more pairing rounds, so "it did
// not pair" is said after it looked.
func waitRounds(t *testing.T, n *authtest.MeshNode, more int64) {
	t.Helper()
	r0, err := n.Fyrd.Roster()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.Fyrd.WaitRoster(30*time.Second, func(r *authtest.Roster) bool { return r.Rounds >= r0.Rounds+more }); err != nil {
		t.Fatal(err)
	}
}

func ownerFronts(t *testing.T, n *authtest.MeshNode) (cookie string) {
	t.Helper()
	link, err := n.Fyrd.OwnerOnce()
	if err != nil {
		t.Fatal(err)
	}
	c, err := authtest.Redeem(n.Fyrd.URL(), link)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func hostsOnline(n *authtest.MeshNode, cookie string) map[string]bool {
	r, err := n.Fyrd.Get("/api/front/state", cookie)
	if err != nil || r.Status != 200 {
		return nil
	}
	var st struct {
		Hosts []struct {
			Name   string
			Online bool
		}
	}
	json.Unmarshal(r.Body, &st)
	out := map[string]bool{}
	for _, h := range st.Hosts {
		out[h.Name] = h.Online
	}
	return out
}

func waitHost(t *testing.T, n *authtest.MeshNode, cookie, host string) {
	t.Helper()
	end := time.Now().Add(30 * time.Second)
	for time.Now().Before(end) {
		if hostsOnline(n, cookie)[host] {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s's front does not follow %s: %v\n%s", n.Name, host, hostsOnline(n, cookie), n.Fyrd.Log())
}

// Condition 1: two installs of one tailnet user, each with Serve up, pair with
// no press, and each front follows the other over the peer link, with no whois
// link in use.
func TestTwoInstallsOfOneUserPairAndFollowEachOther(t *testing.T) {
	m := mesh(t, authtest.MeshSpec{Name: "a", UserID: 1}, authtest.MeshSpec{Name: "b", UserID: 1})
	a, b := m.Node("a"), m.Node("b")
	waitOthers(t, a, "b")
	waitOthers(t, b, "a")
	ca, cb := ownerFronts(t, a), ownerFronts(t, b)
	waitHost(t, a, ca, "b")
	waitHost(t, b, cb, "a")
	// a call through a's front reaches b's host over the link
	if r, err := a.Fyrd.Get(a.Fyrd.HostPath(ca, "b", "/commands"), ca); err != nil || r.Status == 502 || r.Status == 503 {
		t.Fatalf("a call through a's front to b: %v %d %s", err, r.Status, r.Body)
	}
	r, _ := a.Fyrd.Roster()
	if r.WhoisLinkUses != 0 {
		t.Errorf("the whois link was used %d times", r.WhoisLinkUses)
	}
	if !strings.Contains(a.Fyrd.Log(), "paired with b.tail-test.ts.net") {
		t.Errorf("the pairing is not logged:\n%s", a.Fyrd.Log())
	}
}

// Never: another tailnet user, a shared-in node, a tagged node, a node whose
// key expired, and a node that answers the peer port but has no Serve.
func TestNodesThatDoNotPair(t *testing.T) {
	m := mesh(t,
		authtest.MeshSpec{Name: "a", UserID: 1},
		authtest.MeshSpec{Name: "other", UserID: 2},
		authtest.MeshSpec{Name: "shared", UserID: 1, SharedIn: true},
		authtest.MeshSpec{Name: "tagged", Tags: []string{"tag:server"}},
		authtest.MeshSpec{Name: "expired", UserID: 1, Expired: true},
		authtest.MeshSpec{Name: "noserve", UserID: 1, NoServe: true},
	)
	a := m.Node("a")
	waitRounds(t, a, 4)
	r, _ := a.Fyrd.Roster()
	if len(r.Others()) != 0 {
		t.Fatalf("a paired with %v", r.Others())
	}
	// ... and none of them paired with a either
	for _, n := range m.Nodes[1:] {
		rr, err := n.Fyrd.Roster()
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range rr.Others() {
			if o == "a" {
				t.Errorf("%s paired with a", n.Name)
			}
		}
	}
	// the one that has a peer port but no Serve was probed and refused
	if !strings.Contains(a.Fyrd.Log(), "noserve.tail-test.ts.net: the node does not answer on its Serve endpoint") {
		t.Errorf("a did not probe the node with no Serve:\n%s", a.Fyrd.Log())
	}
}

// 3 installs; a and c cannot see each other, so b pairs with both and a learns
// c by sync (a member vouches). A device signed in at a and revoked at a is then
// refused at c, whichever front forwards it.
func TestARevokedDeviceIsRefusedAtAnotherInstallThroughAThird(t *testing.T) {
	m := mesh(t,
		authtest.MeshSpec{Name: "a", UserID: 1, Hide: []string{"c"}},
		authtest.MeshSpec{Name: "b", UserID: 1},
		authtest.MeshSpec{Name: "c", UserID: 1, Hide: []string{"a"}},
	)
	a, b, c := m.Node("a"), m.Node("b"), m.Node("c")
	waitOthers(t, a, "b", "c") // c by vouching
	waitOthers(t, c, "a", "b")
	waitOthers(t, b, "a", "c")

	// a device signed in at a
	cookie := ownerFronts(t, a)
	list, err := a.Fyrd.OwnerCall("GET", "/device/list", nil)
	if err != nil {
		t.Fatal(err)
	}
	var devID string
	for _, d := range list["devices"].([]any) {
		devID = d.(map[string]any)["id"].(string)
	}
	_ = cookie

	// c's peer port, dialed with a's key (as a's front does): the device's id in
	// Fyr-Actor is accepted, then refused once revoked.
	call := func() int {
		return peerCall(t, a, c, "/api/profiles", "v=1; kind=device; id="+devID+"; how=local")
	}
	if got := call(); got != 200 {
		t.Fatalf("a's device at c before the revoke: %d", got)
	}
	if out, err := a.Fyrd.Fyr("device", "revoke", devID); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	end := time.Now().Add(30 * time.Second)
	for call() != 401 {
		if time.Now().After(end) {
			t.Fatalf("c still takes the device revoked at a\n%s", c.Fyrd.Log())
		}
		time.Sleep(50 * time.Millisecond)
	}
	// b, which learned it first, refuses it too
	if got := peerCall(t, a, b, "/api/profiles", "v=1; kind=device; id="+devID+"; how=local"); got != 401 {
		t.Errorf("b: %d (want 401)", got)
	}
}

// peerCall is what a member's front does: a request over the pinned link,
// as install `from`, to install `to`'s host, with the given Fyr-Actor.
func peerCall(t *testing.T, from, to *authtest.MeshNode, path, actor string) int {
	t.Helper()
	status, _ := peerDo(t, from, to, "GET", path, actor, nil)
	return status
}

func peerDo(t *testing.T, from, to *authtest.MeshNode, method, path, actor string, body io.Reader) (int, string) {
	t.Helper()
	key, err := identity.Load(from.Fyrd.Data)
	if err != nil {
		t.Fatal(err)
	}
	toKey, err := identity.Load(to.Fyrd.Data)
	if err != nil {
		t.Fatal(err)
	}
	actor = strings.ReplaceAll(actor, "id=front;", "id="+key.ID()+";") // an install speaks as itself over a link
	port := peerPortOf(t, to)
	d := &peerlink.Dialer{Key: key}
	cl := d.Client(toKey.ID(), net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	req, _ := http.NewRequest(method, "https://peer/"+strings.TrimPrefix(path, "/"), body)
	if actor != "" {
		req.Header.Set("Fyr-Actor", actor)
	}
	if method != "GET" {
		req.Header.Set("X-Fyr", "1")
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := cl.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func peerPortOf(t *testing.T, n *authtest.MeshNode) int {
	t.Helper()
	r, err := n.Fyrd.Roster()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range r.Members {
		if m.Self {
			return m.Port
		}
	}
	t.Fatal("no self in the roster")
	return 0
}

// 4.2 (peer): a key that is not in the roster gets nothing but who this install
// is; a wrong pinned id fails the handshake; /mcp is 404 for a member.
func TestPeerPortRefusesStrangersAndWrongKeys(t *testing.T) {
	m := mesh(t, authtest.MeshSpec{Name: "a", UserID: 1}, authtest.MeshSpec{Name: "b", UserID: 1})
	a, b := m.Node("a"), m.Node("b")
	waitOthers(t, a, "b")
	waitOthers(t, b, "a")
	port := peerPortOf(t, a)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	aKey, _ := identity.Load(a.Fyrd.Data)

	// a stranger: a key no roster holds
	stranger, err := identity.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sc := (&peerlink.Dialer{Key: stranger}).Client(aKey.ID(), addr)
	get := func(c *http.Client, path string) (int, string) {
		resp, err := c.Get("https://peer" + path)
		if err != nil {
			return 0, err.Error()
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if st, body := get(sc, "/.well-known/fyr"); st != 200 || !strings.Contains(body, aKey.ID()) {
		t.Errorf("a stranger asking who this is: %d %s", st, body)
	}
	for _, p := range []string{"/api/state", "/roster", "/api/profiles"} {
		if st, _ := get(sc, p); st != 403 {
			t.Errorf("a stranger at %s: %d (want 403)", p, st)
		}
	}
	// a wrong key pinned: the handshake fails before any request
	wrong := (&peerlink.Dialer{Key: stranger}).Client(stranger.ID(), addr)
	if _, err := wrong.Get("https://peer/.well-known/fyr"); err == nil {
		t.Error("a link pinned to the wrong id worked")
	}
	// the port speaks no plain HTTP
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+addr+"/.well-known/fyr", nil)
	if resp, err := http.DefaultClient.Do(req); err == nil && resp.StatusCode == 200 {
		t.Error("plain HTTP answered on the peer port")
	}
	// a member: /mcp is 404, the host protocol works
	if st, _ := peerDo(t, b, a, "GET", "/mcp", "v=1; kind=install; id=front; how=link", nil); st != 404 {
		t.Errorf("/mcp on the peer port for a member: %d (want 404)", st)
	}
	if st, _ := peerDo(t, b, a, "GET", "/api/profiles", "v=1; kind=install; id=front; how=link", nil); st != 200 {
		t.Errorf("the host protocol for a member: %d", st)
	}
	if st, _ := peerDo(t, b, a, "GET", "/api/profiles", "", nil); st != 401 {
		t.Errorf("a member with no Fyr-Actor: %d (want 401)", st)
	}
}

// 3.7: a member reads every host and drives it unless the owner switched it off
// there. The switch is local to the host.
func TestTheDriveSwitch(t *testing.T) {
	m := mesh(t, authtest.MeshSpec{Name: "a", UserID: 1}, authtest.MeshSpec{Name: "b", UserID: 1})
	a, b := m.Node("a"), m.Node("b")
	waitOthers(t, a, "b")
	waitOthers(t, b, "a")
	aKey, _ := identity.Load(a.Fyrd.Data)
	cmd := `{"commandId":"d1","action":"nope"}`
	actor := "v=1; kind=install; id=front; how=link"
	if st, _ := peerDo(t, a, b, "POST", "/api/command", actor, strings.NewReader(cmd)); st != 400 { // reaches the host: an unknown action
		t.Fatalf("a drives b: %d", st)
	}
	if out, err := b.Fyrd.Fyr("host", "drive", aKey.ID(), "off"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if st, body := peerDo(t, a, b, "POST", "/api/command", actor, strings.NewReader(cmd)); st != 403 {
		t.Errorf("a drives b after it was switched off: %d %s", st, body)
	}
	if st, _ := peerDo(t, a, b, "GET", "/api/profiles", actor, nil); st != 200 {
		t.Errorf("a reads b after the switch: %d", st)
	}
	if out, _ := b.Fyrd.Fyr("host", "list"); !strings.Contains(out, "may not drive this host") {
		t.Errorf("fyr host list: %s", out)
	}
	b.Fyrd.Fyr("host", "drive", aKey.ID(), "on")
	if st, _ := peerDo(t, a, b, "POST", "/api/command", actor, strings.NewReader(cmd)); st != 400 {
		t.Errorf("a drives b again: %d", st)
	}
}
