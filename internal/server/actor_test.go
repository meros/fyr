package server

// SPEC V0.156 (decision 0030 slice 1): the host reads Fyr-Actor on the pipe
// and the peer listener only; the peer listener also refuses a browser.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/authn"
)

func serveReq(h http.Handler, method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Host = "100.64.0.1:7778"
	for k, v := range hdr {
		if strings.EqualFold(k, "Host") {
			r.Host = v
			continue
		}
		r.Header.Set(k, v)
	}
	if method == "POST" {
		r.Header.Set("X-Fyr", "1")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const goodActor = "v=1; kind=device; id=dev_x; how=tailnet"

// The rebinding row of decision 0030 4.3: a request with an Origin, or with a
// Host that is not this node's, gets nothing on the whois listener.
func TestPeerHandlerRefusesABrowserAndAForeignHost(t *testing.T) {
	s := &Server{OwnerLogin: "me@example.com", MCP: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("mcp")) })}
	h := s.PeerHandler(fakePeers{"192.0.2.1:1234": "me@example.com"})
	for name, c := range map[string]struct {
		hdr  map[string]string
		path string
		want int
	}{
		"a front":                    {map[string]string{"Fyr-Actor": goodActor}, "/api/profiles", 200},
		"a front by its ts.net name": {map[string]string{"Fyr-Actor": goodActor, "Host": "mac.tail-test.ts.net:7778"}, "/api/profiles", 200},
		"a browser (Origin)":         {map[string]string{"Fyr-Actor": goodActor, "Origin": "https://evil.example"}, "/api/profiles", 403},
		"a rebinding Host":           {map[string]string{"Fyr-Actor": goodActor, "Host": "evil.example:7778"}, "/api/profiles", 403},
		"localhost":                  {map[string]string{"Fyr-Actor": goodActor, "Host": "localhost:7778"}, "/api/profiles", 403},
		"/mcp is not served here":    {map[string]string{"Fyr-Actor": goodActor}, "/mcp", 404},
		"a malformed Fyr-Actor":      {map[string]string{"Fyr-Actor": "v=1; kind=agent; id=x; how=local"}, "/api/profiles", 401},
	} {
		if w := serveReq(h, "GET", c.path, c.hdr); w.Code != c.want {
			t.Errorf("%s: %d (want %d) %s", name, w.Code, c.want, w.Body)
		}
	}
	// The owner's main port keeps /mcp (agents' bearer).
	if w := serveReq(s.Handler(), "GET", "/mcp", map[string]string{"Host": "127.0.0.1:7777"}); w.Code != 200 || w.Body.String() != "mcp" {
		t.Errorf("/mcp on the main port: %d %s", w.Code, w.Body)
	}
}

// Slice 1's lockout guard: a front of an older build sends no Fyr-Actor, is
// still served by a new host, and is counted.
func TestPeerHandlerServesALegacyFront(t *testing.T) {
	s := &Server{OwnerLogin: "me@example.com"}
	h := s.PeerHandler(fakePeers{"192.0.2.1:1234": "me@example.com"})
	before := s.LegacyFronts()
	if w := serveReq(h, "GET", "/api/profiles", nil); w.Code != 200 {
		t.Fatalf("a front without Fyr-Actor: %d %s", w.Code, w.Body)
	}
	if s.LegacyFronts() != before+1 {
		t.Errorf("not counted: %d -> %d", before, s.LegacyFronts())
	}
	if serveReq(h, "GET", "/api/profiles", map[string]string{"Fyr-Actor": goodActor}); s.LegacyFronts() != before+1 {
		t.Error("a front with Fyr-Actor was counted as legacy")
	}
}

// On the pipe the header is required: missing or malformed is 401, never a
// default; a good one reaches the handler as the principal, with the host's
// own install as the front.
func TestPipeHandlerNeedsFyrActor(t *testing.T) {
	s := &Server{ID: "SELF-0000-0000-0000"}
	h := s.PipeHandler()
	for name, hdr := range map[string]map[string]string{
		"missing":   nil,
		"malformed": {"Fyr-Actor": "kind=device"},
		"an agent":  {"Fyr-Actor": "v=1; kind=agent; id=x; how=local"},
	} {
		if w := serveReq(h, "GET", "/api/profiles", hdr); w.Code != 401 {
			t.Errorf("%s: %d (want 401)", name, w.Code)
		}
	}
	if w := serveReq(h, "GET", "/api/profiles", map[string]string{"Fyr-Actor": goodActor}); w.Code != 200 {
		t.Errorf("a good one: %d %s", w.Code, w.Body)
	}
	var got authn.Principal
	probe := withActor(actorPipe, "SELF-0000-0000-0000", nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got, _ = authn.From(r.Context()) }))
	serveReq(probe, "GET", "/x", map[string]string{"Fyr-Actor": goodActor, "Fyr-Origin": "https://desk.tail.ts.net:7443"})
	if got.Kind != authn.KindDevice || got.ID != "dev_x" || got.Front != "SELF-0000-0000-0000" {
		t.Errorf("principal %+v", got)
	}
}

