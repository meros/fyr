// SPEC 0.5 V0.183: the peer side, moved out of fyrd, keeps decision 0030's
// wiring: the peer way in's mode starts and stops the old listeners (5.1), a
// front follows each member over its link and drops a removed one (3.6,
// Amendment 1), the devices stay in step with the roster, and the owner
// socket's routes answer with their status codes.
package peerwire

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/front"
	"github.com/meros/fyr/internal/identity"
	"github.com/meros/fyr/internal/peer"
	"github.com/meros/fyr/internal/peerlink"
	"github.com/meros/fyr/internal/roster"
)

type fakeFront struct {
	mu      sync.Mutex
	follows map[string]string // id → addr
	dropped []string
}

func (f *fakeFront) AllOnPeerLink() (bool, string) { return false, "one host on the old link" }
func (f *fakeFront) DropPeers(member func(string) bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id := range f.follows {
		if !member(id) {
			f.dropped = append(f.dropped, id)
			delete(f.follows, id)
		}
	}
}
func (f *fakeFront) UsePeerLink(id, name string, ep front.PeerEndpoint) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.follows[id] = ep.Addr
}
func (f *fakeFront) get() (map[string]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := map[string]string{}
	for k, v := range f.follows {
		m[k] = v
	}
	return m, append([]string(nil), f.dropped...)
}

type fakeAuth struct {
	mu       sync.Mutex
	mode     string
	refused  []string
	revoked  []string
	signed   []auth.Device
	onMode   func(way, mode string)
	onDevice func(auth.Device)
	onRevoke func([]string)
	gate     func() (bool, string)
	links    func() int
}

func (a *fakeAuth) Mode(string) string     { a.mu.Lock(); defer a.mu.Unlock(); return a.mode }
func (a *fakeAuth) Success(string, string) {}
func (a *fakeAuth) WouldRefuse(way string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refused = append(a.refused, way)
}
func (a *fakeAuth) SetPeerGate(g func() (bool, string), l func() int) { a.gate, a.links = g, l }
func (a *fakeAuth) OnMode(f func(way, mode string))                   { a.onMode = f }
func (a *fakeAuth) Signed() []auth.Device                             { return a.signed }
func (a *fakeAuth) OnDevice(f func(auth.Device))                      { a.onDevice = f }
func (a *fakeAuth) OnRevoke(f func([]string))                         { a.onRevoke = f }
func (a *fakeAuth) Revoke(id, by string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.revoked = append(a.revoked, id+" by "+by)
	return []string{id}
}

type fakeOld struct {
	mu    sync.Mutex
	sets  []bool
	onUse func()
}

func (o *fakeOld) Set(on bool)       { o.mu.Lock(); o.sets = append(o.sets, on); o.mu.Unlock() }
func (o *fakeOld) SetOnUse(f func()) { o.onUse = f }

type fakeHost struct{ onWhois func() }

func (h *fakeHost) Members(*peer.Env) http.Handler { return http.NotFoundHandler() }
func (h *fakeHost) SetOnWhois(f func())            { h.onWhois = f }

type rig struct {
	env  *peer.Env
	ros  *roster.Roster
	key  *identity.Key
	port atomic.Int64
	dir  string
}

func deviceKnown(r *roster.Roster, id string) bool {
	for _, s := range r.Devices() {
		if s.Subject == id {
			return true
		}
	}
	return false
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	key, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	ros, err := roster.Open(dir, key, "desk", nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{ros: ros, key: key, dir: dir}
	r.env = &peer.Env{Key: key, Roster: ros, Name: "desk", Drive: peer.OpenDrive(dir), Invites: &peer.Invites{}, Logf: t.Logf,
		Port: func() int { return int(r.port.Load()) }}
	return r
}

func (r *rig) member(t *testing.T, name, host string, port int) string {
	t.Helper()
	k, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	r.ros.Add(k.Public, name, host, port)
	return k.ID()
}

func (r *rig) start(t *testing.T, d Deps) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d.Name, d.Key, d.Env, d.Port = "desk", r.key, r.env, &r.port
	// The mesh dials the members as soon as it runs: a real dialer, to a closed
	// port, so no test reaches the network.
	d.Dial = &peerlink.Dialer{Key: r.key, Resolve: func(string) string { return "127.0.0.1:1" }}
	r.env.Dial = d.Dial
	d.Config.Every = time.Hour
	d.Self = func() (string, string, error) { return "", "", context.Canceled } // no tailnet
	Start(ctx, d)
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal(what)
}

func TestV183AFrontFollowsEachMemberAndDropsARemovedOne(t *testing.T) {
	r := newRig(t)
	b := r.member(t, "laptop", "laptop.lan", 7443)
	c := r.member(t, "nas", "", 0) // no peer port yet: followed, never dialled
	fr := &fakeFront{follows: map[string]string{}}
	r.start(t, Deps{Front: fr})
	got, _ := fr.get()
	if got[b] != "laptop.lan:7443" || got[c] != "" || len(got) != 2 {
		t.Fatalf("follows %v", got)
	}
	if _, ok := got[r.key.ID()]; ok {
		t.Fatal("the front follows itself")
	}
	if err := r.ros.Remove(c); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the removed member was not dropped", func() bool { _, d := fr.get(); return len(d) == 1 && d[0] == c })
}

