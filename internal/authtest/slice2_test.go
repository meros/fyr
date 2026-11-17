package authtest_test

// Decision 0030 slice 2 (V0.157): sessions decide, and Devices. Real fyrds
// behind the fake tailscaled; the fyr command and the owner socket as the
// owner uses them.

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/authtest"
)

func doc() map[string]string {
	return map[string]string{"Sec-Fetch-Dest": "document", "Sec-Fetch-Mode": "navigate", "Accept": "text/html"}
}

func status(t *testing.T, base string, r authtest.Request) *authtest.Response {
	t.Helper()
	resp, err := authtest.Do(base, r)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// The loopback and browser rows of decision 0030 4.2: in enforce a cookie gets
// the API and no cookie gets 401 (a page for a navigation, the client's files
// still served); `fyr auth observe` restores the old way in; `fyr open` never
// needs the mode. In observe nothing is refused.
func TestBrowserEnforceAndTheWayBack(t *testing.T) {
	l := live(t)
	if m, _ := l.Laptop.Mode("browser"); m != "observe" {
		t.Fatalf("a fresh install must observe, got %q", m)
	}
	if r := status(t, l.Laptop.URL(), authtest.Request{Path: "/api/state"}); r.Status != 200 {
		t.Fatalf("observe, no cookie: %d (the old way in must keep working)", r.Status)
	}
	if out, err := l.Laptop.Fyr("auth", "enforce"); err != nil || !strings.Contains(out, "enforce") {
		t.Fatalf("fyr auth enforce: %v %s", err, out)
	}
	for name, c := range map[string]struct {
		req  authtest.Request
		want int
	}{
		"the local cookie":              {authtest.Request{Path: "/api/state", Cookie: l.Local}, 200},
		"the tailnet cookie":            {authtest.Request{Path: "/api/state", Cookie: l.Tailnet}, 200},
		"no cookie":                     {authtest.Request{Path: "/api/state"}, 401},
		"no cookie, a command":          {authtest.Request{Method: "POST", Path: "/api/command", Header: map[string]string{"Origin": l.Laptop.URL()}, Body: strings.NewReader("{}")}, 401},
		"a made-up cookie":              {authtest.Request{Path: "/api/state", Cookie: "nope"}, 401},
		"no cookie, a navigation":       {authtest.Request{Path: "/", Header: doc()}, 401},
		"no cookie, a script":           {authtest.Request{Path: "/app.js", Header: map[string]string{"Sec-Fetch-Dest": "script"}}, 404},
		"no cookie, the well-known":     {authtest.Request{Path: "/.well-known/fyr"}, 200},
		"no cookie, a preview":          {authtest.Request{Path: "/api/previews/pv_none"}, 404},
		"a header only tailscaled sets": {authtest.Request{Path: "/api/state", Cookie: l.Local, Header: map[string]string{"Tailscale-User-Login": l.OwnerLogin}}, 400},
	} {
		if r := status(t, l.Laptop.URL(), c.req); r.Status != c.want {
			t.Errorf("enforce, %s: %d (want %d) %.80s", name, r.Status, c.want, r.Body)
		}
	}
	if r := status(t, l.Laptop.URL(), authtest.Request{Path: "/", Header: doc()}); !strings.Contains(string(r.Body), "This device is not signed in") {
		t.Errorf("the navigation page: %.200s", r.Body)
	}
	if out, err := l.Laptop.Fyr("open", "-data", l.Laptop.Data); err == nil {
		_ = out // fyr open execs a browser; the link itself is checked below through the socket
	}
	link, err := l.Laptop.OwnerOnce()
	if err != nil {
		t.Fatalf("fyr open's socket call in enforce: %v", err)
	}
	if s, err := authtest.Redeem(l.Laptop.URL(), link); err != nil || s == "" {
		t.Errorf("a link in enforce: %v", err)
	}
	// The way back.
	if _, err := l.Laptop.Fyr("auth", "observe"); err != nil {
		t.Fatal(err)
	}
	if r := status(t, l.Laptop.URL(), authtest.Request{Path: "/api/state"}); r.Status != 200 {
		t.Errorf("after fyr auth observe, no cookie: %d", r.Status)
	}
	if r := status(t, l.Laptop.URL(), authtest.Request{Path: "/api/state", Cookie: l.Local}); r.Status != 200 {
		t.Errorf("after fyr auth observe, the cookie: %d", r.Status)
	}
	out, _ := l.Laptop.Fyr("auth", "status")
	if !strings.Contains(out, "browser  observe") {
		t.Errorf("fyr auth status: %s", out)
	}
}

func extraNodes(l *authtest.Live) (phone2, shared authtest.Node) {
	phone2 = authtest.Node{Name: "tablet.tail-test.ts.net", IP: "100.64.0.20", UserID: 1, Online: true}
	shared = authtest.Node{Name: "shared.tail-test.ts.net", IP: "100.64.0.21", UserID: 1, SharedIn: true, Online: true}
	l.TS.AddNode(phone2)
	l.TS.AddNode(shared)
	return
}

// 3.4 and 4.2 (serve.sock): in enforce the owner's approved node gets a
// session silently, a new node waits for approval, and another user's node, a
// tagged node and a node shared in get nothing.
func TestTailnetNodesInEnforce(t *testing.T) {
	l := live(t)
	phone2, shared := extraNodes(l)
	// The phone signs in once while observing: its node is approved (5.2).
	if r := status(t, l.Phones.URL, authtest.Request{Path: "/api/state"}); r.Status != 200 || r.Session() == "" {
		t.Fatalf("observe: %d", r.Status)
	}
	if err := l.Laptop.SetMode("browser", "enforce"); err != nil {
		t.Fatal(err)
	}
	if r := status(t, l.Phones.URL, authtest.Request{Path: "/api/state"}); r.Status != 200 || r.Session() == "" {
		t.Fatalf("the approved phone, no cookie, in enforce: %d %s", r.Status, r.Body)
	}
	for name, n := range map[string]authtest.Node{"another user": l.Other, "a tagged node": l.Tag, "a node shared in": shared} {
		s, err := l.TS.ServeFor(n, l.Laptop.Sock(auth.ServeSocket))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if r := status(t, s.URL, authtest.Request{Path: "/api/state"}); r.Session() != "" || r.Status == 200 && name != "a tagged node" {
			t.Errorf("%s: %d session %q (want nothing)", name, r.Status, r.Session())
		}
	}
	// A new node of the owner's waits. It gets a session that does nothing.
	s2, _ := l.TS.ServeFor(phone2, l.Laptop.Sock(auth.ServeSocket))
	defer s2.Close()
	r := status(t, s2.URL, authtest.Request{Path: "/api/state"})
	if r.Status != 401 || r.Session() == "" {
		t.Fatalf("a new node in enforce: %d session %q (want 401 and a waiting session)", r.Status, r.Session())
	}
	pend := r.Session()
	if r2 := status(t, s2.URL, authtest.Request{Path: "/api/state", Cookie: pend}); r2.Status != 401 {
		t.Errorf("a waiting session got the API: %d", r2.Status)
	}
	st := status(t, s2.URL, authtest.Request{Path: "/auth/status", Cookie: pend})
	var sb struct{ State, Code string }
	json.Unmarshal(st.Body, &sb)
	if sb.State != "pending" || len(sb.Code) != 8 {
		t.Fatalf("status %s", st.Body)
	}
	// The owner approves it from the shell, and the same session works.
	if out, err := l.Laptop.Fyr("device", "approve", sb.Code); err != nil || !strings.Contains(out, "Approved") {
		t.Fatalf("fyr device approve: %v %s", err, out)
	}
	if r3 := status(t, s2.URL, authtest.Request{Path: "/api/state", Cookie: pend}); r3.Status != 200 {
		t.Errorf("after approval: %d", r3.Status)
	}
	// ... and its node is known now: a cleared browser signs in by itself.
	if r4 := status(t, s2.URL, authtest.Request{Path: "/api/state"}); r4.Status != 200 {
		t.Errorf("an approved node, no cookie: %d", r4.Status)
	}
}

// Q4: a fresh install lets the first tailnet node in at once; the second waits.
func TestFirstNodeOfAnEmptyInstallIsApprovedAndTheSecondWaits(t *testing.T) {
	l := live(t)
	phone2, _ := extraNodes(l)
	f, err := authtest.Start(authtest.Options{Name: "fresh", Tailscale: l.TS, Extra: map[string]any{"serveSocketUsers": []int{authtest.OSUID()}}})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Stop()
	if err := f.SetMode("browser", "enforce"); err != nil {
		t.Fatal(err)
	}
	s1, _ := l.TS.ServeFor(l.Phone, f.Sock(auth.ServeSocket))
	s2, _ := l.TS.ServeFor(phone2, f.Sock(auth.ServeSocket))
	defer s1.Close()
	defer s2.Close()
	if r := status(t, s1.URL, authtest.Request{Path: "/api/state"}); r.Status != 200 || r.Session() == "" {
		t.Fatalf("the first node: %d", r.Status)
	}
	if r := status(t, s2.URL, authtest.Request{Path: "/api/state"}); r.Status != 401 {
		t.Fatalf("the second node: %d (want 401: it waits)", r.Status)
	}
}

// 3.4 and 4.3 (a replayed code): a browser with no session asks for a code,
// a signed-in device approves it, once; five wrong tries end every code.
func TestApprovalCodeFlow(t *testing.T) {
	l := live(t)
	l.Laptop.SetMode("browser", "enforce")
	r := status(t, l.Laptop.URL(), authtest.Request{Method: "POST", Path: "/auth/code"})
	var cb struct {
		OK   bool
		Code string
	}
	json.Unmarshal(r.Body, &cb)
	if r.Status != 200 || len(cb.Code) != 8 || r.Session() == "" {
		t.Fatalf("ask for a code: %d %s", r.Status, r.Body)
	}
	waiting := r.Session()
	approve := func(code string) *authtest.Response {
		return status(t, l.Laptop.URL(), authtest.Request{Method: "POST", Path: "/api/auth/approve", Cookie: l.Local,
			Header: map[string]string{"Origin": l.Laptop.URL(), "Content-Type": "application/json"}, Body: strings.NewReader(`{"code":"` + code + `"}`)})
	}
	if r := status(t, l.Laptop.URL(), authtest.Request{Path: "/api/state", Cookie: waiting}); r.Status != 401 {
		t.Fatalf("waiting: %d", r.Status)
	}
	if r := approve(cb.Code); r.Status != 200 {
		t.Fatalf("approve on a signed-in device: %d %s", r.Status, r.Body)
	}
	if r := status(t, l.Laptop.URL(), authtest.Request{Path: "/api/state", Cookie: waiting}); r.Status != 200 {
		t.Errorf("the approved session: %d", r.Status)
	}
	if r := approve(cb.Code); r.Status != 409 {
		t.Errorf("a replayed code: %d (want 409)", r.Status)
	}
	// Approval needs a session: the waiting browser cannot approve itself.
	r2 := status(t, l.Laptop.URL(), authtest.Request{Method: "POST", Path: "/auth/code"})
	json.Unmarshal(r2.Body, &cb)
	self := status(t, l.Laptop.URL(), authtest.Request{Method: "POST", Path: "/api/auth/approve", Cookie: r2.Session(),
		Header: map[string]string{"Origin": l.Laptop.URL()}, Body: strings.NewReader(`{"code":"` + cb.Code + `"}`)})
	if self.Status != 401 && self.Status != 403 {
		t.Errorf("a waiting session approved itself: %d", self.Status)
	}
	for i := 0; i < 5; i++ {
		approve("WRONGCODE")
	}
	if r := approve(cb.Code); r.Status != 409 {
		t.Errorf("a sixth try after five wrong ones: %d (want 409)", r.Status)
	}
}

// Links: one use; `fyr device add` makes a link and a QR; a device added by
// another is revoked with it (the cascade).
func TestDeviceAddLinkAndTheCascade(t *testing.T) {
	l := live(t)
	out, err := l.Laptop.Fyr("device", "add", "-name", "Kitchen tablet")
	if err != nil || !strings.Contains(out, "/auth/once#") || !strings.Contains(out, "█") {
		t.Fatalf("fyr device add: %v\n%s", err, out)
	}
	var link string
	for _, f := range strings.Fields(out) {
		if strings.Contains(f, "/auth/once#") {
			link = f
		}
	}
	s, err := authtest.Redeem(l.Laptop.URL(), link)
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if _, err := authtest.Redeem(l.Laptop.URL(), link); err == nil {
		t.Error("a link worked twice")
	}
	// A signed-in device adds one: the new device is its child.
	add := status(t, l.Laptop.URL(), authtest.Request{Method: "POST", Path: "/api/auth/add", Cookie: l.Local,
		Header: map[string]string{"Origin": l.Laptop.URL()}, Body: strings.NewReader(`{"name":"child"}`)})
	var ab struct{ URL, SVG string }
	json.Unmarshal(add.Body, &ab)
	if add.Status != 200 || !strings.Contains(ab.URL, "/auth/once#") || !strings.HasPrefix(ab.SVG, "<svg") {
		t.Fatalf("Add a device: %d %s", add.Status, add.Body)
	}
	child, err := authtest.Redeem(l.Laptop.URL(), ab.URL)
	if err != nil {
		t.Fatal(err)
	}
	l.Laptop.SetMode("browser", "enforce")
	if r := status(t, l.Laptop.URL(), authtest.Request{Path: "/api/state", Cookie: child}); r.Status != 200 {
		t.Fatalf("the child: %d", r.Status)
	}
	if r := status(t, l.Laptop.URL(), authtest.Request{Path: "/api/state", Cookie: s}); r.Status != 200 {
		t.Fatalf("the shell-added device: %d", r.Status)
	}
	// Revoke the Local device (the parent of the child) from another device.
	rv := status(t, l.Laptop.URL(), authtest.Request{Method: "POST", Path: "/api/auth/revoke", Cookie: l.Tailnet,
		Header: map[string]string{"Origin": l.Laptop.URL()}, Body: strings.NewReader(`{"id":"` + l.LocalID + `"}`)})
	if rv.Status != 200 {
		t.Fatalf("revoke: %d %s", rv.Status, rv.Body)
	}
	for name, c := range map[string]string{"the revoked device": l.Local, "its child": child} {
		if r := status(t, l.Laptop.URL(), authtest.Request{Path: "/api/state", Cookie: c}); r.Status != 401 {
			t.Errorf("%s after revoke: %d (want 401)", name, r.Status)
		}
	}
	if r := status(t, l.Laptop.URL(), authtest.Request{Path: "/api/state", Cookie: s}); r.Status != 200 {
		t.Errorf("a device the revoked one did not add: %d", r.Status)
	}
}

// 4.3 (a stolen cookie after revoke): the cookie gets 401, its open stream
// closes within a second, and its tailnet node cannot enrol again.
func TestRevokeClosesTheStreamAndBlocksTheNode(t *testing.T) {
	l := live(t)
	// The phone's session, and its node recorded through Serve.
	if r := status(t, l.Phones.URL, authtest.Request{Path: "/api/state", Cookie: l.Tailnet}); r.Status != 200 {
		t.Fatalf("phone: %d", r.Status)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", l.Laptop.URL()+"/api/stream", nil)
	req.AddCookie(&http.Cookie{Name: auth.Cookie, Value: l.Tailnet})
	resp, err := http.DefaultClient.Do(req)
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
	if out, err := l.Laptop.Fyr("device", "revoke", l.TailnetID); err != nil || !strings.Contains(out, "Revoked") {
		t.Fatalf("fyr device revoke: %v %s", err, out)
	}
	select {
	case <-closed:
	case <-time.After(1500 * time.Millisecond):
		t.Error("the revoked device's stream stayed open")
	}
	l.Laptop.SetMode("browser", "enforce")
	if r := status(t, l.Laptop.URL(), authtest.Request{Path: "/api/state", Cookie: l.Tailnet}); r.Status != 401 {
		t.Errorf("the stolen cookie: %d", r.Status)
	}
	if r := status(t, l.Phones.URL, authtest.Request{Path: "/api/state"}); r.Status != 403 || r.Session() != "" {
		t.Errorf("the revoked device's node enrolled again: %d session %q", r.Status, r.Session())
	}
}

// 3.2 and 4.3: Origin and Sec-Fetch-Site rules, before the cookie.
func TestOriginAndFetchSiteRules(t *testing.T) {
	l := live(t)
	base := l.Laptop.URL()
	for name, c := range map[string]struct {
		req  authtest.Request
		want int
	}{
		"a same-origin command":      {authtest.Request{Method: "POST", Path: "/api/command", Cookie: l.Local, Header: map[string]string{"Origin": base}, Body: strings.NewReader(`{"commandId":"o1","action":"nope"}`)}, 400},
		"a cross-origin command":     {authtest.Request{Method: "POST", Path: "/api/command", Cookie: l.Local, Header: map[string]string{"Origin": "https://evil.example"}, Body: strings.NewReader(`{}`)}, 403},
		"a null origin":              {authtest.Request{Method: "POST", Path: "/api/command", Cookie: l.Local, Header: map[string]string{"Origin": "null"}, Body: strings.NewReader(`{}`)}, 403},
		"a cross-origin stream":      {authtest.Request{Path: "/api/stream", Cookie: l.Local, Header: map[string]string{"Origin": "https://evil.example"}}, 403},
		"a cross-site credentialed":  {authtest.Request{Path: "/api/state", Cookie: l.Local, Header: map[string]string{"Sec-Fetch-Site": "cross-site"}}, 403},
		"a same-site credentialed":   {authtest.Request{Path: "/api/state", Cookie: l.Local, Header: map[string]string{"Sec-Fetch-Site": "same-site"}}, 403},
		"a same-origin credentialed": {authtest.Request{Path: "/api/state", Cookie: l.Local, Header: map[string]string{"Sec-Fetch-Site": "same-origin"}}, 200},
		"a rebinding Host":           {authtest.Request{Path: "/api/state", Cookie: l.Local, Host: "evil.example:7777"}, 403},
	} {
		if r := status(t, base, c.req); r.Status != c.want {
			t.Errorf("%s: %d (want %d) %.80s", name, r.Status, c.want, r.Body)
		}
	}
	// Serve's origin is the public name.
	r := status(t, l.Phones.URL, authtest.Request{Method: "POST", Path: "/api/command", Cookie: l.Tailnet, Header: map[string]string{"Origin": "https://" + l.Self.Name + ":7443"}, Body: strings.NewReader(`{"commandId":"o2","action":"nope"}`)})
	if r.Status != 400 {
		t.Errorf("a command with Serve's own origin: %d", r.Status)
	}
	if r := status(t, l.Phones.URL, authtest.Request{Method: "POST", Path: "/api/command", Cookie: l.Tailnet, Header: map[string]string{"Origin": "https://evil.example"}, Body: strings.NewReader(`{}`)}); r.Status != 403 {
		t.Errorf("a cross-origin command through Serve: %d", r.Status)
	}
}
