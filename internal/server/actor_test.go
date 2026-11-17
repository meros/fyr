package server

// SPEC V0.156 (decision 0030 slice 1): the host reads Fyr-Actor on the pipe
// and the peer listener only; the peer listener also refuses a browser.

import (
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
	before := LegacyFronts()
	if w := serveReq(h, "GET", "/api/profiles", nil); w.Code != 200 {
		t.Fatalf("a front without Fyr-Actor: %d %s", w.Code, w.Body)
	}
	if LegacyFronts() != before+1 {
		t.Errorf("not counted: %d -> %d", before, LegacyFronts())
	}
	if serveReq(h, "GET", "/api/profiles", map[string]string{"Fyr-Actor": goodActor}); LegacyFronts() != before+1 {
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
	probe := withActor(actorPipe, "SELF-0000-0000-0000", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got, _ = authn.From(r.Context()) }))
	serveReq(probe, "GET", "/x", map[string]string{"Fyr-Actor": goodActor, "Fyr-Origin": "https://desk.tail.ts.net:7443"})
	if got.Kind != authn.KindDevice || got.ID != "dev_x" || got.Front != "SELF-0000-0000-0000" {
		t.Errorf("principal %+v", got)
	}
}

// Fyr-Origin is the browser's public origin, read on the pipe only for a
// preview's CSP; a malformed one is ignored.
func TestPipeHandlerOrigin(t *testing.T) {
	var origin string
	probe := withActor(actorPipe, "", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { origin, _ = authn.OriginFrom(r.Context()) }))
	for in, want := range map[string]string{"https://desk.tail.ts.net:7443": "https://desk.tail.ts.net:7443", "http://127.0.0.1:7777": "http://127.0.0.1:7777",
		"https://x.example/ evil": "", "javascript:alert(1)": "", "https://a b": ""} {
		origin = ""
		serveReq(probe, "GET", "/x", map[string]string{"Fyr-Actor": goodActor, "Fyr-Origin": in})
		if origin != want {
			t.Errorf("Fyr-Origin %q -> %q (want %q)", in, origin, want)
		}
	}
}