func TestV183TheModeStartsAndStopsTheOldListeners(t *testing.T) {
	r := newRig(t)
	r.member(t, "laptop", "laptop.lan", 7443)
	st, old, host := &fakeAuth{mode: auth.ModeObserve}, &fakeOld{}, &fakeHost{}
	r.start(t, Deps{Auth: st, Old: old, Host: host})
	if st.onMode == nil || old.onUse == nil || host.onWhois == nil {
		t.Fatal("not wired")
	}
	st.onMode(auth.WayPeer, auth.ModeEnforce)
	st.onMode(auth.WayBrowser, auth.ModeEnforce) // another way: no change
	st.onMode(auth.WayPeer, auth.ModeObserve)
	if len(old.sets) != 2 || old.sets[0] || !old.sets[1] {
		t.Fatalf("sets %v", old.sets)
	}
	old.onUse()
	host.onWhois()
	if len(st.refused) != 2 || st.refused[0] != auth.WayPeer || st.refused[1] != auth.WayPeer {
		t.Fatalf("refused %v", st.refused)
	}
	// Without a front the gate is open; it counts the other members.
	if ok, _ := st.gate(); !ok || st.links() != 1 {
		t.Fatalf("gate %v links %d", ok, st.links())
	}
	if r.env.Enforce() {
		t.Fatal("enforce while observing")
	}
}

func TestV183TheGateAsksTheFront(t *testing.T) {
	r := newRig(t)
	st := &fakeAuth{}
	r.start(t, Deps{Auth: st, Front: &fakeFront{follows: map[string]string{}}})
	if ok, why := st.gate(); ok || why == "" {
		t.Fatalf("%v %q", ok, why)
	}
}

func TestV183DevicesFollowTheRoster(t *testing.T) {
	r := newRig(t)
	st := &fakeAuth{signed: []auth.Device{{ID: "d1", Name: "phone", How: auth.HowTailnet}}}
	r.start(t, Deps{Auth: st})
	if !deviceKnown(r.ros, "d1") {
		t.Fatal("a signed-in device is not in the roster")
	}
	st.onDevice(auth.Device{ID: "d2", Name: "tablet", How: auth.HowTailnet})
	if !deviceKnown(r.ros, "d2") {
		t.Fatal("a new device is not in the roster")
	}
	st.onRevoke([]string{"d2"})
	if !r.ros.DeviceRevoked("d2") {
		t.Fatal("a revoke here did not reach the roster")
	}
	// A revoke from another member ends the session here.
	r.ros.DeviceRevoke("d1")
	eventually(t, "the roster's revoke did not end the session", func() bool {
		st.mu.Lock()
		defer st.mu.Unlock()
		for _, x := range st.revoked {
			if x == "d1 by roster" {
				return true
			}
		}
		return false
	})
}

func TestV183ThePeerPortIsPublished(t *testing.T) {
	r := newRig(t)
	r.start(t, Deps{Config: Config{PeerListen: "127.0.0.1:0"}})
	if r.port.Load() == 0 {
		t.Fatal("no peer port")
	}
	m, _ := r.ros.Member(r.key.ID())
	if m.Port != int(r.port.Load()) {
		t.Fatalf("the roster says port %d, the listener %d", m.Port, r.port.Load())
	}
}

func TestV183Addr(t *testing.T) {
	for _, c := range []struct {
		m    roster.Member
		want string
	}{
		{roster.Member{Name: "a", Host: "a.ts.net", Port: 7443}, "a.ts.net:7443"},
		{roster.Member{Name: "a", Port: 7443}, "a:7443"},
		{roster.Member{Name: "a", Host: "a.ts.net"}, ""},
		{roster.Member{Name: "a", Host: "::1", Port: 1}, "[::1]:1"},
	} {
		if got := Addr(c.m); got != c.want {
			t.Errorf("%+v: %q, want %q", c.m, got, c.want)
		}
	}
}

