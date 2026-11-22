package authtest_test

// Decision 0030 slice 4 (V0.159): the owner's invite, tagged installs, Hosts.

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/peerlink"
	"github.com/meros/fyr/internal/testkit/authtest"
)

func peerAddr(t *testing.T, n *authtest.MeshNode) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(peerPortOf(t, n)))
}

func inviteCode(t *testing.T, n *authtest.MeshNode) string {
	t.Helper()
	out, err := n.Fyrd.Fyr("pair")
	if err != nil {
		t.Fatalf("fyr pair: %v\n%s", err, out)
	}
	if !strings.Contains(out, "█") {
		t.Errorf("fyr pair shows no QR code:\n%s", out)
	}
	for _, f := range strings.Fields(out) {
		if strings.HasPrefix(f, "fyr1-") {
			return f
		}
	}
	t.Fatalf("no invite in:\n%s", out)
	return ""
}

// 3.6 condition 3 and 4.2 (peer): a (untagged) and c pair by themselves; a tagged
// install b cannot, so the owner opens an invite on a and gives it to b; then c
// learns b by vouching, with no press, and says so.
func TestInvitePairsATaggedInstallAndAnotherLearnsItByVouching(t *testing.T) {
	m := mesh(t,
		authtest.MeshSpec{Name: "a", UserID: 1},
		authtest.MeshSpec{Name: "c", UserID: 1},
		authtest.MeshSpec{Name: "b", Tags: []string{"tag:server"}},
	)
	a, b, c := m.Node("a"), m.Node("b"), m.Node("c")
	waitOthers(t, a, "c") // condition 1, a and c; b is tagged and waits
	waitOthers(t, c, "a")
	waitRounds(t, a, 3)
	if r, _ := a.Fyrd.Roster(); len(r.Others()) != 1 {
		t.Fatalf("a paired with %v before any invite", r.Others())
	}
	if out, _ := a.Fyrd.Fyr("host", "list"); !strings.Contains(out, "not paired: b.tail-test.ts.net") || !strings.Contains(out, "tagged: pair it with an invite") {
		t.Errorf("a does not show the tagged node as a candidate:\n%s", out)
	}

	code := inviteCode(t, a)
	if out, err := b.Fyrd.Fyr("pair", code, "-addr", peerAddr(t, a)); err != nil || !strings.Contains(out, "Paired with a") {
		t.Fatalf("fyr pair CODE: %v\n%s", err, out)
	}
	waitOthers(t, a, "b", "c")
	// b has a at once and learns c by vouching in the next sync: wait for both, not
	// for "exactly a" (under load b can have c before the test looks).
	waitOthers(t, b, "a", "c")
	cr, err := c.Fyrd.WaitRoster(30*time.Second, func(r *authtest.Roster) bool { return strings.Contains(strings.Join(r.Others(), ","), "b") })
	if err != nil {
		t.Fatal(err)
	}
	_ = cr
	if out, _ := c.Fyrd.Fyr("host", "list"); !strings.Contains(out, "vouched for by a") {
		t.Errorf("c does not say b was vouched for by a:\n%s", out)
	}
}

// The invite's rows: used once; five wrong tries end it; a relay with a key of its
// own cannot complete it (the handshake pins the id); the secret never reaches it.
func TestInviteReplayWrongTriesAndARelay(t *testing.T) {
	m := mesh(t,
		authtest.MeshSpec{Name: "a", UserID: 1},
		authtest.MeshSpec{Name: "b", Tags: []string{"tag:server"}},
		authtest.MeshSpec{Name: "d", Tags: []string{"tag:server"}},
	)
	a, b, d := m.Node("a"), m.Node("b"), m.Node("d")
	addr := peerAddr(t, a)

	// a man in the middle: a listener with its own key at another address
	relayKey, err := identity.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	relayHits := 0
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	rs, err := peerlink.Serve(ln, relayKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { relayHits++ }))
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	code := inviteCode(t, a)
	if out, err := b.Fyrd.Fyr("pair", code, "-addr", ln.Addr().String()); err == nil {
		t.Fatalf("an invite completed through a relay with its own key:\n%s", out)
	}
	if relayHits != 0 {
		t.Errorf("the relay saw %d requests: the secret must not leave the pinned channel", relayHits)
	}
	// the invite still works at the real address ...
	if out, err := b.Fyrd.Fyr("pair", code, "-addr", addr); err != nil {
		t.Fatalf("the invite after the relay: %v\n%s", err, out)
	}
	// ... once
	if out, err := d.Fyrd.Fyr("pair", code, "-addr", addr); err == nil {
		t.Errorf("a used invite worked again:\n%s", out)
	}
	// five wrong tries end a new one
	code2 := inviteCode(t, a)
	wrong := code2[:len(code2)-10] + "ABCDEFGH23"
	for i := 0; i < 5; i++ {
		if _, err := d.Fyrd.Fyr("pair", wrong, "-addr", addr); err == nil {
			t.Fatal("a wrong secret worked")
		}
	}
	if out, err := d.Fyrd.Fyr("pair", code2, "-addr", addr); err == nil {
		t.Errorf("the right invite worked after five wrong tries:\n%s", out)
	}
}

// 3.4: a tagged install serves no tailnet browser until it is paired, and then
// does: it learns the owner's login from the roster.
func TestATaggedInstallServesNoTailnetBrowserUntilPaired(t *testing.T) {
	m := mesh(t, authtest.MeshSpec{Name: "a", UserID: 1}, authtest.MeshSpec{Name: "t", Tags: []string{"tag:server"}})
	a, tg := m.Node("a"), m.Node("t")
	phone := authtest.Node{Name: "phone.tail-test.ts.net", IP: "100.64.1.50", UserID: 1, Online: true}
	tg.TS.AddNode(phone)
	sv, err := tg.TS.ServeFor(phone, tg.Fyrd.Sock(auth.ServeSocket))
	if err != nil {
		t.Fatal(err)
	}
	defer sv.Close()
	r := status(t, sv.URL, authtest.Request{Path: "/api/front/state"})
	if r.Status != 403 || r.Session() != "" {
		t.Fatalf("an unpaired tagged install through Serve: %d session %q (want 403 and none)", r.Status, r.Session())
	}
	code := inviteCode(t, a)
	if out, err := tg.Fyrd.Fyr("pair", code, "-addr", peerAddr(t, a)); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	end := time.Now().Add(30 * time.Second)
	for {
		r = status(t, sv.URL, authtest.Request{Path: "/api/front/state"})
		if r.Status == 200 && r.Session() != "" {
			break
		}
		if time.Now().After(end) {
			t.Fatalf("a paired tagged install still serves no tailnet browser: %d\n%s", r.Status, tg.Fyrd.Log())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if out, _ := tg.Fyrd.Fyr("host", "list"); !strings.Contains(out, "a") {
		t.Errorf("fyr host list: %s", out)
	}
}
