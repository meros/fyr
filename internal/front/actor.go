package front

// Decision 0030 slice 1: who acts on every hop. The browser-facing listeners
// make a principal (withAuth); the front sends it to a host as Fyr-Actor and
// the public origin as Fyr-Origin, and nothing else the browser or Tailscale
// put on the request.

import (
	"context"
	"errors"
	"github.com/meros/fyr/internal/httpguard"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/authn"
	"github.com/meros/fyr/internal/peerlink"
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
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" || httpguard.IsViaServe(r) {
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
	Addr string // host:port of its peer port; "" when only the host can open the link
	Dial *peerlink.Dialer
	// Mesh is the link manager: the two-way link to this host, opened by either side.
	// With it the front sends over the link; Dial stays as the route to a peer of an
	// older slice (fyr-peer/1, one way).
	Mesh *peerlink.Mesh
}

type peerRoute struct {
	rt     http.RoundTripper // the two-way link; nil without a mesh
	legacy http.RoundTripper // one request per connection (fyr-peer/1), for a peer of an older slice
	base   *url.URL
	id     string
	addr   string
	mesh   *peerlink.Mesh
}

// linkTransport is how a front reaches one host. The peer link is preferred
// once a roster member has one; the V0.51 whois link stays as the fallback (a
// host with no peer link yet is followed as before), and its use is counted.
type linkTransport struct {
	whois      http.RoundTripper
	whoisBase  *url.URL // nil: the host has only a peer link
	peer       atomic.Pointer[peerRoute]
	downUntil  atomic.Int64 // unix nanos: the peer link failed to connect; use the whois link until then
	uses       atomic.Int64 // requests sent over the whois link although a peer link is attached
	onFallback func()
}

func (t *linkTransport) attach(ep PeerEndpoint) {
	host := ep.Addr
	if host == "" {
		host = ep.ID // a member that only dials in: the address means nothing, the link is the route
	}
	base, err := url.Parse("https://" + host)
	if err != nil || (ep.Dial == nil && ep.Mesh == nil) {
		return
	}
	r := &peerRoute{base: base, id: ep.ID, addr: ep.Addr, mesh: ep.Mesh}
	if ep.Mesh != nil {
		wait := time.Duration(0)
		if t.whoisBase == nil { // no other route: a request waits a moment for a link
			wait = 5 * time.Second
		}
		r.rt = ep.Mesh.Transport(ep.ID, wait)
	}
	if ep.Dial != nil && ep.Addr != "" {
		r.legacy = ep.Dial.Transport(ep.ID, ep.Addr)
	}
	t.peer.Store(r)
	t.downUntil.Store(0)
}

// peerID is the install the peer link is pinned to, "" with none.
func (t *linkTransport) peerID() string {
	if p := t.peer.Load(); p != nil {
		return p.id
	}
	return ""
}

func (t *linkTransport) hasPeer() bool { return t.peer.Load() != nil }

func (t *linkTransport) peerAddr() string {
	if p := t.peer.Load(); p != nil {
		return p.addr
	}
	return ""
}

func (t *linkTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	p := t.peer.Load()
	if p != nil && time.Now().UnixNano() >= t.downUntil.Load() {
		var lastErr error
		// 1. the two-way link, when it is up
		if p.rt != nil {
			resp, err := p.rt.RoundTrip(reroute(r, p.base))
			if err == nil {
				return resp, nil
			}
			var nl *peerlink.ErrNoLink
			if !errors.As(err, &nl) {
				return nil, err // the link was up: a failure on it is the request's own
			}
			lastErr = err
		}
		// 2. a peer of an older slice: one request per connection
		if p.legacy != nil && (p.mesh == nil || p.mesh.Status(p.id).Err == nil || p.mesh.Status(p.id).Err.Kind == peerlink.ErrOlderVersion) {
			resp, err := p.legacy.RoundTrip(reroute(r, p.base))
			if err == nil {
				return resp, nil
			}
			var de *peerlink.DialError
			if !errors.As(err, &de) {
				return nil, err
			}
			lastErr = err
		}
		if t.whoisBase == nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, errors.New("no route to the host: its peer link is down and it has no other")
		}
		t.downUntil.Store(time.Now().Add(2 * time.Second).UnixNano())
	}
	if t.whoisBase == nil {
		return nil, errors.New("no route to the host: its peer link is down and it has no other")
	}
	if p != nil {
		t.uses.Add(1)
		if t.onFallback != nil {
			t.onFallback() // a use of the old link counts against the peer way's enforce
		}
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
		if l.gone {
			continue
		}
		if (id != "" && (l.reportedID() == id || (l.lt != nil && l.lt.peerID() == id))) || (found == nil && l.name() == name && l.cfg.Dial == nil) {
			found = l
		}
	}
	f.mu.Unlock()
	if found != nil {
		if found.lt != nil && (!found.lt.hasPeer() || found.lt.peerID() != id || found.lt.peerAddr() != ep.Addr) {
			// a new peer link, or the same install on a new key or address (a rotation)
			if was := found.lt.peerID(); was != "" && was != id {
				f.followKey(found.name(), was, id)
			}
			found.lt.attach(ep)
			f.opt.Logf("front: host %s now over the peer link (%s)", found.name(), ep.Addr)
		}
		return
	}
	f.AddHost(HostConfig{Name: name, URL: peerURL(ep), Remote: true, Found: true, Peer: &ep})
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