func TestV183TheOwnerRoutesAnswerWithTheirCodes(t *testing.T) {
	r := newRig(t)
	b := r.member(t, "laptop", "laptop.lan", 7443)
	restarted := make(chan struct{}, 1)
	mux := http.NewServeMux()
	OwnerRoutes(Owner{Env: r.env, Key: r.key, Data: r.dir, WhoisLinkUses: func() int64 { return 3 },
		Restart: func() { restarted <- struct{}{} }})(mux)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	w := do("GET", "/host/list", "")
	var list map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || w.Code != 200 || list["whoisLinkUses"] != 3.0 {
		t.Fatalf("list %d %s", w.Code, w.Body)
	}
	if w := do("POST", "/pair/invite", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"code"`) {
		t.Fatalf("invite %d %s", w.Code, w.Body)
	}
	if w := do("POST", "/pair/join", `{"Code":"nonsense"}`); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"error"`) {
		t.Fatalf("join %d %s", w.Code, w.Body)
	}
	if w := do("POST", "/host/drive", `{"ID":"nobody","On":true}`); w.Code != http.StatusNotFound {
		t.Fatalf("drive unknown %d", w.Code)
	}
	if w := do("POST", "/host/drive", `{"ID":"`+r.key.ID()+`","On":false}`); w.Code != http.StatusNotFound {
		t.Fatalf("drive self %d", w.Code)
	}
	if w := do("POST", "/host/drive", `{"ID":"`+b+`","On":false}`); w.Code != 200 || r.env.Drive.MayDrive(b) {
		t.Fatalf("drive %d", w.Code)
	}
	if w := do("POST", "/host/remove", `{"ID":"nobody"}`); w.Code != http.StatusConflict {
		t.Fatalf("remove unknown %d", w.Code)
	}
	if w := do("POST", "/host/remove", `{"ID":"`+b+`"}`); w.Code != 200 {
		t.Fatalf("remove %d %s", w.Code, w.Body)
	}
	if w := do("POST", "/host/rotate", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"from":"`+r.key.ID()+`"`) {
		t.Fatalf("rotate %d %s", w.Code, w.Body)
	}
	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Fatal("no restart after a rotation")
	}
}

// V0.owner-without-tailnet (#197): Start does not take OwnerLogin from tailnet.Self;
// config's OwnerLogin is written to the roster, and /auth/owner reads and sets it.
func TestOwnerLoginComesFromConfigOrOwnerRouteNotTailnetSelf(t *testing.T) {
	// 1. Self returns a Tailscale user, config has no OwnerLogin: roster stays empty.
	r := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d := Deps{
		Name: "desk", Key: r.key, Env: r.env, Port: &r.port,
		Dial:   &peerlink.Dialer{Key: r.key, Resolve: func(string) string { return "127.0.0.1:1" }},
		Config: Config{Every: time.Hour},
		Self:   func() (string, string, error) { return "tailnet@example.com", "desk.ts.net", nil },
	}
	r.env.Dial = d.Dial
	Start(ctx, d)
	if got := r.ros.OwnerLogin(); got != "" {
		t.Fatalf("Start took ownerLogin %q from tailnet.Self", got)
	}

	// 2. OwnerRoutes: GET/POST /auth/owner sets the roster login and refuses blanks.
	mux := http.NewServeMux()
	OwnerRoutes(Owner{Env: r.env, Key: r.key, Data: r.dir})(mux)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	if w := do("GET", "/auth/owner", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"ownerLogin":""`) {
		t.Fatalf("GET empty: %d %s", w.Code, w.Body)
	}
	for _, bad := range []string{`{"login":""}`, `{"login":"  "}`, `{"login":"has space@example.com"}`} {
		if w := do("POST", "/auth/owner", bad); w.Code != http.StatusBadRequest {
			t.Errorf("POST %s: %d (want 400)", bad, w.Code)
		}
	}
	if w := do("POST", "/auth/owner", `{"login":"owner@example.com"}`); w.Code != 200 || r.ros.OwnerLogin() != "owner@example.com" {
		t.Fatalf("POST valid: %d %s (roster=%q)", w.Code, w.Body, r.ros.OwnerLogin())
	}
	if w := do("GET", "/auth/owner", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"ownerLogin":"owner@example.com"`) || !strings.Contains(w.Body.String(), `"source":"roster"`) {
		t.Fatalf("GET after set: %d %s", w.Code, w.Body)
	}

	// 3. Config's OwnerLogin is written to the roster on Start and wins over the roster.
	r2 := newRig(t)
	ctx2, cancel2 := context.WithCancel(context.Background())
	t.Cleanup(cancel2)
	d2 := Deps{
		Name: "desk2", Key: r2.key, Env: r2.env, Port: &r2.port,
		Dial:   &peerlink.Dialer{Key: r2.key, Resolve: func(string) string { return "127.0.0.1:1" }},
		Config: Config{Every: time.Hour, OwnerLogin: "cfg@example.com"},
		Self:   func() (string, string, error) { return "tailnet@example.com", "desk2.ts.net", nil },
	}
	r2.env.Dial = d2.Dial
	Start(ctx2, d2)
	if got := r2.ros.OwnerLogin(); got != "cfg@example.com" {
		t.Fatalf("config OwnerLogin not recorded in roster: %q", got)
	}
	mux2 := http.NewServeMux()
	OwnerRoutes(Owner{Env: r2.env, Key: r2.key, Data: r2.dir, ConfigOwner: "cfg@example.com"})(mux2)
	w := httptest.NewRecorder()
	mux2.ServeHTTP(w, httptest.NewRequest("GET", "/auth/owner", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ownerLogin":"cfg@example.com"`) || !strings.Contains(w.Body.String(), `"source":"config"`) {
		t.Fatalf("GET config source: %d %s", w.Code, w.Body)
	}
}
