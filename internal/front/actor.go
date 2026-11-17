package front

// Decision 0030 slice 1: who acts on every hop. The browser-facing listeners
// make a principal (withAuth); the front sends it to a host as Fyr-Actor and
// the public origin as Fyr-Origin, and nothing else the browser or Tailscale
// put on the request.

import (
	"errors"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/authn"
	"github.com/meros/fyr/internal/peerlink"
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

// PeerEndpoint is a host's peer link (decision 0030 3.6, V0.158): TLS 1.3 pinned
// to the host's install id, on its peer port.
type PeerEndpoint struct {
	ID   string
	Addr string // host:port of its peer port
	Dial *peerlink.Dialer
}

type peerRoute struct {
	rt   http.RoundTripper
	base *url.URL
}

// linkTransport is how a front reaches one host. The peer link is preferred
// once a roster member has one; the V0.51 whois link stays as the fallback (a
// host with no peer link yet is followed as before), and its use is counted.
type linkTransport struct {
	whois     http.RoundTripper
	whoisBase *url.URL // nil: the host has only a peer link
	peer      atomic.Pointer[peerRoute]
	downUntil atomic.Int64 // unix nanos: the peer link failed to connect; use the whois link until then
	uses      atomic.Int64 // requests sent over the whois link although a peer link is attached
}

func (t *linkTransport) attach(ep PeerEndpoint) {
	base, err := url.Parse("https://" + ep.Addr)
	if err != nil || ep.Dial == nil {
		return
	}
	t.peer.Store(&peerRoute{rt: ep.Dial.Transport(ep.ID, ep.Addr), base: base})
	t.downUntil.Store(0)
}

func (t *linkTransport) hasPeer() bool { return t.peer.Load() != nil }

func (t *linkTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	p := t.peer.Load()
	if p != nil && time.Now().UnixNano() >= t.downUntil.Load() {
		resp, err := p.rt.RoundTrip(reroute(r, p.base))
		if err == nil {
			return resp, nil
		}
		var de *peerlink.DialError
		if !errors.As(err, &de) || t.whoisBase == nil {
			return nil, err // a failure after the link was made is the request's own
		}
		t.downUntil.Store(time.Now().Add(30 * time.Second).UnixNano())
	}
	if t.whoisBase == nil {
		return nil, errors.New("no route to the host: its peer link is down and it has no other")
	}
	if p != nil {
		t.uses.Add(1)
	}
	rt := t.whois
	if rt == nil {
		rt = http.DefaultTransport
	}
	return rt.RoundTrip(reroute(r, t.whoisBase))
}

// reroute is r sent to base: the scheme and host change, the path stays.
func reroute(r *http.Request, base *url.URL) *http.Request {
	r2 := r.Clone(r.Context())
	r2.URL.Scheme, r2.URL.Host = base.Scheme, base.Host
	r2.Host = ""
	return r2
}

// UsePeerLink gives the host with that install id (or, failing that, that name)
// a peer link; with none followed yet, it is followed over the peer link alone.
// The whois link stays beside it as the fallback.
func (f *Front) UsePeerLink(id, name string, ep PeerEndpoint) {
	f.mu.Lock()
	var found *link
	for _, l := range f.all() {
		if (id != "" && l.reportedID() == id) || (found == nil && l.name() == name && l.cfg.Dial == nil) {
			found = l
		}
	}
	f.mu.Unlock()
	if found != nil {
		if found.lt != nil && !found.lt.hasPeer() {
			found.lt.attach(ep)
			f.opt.Logf("front: host %s now over the peer link (%s)", found.name(), ep.Addr)
		}
		return
	}
	f.AddHost(HostConfig{Name: name, URL: "https://" + ep.Addr, Remote: true, Found: true, Peer: &ep})
}

// WhoisLinkUses counts the requests that went over a whois link although the
// host has a peer link (decision 0030 slice 3: counted, until the peer way in
// enforces).
func (f *Front) WhoisLinkUses() int64 {
	var n int64
	for _, l := range f.all() {
		if l.lt != nil {
			n += l.lt.uses.Load()
		}
	}
	return n
}