// Fyr-Origin is the browser's public origin, read on the pipe only for a
// preview's CSP; a malformed one is ignored.
func TestPipeHandlerOrigin(t *testing.T) {
	var origin string
	probe := withActor(actorPipe, "", nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { origin, _ = authn.OriginFrom(r.Context()) }))
	for in, want := range map[string]string{"https://desk.tail.ts.net:7443": "https://desk.tail.ts.net:7443", "http://127.0.0.1:7777": "http://127.0.0.1:7777",
		"https://x.example/ evil": "", "javascript:alert(1)": "", "https://a b": ""} {
		origin = ""
		serveReq(probe, "GET", "/x", map[string]string{"Fyr-Actor": goodActor, "Fyr-Origin": in})
		if origin != want {
			t.Errorf("Fyr-Origin %q -> %q (want %q)", in, origin, want)
		}
	}
}

type fakeGate struct {
	member  bool
	revoked map[string]bool
	noDrive bool
	enforce bool
}

func (g fakeGate) Member(*http.Request) (string, bool)     { return "FRNT-0000-0000-0000", g.member }
func (g fakeGate) DeviceRevoked(id string) bool            { return g.revoked[id] }
func (g fakeGate) MayDrive(string) bool                    { return !g.noDrive }
func (g fakeGate) DeviceKnown(id string) bool              { return !g.enforce || id == "dev_x" }
func (g fakeGate) Enforcing() bool                         { return g.enforce }
func (g fakeGate) Track(string, context.CancelFunc) func() { return func() {} }
func (g fakeGate) Served()                                 {}

// SPEC V0.158 (decision 0030 3.3, 3.7): the host's peer-port handler. A key that
// is not in the roster is 403; the principal comes from Fyr-Actor and is
// required; a device the roster says was revoked is 401 whichever front forwards
// it; a call that changes things from an install switched off here is 403; the
// front is the key the link proved; no /mcp.
func TestLinkHandler(t *testing.T) {
	s := &Server{MCP: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("mcp")) })}
	good := map[string]string{"Fyr-Actor": goodActor}
	for name, c := range map[string]struct {
		g      fakeGate
		method string
		path   string
		hdr    map[string]string
		want   int
	}{
		"a member":                      {fakeGate{member: true}, "GET", "/api/profiles", good, 200},
		"not in the roster":             {fakeGate{}, "GET", "/api/profiles", good, 403},
		"no Fyr-Actor":                  {fakeGate{member: true}, "GET", "/api/profiles", nil, 401},
		"a revoked device":              {fakeGate{member: true, revoked: map[string]bool{"dev_x": true}}, "GET", "/api/profiles", good, 401},
		"an install reads when off":     {fakeGate{member: true, noDrive: true}, "GET", "/api/profiles", good, 200},
		"an install drives when off":    {fakeGate{member: true, noDrive: true}, "POST", "/api/command", good, 403},
		"an install drives when on":     {fakeGate{member: true}, "POST", "/api/command", good, 400},
		"/mcp is not served":            {fakeGate{member: true}, "GET", "/mcp", good, 404},
		"a revoked device is not asked": {fakeGate{member: true, revoked: map[string]bool{"other": true}}, "GET", "/api/profiles", good, 200},
	} {
		if w := serveReq(s.LinkHandler(c.g), c.method, c.path, c.hdr); w.Code != c.want {
			t.Errorf("%s: %d (want %d) %s", name, w.Code, c.want, w.Body)
		}
	}
	// over a link: kind=local is refused, and kind=install must carry the proven id
	linkReq := func(actor string) int {
		probe := withActor(actorLink, "", nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			probe.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), linkKey{}, linkInfo{"THEM-0000-0000-0000", fakeGate{}})))
		})
		return serveReq(h, "GET", "/x", map[string]string{"Fyr-Actor": actor}).Code
	}
	for actor, want := range map[string]int{
		"v=1; kind=local; id=owner; how=local":                               401,
		"v=1; kind=install; id=LIAR-0000-0000-0000; how=link":                401,
		"v=1; kind=install; id=THEM-0000-0000-0000; how=link":                200,
		"v=1; kind=device; id=dev_x; how=tailnet; front=LIAR-0000-0000-0000": 200,
	} {
		if got := linkReq(actor); got != want {
			t.Errorf("%s over a link: %d (want %d)", actor, got, want)
		}
	}
	var got authn.Principal
	probe := withActor(actorLink, "", nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got, _ = authn.From(r.Context()) }))
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probe.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), linkKey{}, linkInfo{"THEM-0000-0000-0000", fakeGate{}})))
	})
	serveReq(h, "GET", "/x", map[string]string{"Fyr-Actor": "v=1; kind=device; id=dev_x; how=tailnet; front=LIAR-0000-0000-0000"})
	if got.Front != "THEM-0000-0000-0000" {
		t.Errorf("the front is %q: it is the key the link proved, not what the header says", got.Front)
	}
}
