package front

// Decision 0030 slice 1: who acts on every hop. The browser-facing listeners
// make a principal (withAuth); the front sends it to a host as Fyr-Actor and
// the public origin as Fyr-Origin, and nothing else the browser or Tailscale
// put on the request.

import (
	"net/http"

	"github.com/meros/fyr/internal/authn"
	"github.com/meros/fyr/internal/server"
)

// principalOf is the principal withAuth made for a request. A request that
// did not pass withAuth (a front with no session store) is a legacy caller:
// served the old way, as in observe mode.
func principalOf(r *http.Request) authn.Principal {
	if p, ok := authn.From(r.Context()); ok {
		return p
	}
	return legacy
}

// legacy is who a request with no session is, in observe mode (decision 0030
// 5.1): served, counted, and named so on every hop.
var legacy = authn.Principal{Kind: authn.KindDevice, ID: "legacy", How: authn.HowLegacy}

// originOf is the origin the browser used, for a preview's CSP (V0.47): the
// scheme Serve's socket or TLS says, and the Host the listener checked or
// set (ServeSocket sets the public ts.net name).
func originOf(r *http.Request) string {
	if r.Host == "" {
		return ""
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" || server.IsViaServe(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// setActor puts the principal on a request the front builds itself for a
// browser's call.
func setActor(req *http.Request, p authn.Principal, front, origin string) {
	p.Front = front
	req.Header.Set(authn.Header, p.Header())
	if origin != "" {
		req.Header.Set(authn.OriginHeader, origin)
	}
}

// actorTransport is the front's own requests to a host: the host's own name as
// Host (a remote host's listener refuses any other), and Fyr-Actor as the
// install itself unless the call already named a principal.
type actorTransport struct {
	rt   http.RoundTripper
	self authn.Principal
}

func (t actorTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Host = ""
	if r.Header.Get(authn.Header) == "" {
		r.Header.Set(authn.Header, t.self.Header())
	}
	rt := t.rt
	if rt == nil {
		rt = http.DefaultTransport
	}
	return rt.RoundTrip(r)
}

// refuseActor: a browser-facing listener refuses Fyr-Actor (400). Only a
// channel that proved which install speaks may carry it; here any page or
// process could write it.
func refuseActor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(authn.Header) != "" || r.Header.Get(authn.OriginHeader) != "" {
			refuse(w, r, "this listener does not take "+authn.Header, http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// muxer is what the route helpers register on.
type muxer interface {
	Handle(pattern string, h http.Handler)
	HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request))
}

// routeMux is a ServeMux that remembers its patterns, so a test walks every
// route of the front.
type routeMux struct {
	*http.ServeMux
	patterns []string
}

func (m *routeMux) Handle(p string, h http.Handler) {
	m.patterns = append(m.patterns, p)
	m.ServeMux.Handle(p, h)
}

func (m *routeMux) HandleFunc(p string, h func(http.ResponseWriter, *http.Request)) {
	m.patterns = append(m.patterns, p)
	m.ServeMux.HandleFunc(p, h)
}

// RoutePatterns lists the patterns of the browser API's mux (the test of
// decision 0030 4.2 requests each).
func (f *Front) RoutePatterns() []string { return f.mux().patterns }
