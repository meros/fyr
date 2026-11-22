package authtest_test

// V0.owner-without-tailnet (#197): owner identity comes from config or the
// first-run step (`fyr auth owner <login>`), never from tailnet.Self; and with
// tailscaled absent fyrd logs a single line saying the tailnet is off.

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/authtest"
)

// A fresh install with tailscaled running (untagged) and no ownerLogin in
// config does not fall back to tailnet.Self: Tailscale Serve refuses sign-in
// until `fyr auth owner <login>` sets it in the roster, which survives restart.
func TestNoFallbackToTailnetSelfAndFirstRunOwnerStep(t *testing.T) {
	t.Parallel()
	tsDir, rm, err := authtest.NewDataDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rm)
	self := authtest.Node{Name: "desk.tail-test.ts.net", IP: "100.64.2.1", UserID: 1, Online: true}
	phone := authtest.Node{Name: "phone.tail-test.ts.net", IP: "100.64.2.2", UserID: 1, Online: true}
	ts, err := authtest.NewTailscaled(tsDir, self, authtest.User{ID: 1, Login: "owner@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ts.Close)
	ts.AddNode(phone)

	f, err := authtest.Start(authtest.Options{
		Name:      "desk",
		Tailscale: ts,
		Extra:     map[string]any{"serveSocketUsers": []int{os.Getuid()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Stop)

	sv, err := ts.ServeFor(phone, f.Sock(auth.ServeSocket))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sv.Close)

	// 1. Before setting ownerLogin, Serve refuses the tailnet user (no fallback to tailnet.Self).
	if r := status(t, sv.URL, authtest.Request{Path: "/api/front/state"}); r.Status != 403 || r.Session() != "" {
		t.Fatalf("Serve before setting owner: %d session %q (want 403 and none)\n%s", r.Status, r.Session(), f.Log())
	}
	if out, err := f.Fyr("auth", "owner"); err != nil || !strings.Contains(out, "No Tailscale owner login is set") {
		t.Fatalf("fyr auth owner (unset): %v\n%s", err, out)
	}

	// 2. First-run step: set the owner login over owner.sock.
	if out, err := f.Fyr("auth", "owner", "owner@example.com"); err != nil || !strings.Contains(out, "owner@example.com") {
		t.Fatalf("fyr auth owner SET: %v\n%s", err, out)
	}
	if out, err := f.Fyr("auth", "owner"); err != nil || !strings.Contains(out, "owner@example.com (roster)") {
		t.Fatalf("fyr auth owner (set): %v\n%s", err, out)
	}
	if r := status(t, sv.URL, authtest.Request{Path: "/api/front/state"}); r.Status != 200 || r.Session() == "" {
		t.Fatalf("Serve after fyr auth owner: %d session %q (want 200 and session)", r.Status, r.Session())
	}

	// 3. Survives restart without ownerLogin in config.json.
	if err := f.Restart(); err != nil {
		t.Fatal(err)
	}
	sv2, err := ts.ServeFor(phone, f.Sock(auth.ServeSocket))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sv2.Close)
	if r := status(t, sv2.URL, authtest.Request{Path: "/api/front/state"}); r.Status != 200 || r.Session() == "" {
		t.Fatalf("Serve after restart: %d session %q\n%s", r.Status, r.Session(), f.Log())
	}
}

// Acceptance #1 of #197: with tailscaled absent, fyrd starts and serves the
// local browser with no error in the log beyond one line saying the tailnet is off.
func TestWithoutTailscaledStartsQuietlyWithSingleTailnetLine(t *testing.T) {
	t.Parallel()
	f, err := authtest.Start(authtest.Options{Name: "solo"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Stop)

	link, err := f.OwnerOnce()
	if err != nil {
		t.Fatalf("owner socket: %v", err)
	}
	cookie, err := authtest.Redeem(f.URL(), link)
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if r, err := f.Get("/api/front/state", cookie); err != nil || r.Status != 200 {
		t.Fatalf("GET /api/front/state: %v %d", err, r.Status)
	}

	log := f.Log()
	var tailnetLines []string
	for _, line := range strings.Split(log, "\n") {
		low := strings.ToLower(line)
		if strings.Contains(low, "tailnet") || strings.Contains(low, "tailscaled") {
			tailnetLines = append(tailnetLines, line)
		}
		if strings.Contains(low, "this install is tagged") {
			t.Errorf("spurious tagged-node log without tailscaled: %q", line)
		}
		if strings.Contains(low, "no such file or directory") && strings.Contains(low, "tailscale") {
			t.Errorf("raw tailscale dial error in log: %q", line)
		}
	}
	if len(tailnetLines) != 1 {
		t.Errorf("want exactly 1 tailnet log line without tailscaled, got %d:\n%s", len(tailnetLines), strings.Join(tailnetLines, "\n"))
	}
}

// Condition 1 auto-pairing does not run until an owner login is configured
// (via config or `fyr auth owner`); once set on both, they pair automatically.
func TestNoAutoPairWithoutConfiguredOwnerLogin(t *testing.T) {
	m := mesh(t,
		authtest.MeshSpec{Name: "a", UserID: 1, NoOwnerLogin: true},
		authtest.MeshSpec{Name: "b", UserID: 1, NoOwnerLogin: true},
	)
	a, b := m.Node("a"), m.Node("b")
	waitRounds(t, a, 3)
	if r, _ := a.Fyrd.Roster(); len(r.Others()) != 0 {
		t.Fatalf("a auto-paired without ownerLogin configured: %v", r.Others())
	}
	if out, err := a.Fyrd.Fyr("auth", "owner", "owner@example.com"); err != nil {
		t.Fatalf("a fyr auth owner: %v\n%s", err, out)
	}
	if out, err := b.Fyrd.Fyr("auth", "owner", "owner@example.com"); err != nil {
		t.Fatalf("b fyr auth owner: %v\n%s", err, out)
	}
	waitOthers(t, a, "b")
	waitOthers(t, b, "a")
	if _, err := a.Fyrd.WaitRoster(30*time.Second, func(r *authtest.Roster) bool { return r.OwnerLogin == "owner@example.com" }); err != nil {
		t.Fatalf("ownerLogin not in roster: %v", err)
	}
}