// followKey moves the pinned id of a host that rotated its key to the new one,
// so Settings → Hosts does not say another install answers under its name
// (V0.124): the roster proved the new key came from the old one.
func (f *Front) followKey(name, oldID, newID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pins[name] == oldID {
		f.pins[name] = newID
		if err := f.savePinsLocked(); err != nil {
			f.opt.Logf("front: host ids: %v", err)
		}
	}
}

// DropPeers stops following the hosts that were reached only over a peer link to
// an install that is no longer a member (it was removed), and takes the peer link
// off a host that also has a whois link, which stays as it was.
func (f *Front) DropPeers(member func(id string) bool) {
	f.mu.Lock()
	var cancels []context.CancelFunc
	keep := make([]*link, 0, len(f.all()))
	for _, l := range f.all() {
		id := ""
		if l.lt != nil {
			id = l.lt.peerID()
		}
		if id == "" || member(id) {
			keep = append(keep, l)
			continue
		}
		if l.lt.whoisBase != nil { // also followed over the old link: it stays, without the peer link
			l.lt.peer.Store(nil)
			keep = append(keep, l)
			continue
		}
		l.gone = true
		if l.cancel != nil {
			cancels = append(cancels, l.cancel)
		}
		f.opt.Logf("front: host %s dropped: its install was removed from the roster", l.name())
	}
	changed := len(keep) != len(f.all())
	if changed {
		f.links.Store(&keep)
		f.publishHostsLocked()
	}
	f.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// AllOnPeerLink says whether every host this front follows (the one in this
// process aside) is reached over a pinned peer link: the condition for the peer
// way in to enforce by itself, which removes the whois link and the loopback host
// port (decision 0030 5.1 rule 3). When it is not, it names a host that is not.
func (f *Front) AllOnPeerLink() (bool, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.all() {
		if l.cfg.Dial != nil || l.gone { // the host in this process, over the pipe
			continue
		}
		if l.lt == nil || !l.lt.hasPeer() {
			return false, "host " + l.name() + " is still followed over the whois link or a configured address: pair it (it needs a peer port)"
		}
		if st := l.lt.linkStatus(); st.State != "up" {
			return false, "host " + l.name() + " has no link up now (" + linkWhy(st) + ")"
		}
	}
	return true, ""
}

// linkStatus is the state of the route this front uses to the host.
func (t *linkTransport) linkStatus() api.Link {
	p := t.peer.Load()
	switch {
	case p == nil && t.whoisBase != nil:
		return api.Link{State: "up", Via: "whois"}
	case p == nil:
		return api.Link{State: "down", Via: "peer"}
	case p.mesh == nil:
		return api.Link{State: "up", Via: "peer"} // a one-way link of an older slice
	}
	st := p.mesh.Status(p.id)
	out := api.Link{State: st.State, Via: "peer", Opened: st.Opened}
	if !st.Since.IsZero() {
		out.Since = st.Since.UTC().Format(time.RFC3339)
	}
	if st.Err != nil {
		out.Error = &api.LinkError{Kind: st.Err.Kind, Text: st.Err.Text}
		if st.Err.Kind == peerlink.ErrOlderVersion && p.legacy != nil { // followed one way over fyr-peer/1
			out.State, out.Opened = "up", "here"
		}
	}
	return out
}

func linkWhy(st api.Link) string {
	if st.Error != nil {
		return st.Error.Kind
	}
	return st.State
}

// peerURL is the URL a host reached only over its peer link carries: its address, or
// its id when only the host can open the link.
func peerURL(ep PeerEndpoint) string {
	if ep.Addr == "" {
		return "https://" + ep.ID
	}
	return "https://" + ep.Addr
}
