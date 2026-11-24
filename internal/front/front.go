// Package front is the part of fyrd that serves the client (decision 0016,
// SPEC 0.5 V0.51). A front is configured with a list of hosts only. It talks
// to each over the host protocol (the v0 HTTP API without the web client:
// state, stream, commands, thread calls), merges their state, sends each
// command to the host that owns the record, and keeps the last state of a
// host that went offline, marked read-only. It never touches an engine or a
// store, also when its host runs in the same process.
package front

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"github.com/meros/fyr/internal/httpguard"
	"github.com/meros/fyr/internal/kvstore"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/authn"
	"github.com/meros/fyr/internal/notify"
	"github.com/meros/fyr/internal/peer"
	"github.com/meros/fyr/internal/stream"
)

// HostConfig is one host in a front's list.
type HostConfig struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Remote: the host runs on another machine. The browser's identity
	// headers are not passed on; the host checks the front's tailnet identity.
	Remote bool `json:"-"`
	// Found: it is not in the config; the roster brought it (UsePeerLink).
	// V0.123's tailnet discovery set it too, until V0.retire-discovery.
	Found bool `json:"-"`
	// Dial, when set, connects to the host in-process (V0.129 (3)): the
	// both role's own host, reached at LocalURL with no port open.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error) `json:"-"`
	// Peer is the host's peer link (decision 0030 3.6, V0.158), preferred over
	// URL, the V0.51 whois link, which stays as the fallback. A host reached
	// only by a peer link has the link's address as its URL.
	Peer *PeerEndpoint `json:"-"`
}

// LocalURL is the URL of a host the front dials in-process (V0.129 (3)). It
// names loopback, so the host's guard and the route ("this machine") read it
// as local.
const LocalURL = "http://localhost"

type Options struct {
	// LegacyFronts is this install's host's count of fronts served without a
	// Fyr-Actor (decision 0030 slice 1), for `fyr auth status`; nil: 0 (a
	// front only).
	LegacyFronts func() int64
	Name         string // this machine, shown in the rail
	// ForwardPorts are the ports this front forwards a thread's servers on
	// (V0.forwarded-ports); none: no forwarding.
	ForwardPorts []int
	BuildID      string // the client's build (V0.13 reload)
	OwnerLogin   string // the browser guard's tailnet owner (V0.14)
	// OwnerFn is the owner's login when OwnerLogin is empty: a tagged install has
	// none of its own and takes the one its roster carries (decision 0030 3.4),
	// which it learns by sync once it is paired.
	OwnerFn func() string
	// Peer is this install's peer side: the roster, the invite, the candidates
	// (Settings → Hosts); nil keeps none.
	Peer *peer.Env
	// ConfigNote is "" while the config file is what fyrd read, else what to do
	// (V0.180): Settings → Hosts shows it.
	ConfigNote func() string
	// Auth keeps the browser sessions (V0.130); nil keeps none.
	Auth *auth.Store
	Web  fs.FS // the built client
	// MinBackoff is the first wait before reconnecting to a host; default 500 ms.
	MinBackoff time.Duration
	// Client talks to the hosts; default a client without a timeout (the
	// stream is long-lived).
	Client *http.Client
	Logf   func(format string, args ...any)
	// Push sends this front's notifications to its devices (V0.54); nil
	// sends none.
	Push Pusher
	// NotifyDelay is how long done and blocked wait before they push;
	// default notify.Delay.
	NotifyDelay time.Duration
	// CacheDir keeps the last state per host across a restart (V0.54);
	// empty keeps it in memory only.
	CacheDir string
	// Store keeps the owner's settings that span the hosts (V0.121: the
	// project order); nil keeps none.
	Store kvstore.Table
	// WellKnown is this install's /.well-known/fyr answer (V0.123, decision
	// 0025), which the pairing reads (condition 1); nil serves none.
	WellKnown func() peer.WellKnown
	// Tailnet asks tailscaled which node a Serve request came from (decision
	// 0030 3.4); nil enrols a tailnet browser by its login alone, as V0.130 did.
	Tailnet NodeAuth
	// OwnerRoutes adds calls to the owner socket (the roster's: fyr host …).
	OwnerRoutes func(mux *http.ServeMux)
	// OnWhoisFallback hears each request that went over a whois link although the
	// host has a peer link: it counts against the peer way's enforce.
	OnWhoisFallback func()
	// AuthTick is how often rule 3 looks at the modes; default 10 minutes.
	AuthTick time.Duration
	// ID is this install's key id (V0.124, decision 0026), in the merged
	// state's hostId.
	ID string
}

type Front struct {
	lanOrigin atomic.Pointer[string] // see SetLANOrigin
	fwd       forwards               // V0.forwarded-ports
	opt       Options
	streams   streams
	// authTicks counts rule 3's looks at the modes (a test waits for some).
	authTicks atomic.Int64
	subMu     sync.Mutex
	// links: the hosts followed, copy on write (a found host joins while the
	// front runs, V0.123); read with all().
	links atomic.Pointer[[]*link]
	// run: the context and wait group of Run, so a host added later is
	// followed as long as the ones from the start.
	runMu  sync.Mutex
	runCtx context.Context
	runWG  *sync.WaitGroup
	// hub is /api/front/stream: each frame of a host carries host: <id> (decision 0031 2.4).
	hub *stream.Hub
	// shown: the host of the thread each device last said it shows (the focus's off).
	shown map[string]*link
	// client is the web client served and its build (V0.13); a client-only
	// deploy swaps it (V0.81).
	client atomic.Pointer[clientBuild]

	mu       sync.Mutex // guards the caches of every link
	presence *notify.Presence
	// held: the pushes held for quiet hours and the away batch
	// (V0.push-summary-batch).
	held *notify.Queue
	// noticed: per host and thread, the number of its last change of
	// attention state, so a waiting push knows it is still the newest.
	noticed map[string]uint64
	// order is the owner's project order (V0.121); receipts the replies of
	// the front's own commands by commandId.
	order    []string
	receipts map[string]api.Reply
	// pins: the key id the front first saw for each host, by name (V0.124,
	// decision 0026), stored under front.hostIds.
	pins map[string]string
	// hosts: the owner's host order as this front last set or kept it
	// (V0.132); the newest of it and the hosts' copies wins.
	hosts hostOrder
	// marks are the owner's host marks as this front last set them (V0.171).
	marks hostMarks
	// linkSync: shared values of linked projects already sent (decision 0034).
	linkSync map[string]bool
}

// link is a front's connection to one host and its cache of that host's
// records.
type link struct {
	cfg    HostConfig
	base   *url.URL
	client *http.Client
	proxy  *httputil.ReverseProxy
	// lt routes this link's requests: the peer link when there is one, else the
	// transport the link was made with.
	lt *linkTransport
	// cancel ends following the host; gone is set when it was dropped.
	cancel context.CancelFunc
	gone   bool
	// ctx is the following's context, under f.mu; probing is set while the
	// front asks a stalled host for its state (V0.181).
	ctx     context.Context
	probing atomic.Bool

	raw      map[string]map[string]any // the host's thread DTOs, host revs
	projects []map[string]any
	env      map[string]any // the rest of the host's state: actions, profiles, deploy, user, host
	// reported is the name the host reports (env["host"]), kept apart so
	// name() needs no lock: the proxy's error handler calls it (#40).
	reported atomic.Pointer[string]
	online   bool
	since    time.Time
	err      string
	dirty    bool // changed since the cache was written
	// savedSum and savedAt are the last cache write: its content without
	// savedAt, and when (#22: a host's file is written only on a change, at
	// most every saveGap).
	savedSum [sha256.Size]byte
	savedAt  time.Time
	// idWas: the pinned id, when the host now reports another (V0.124).
	idWas string
	// settings: the last answer of each settings read (V0.127), served
	// while the host is offline.
	settings map[string]settingsRead
}

func New(opt Options, hosts []HostConfig) *Front {
	if opt.MinBackoff <= 0 {
		opt.MinBackoff = 500 * time.Millisecond
	}
	if opt.Client == nil {
		// No Timeout on purpose: this client also carries each host's long-lived
		// event stream, which a whole-request timeout would cut; every call that
		// is not the stream has a context timeout of its own.
		opt.Client = &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}
	}
	if opt.Logf == nil {
		opt.Logf = log.Printf
	}
	if opt.NotifyDelay <= 0 {
		opt.NotifyDelay = notify.Delay
	}
	f := &Front{opt: opt, hub: stream.NewHub(), presence: &notify.Presence{}, held: &notify.Queue{KV: opt.Store}}
	f.presence.Seen(time.Now()) // a restart is not an owner long gone
	if err := f.held.Load(time.Now()); err != nil {
		opt.Logf("front: push: %v", err)
	}
	c := &clientBuild{build: opt.BuildID, web: opt.Web, h: http.NotFoundHandler()}
	if opt.Web != nil {
		c.h = webClient(opt.Web)
	}
	f.client.Store(c)
	f.links.Store(&[]*link{})
	f.loadOrder()
	f.loadPins()
	f.loadHostOrder()
	f.loadHostMarks() // V0.171
	for _, h := range hosts {
		if l := f.newLink(h); l != nil {
			f.append(l)
		}
	}
	return f
}

// all is the hosts followed, in order: the configured ones, then the found ones.
func (f *Front) all() []*link { return *f.links.Load() }

func (f *Front) append(l *link) {
	old := f.all()
	n := append(append(make([]*link, 0, len(old)+1), old...), l)
	f.links.Store(&n)
}

// AddHost follows one more host, one the roster brought over its peer link
// (UsePeerLink): it behaves as a configured one. A host whose name or URL is
// already followed is not added again; false then.
func (f *Front) AddHost(h HostConfig) bool {
	f.runMu.Lock()
	defer f.runMu.Unlock()
	u := strings.TrimRight(h.URL, "/")
	f.mu.Lock()
	for _, l := range f.all() {
		rep := l.reported.Load()
		if l.base.String() == u || (h.Name != "" && (l.name() == h.Name || (rep != nil && *rep == h.Name))) {
			f.mu.Unlock()
			return false
		}
	}
	f.mu.Unlock()
	l := f.newLink(h)
	if l == nil {
		return false
	}
	f.mu.Lock()
	f.append(l)
	f.mu.Unlock()
	f.opt.Logf("front: following found host %s at %s", l.name(), u)
	if ctx, wg := f.runCtx, f.runWG; ctx != nil && ctx.Err() == nil {
		wg.Add(1)
		go func() { defer wg.Done(); f.followOne(ctx, l) }()
	}
	return true
}

func (f *Front) newLink(h HostConfig) *link {
	opt := f.opt
	u, err := url.Parse(strings.TrimRight(h.URL, "/"))
	if err != nil || u.Host == "" {
		opt.Logf("front: host %s: bad url %q", h.Name, h.URL)
		return nil
	}
	client := opt.Client
	if h.Dial != nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		if base, ok := opt.Client.Transport.(*http.Transport); ok {
			tr = base.Clone()
		}
		tr.DialContext, tr.Proxy = h.Dial, nil
		client = &http.Client{Transport: tr, Timeout: opt.Client.Timeout}
	}
	// Decision 0030 3.3: a request the front makes for itself (its stream, its
	// state reads, its settings) is the install's own: it carries Fyr-Actor
	// kind=install, and the host's own name as Host, whatever the call set.
	// A call for a browser sets its own Fyr-Actor first and keeps it.
	self := authn.Principal{Kind: authn.KindInstall, ID: opt.ID, How: authn.HowLink}
	if self.ID == "" {
		self.ID = "front"
	}
	lt := &linkTransport{whois: client.Transport, onFallback: opt.OnWhoisFallback}
	if h.Peer == nil || h.URL != peerURL(*h.Peer) {
		lt.whoisBase = u
	}
	if h.Peer != nil {
		lt.attach(*h.Peer)
	}
	selfClient := *client
	selfClient.Transport = actorTransport{rt: lt, self: self}
	l := &link{cfg: h, base: u, lt: lt, client: &selfClient, raw: map[string]map[string]any{},
		settings: map[string]settingsRead{}}
	l.proxy = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(l.base)
			// Decision 0030 3.3: nothing the browser or Tailscale put on the
			// request goes on. The host's own name is the Host (a preview's
			// CSP takes the browser's origin from Fyr-Origin instead), and the
			// principal travels as Fyr-Actor.
			authn.Strip(pr.Out.Header)
			pr.Out.Host = l.base.Host
			p := principalOf(pr.In)
			p.Front = opt.ID
			pr.Out.Header.Set(authn.Header, p.Header())
			if o := originOf(pr.In); o != "" {
				pr.Out.Header.Set(authn.OriginHeader, o)
			}
			// V0.preview-host-prefix: the prefix hostRoute took off, so the
			// host names a preview's folder where the browser reads it.
			if p := authn.PrefixFrom(pr.In.Context()); p != "" {
				pr.Out.Header.Set(authn.PrefixHeader, p)
			}
		},
		Transport:     lt,
		FlushInterval: -1,
		// V0.181: the host's 503 past a deadline means it is stuck: the
		// front checks it and, while it does not answer, marks it offline.
		ModifyResponse: func(resp *http.Response) error {
			if stalledAnswer(resp) {
				f.suspect(l, "a call got no answer in time")
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			api.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": l.name() + ": " + err.Error()})
		},
	}
	if opt.CacheDir != "" {
		f.restore(l)
	}
	return l
}

// Run follows every host until ctx ends, and keeps the cache on disk.
func (f *Front) Run(ctx context.Context) {
	var wg sync.WaitGroup
	f.runMu.Lock()
	f.runCtx, f.runWG = ctx, &wg
	for _, l := range f.all() {
		wg.Add(1)
		go func() { defer wg.Done(); f.followOne(ctx, l) }()
	}
	f.runMu.Unlock()
	wg.Add(1)
	go func() { defer wg.Done(); f.watchAuth(ctx) }()
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(notify.Tick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				f.flushHeld(now) // V0.push-summary-batch
			}
		}
	}()
	if f.opt.CacheDir != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTicker(saveEvery)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					f.saveDirty()
				}
			}
		}()
	}
	<-ctx.Done()
	f.runMu.Lock()
	f.runCtx = nil // a host found now is not followed
	f.runMu.Unlock()
	wg.Wait()
	f.save(time.Now(), true) // the last state, whatever the gap
}

// name is the configured name, else the name the host reports.
func (l *link) name() string {
	if l.cfg.Name != "" {
		return l.cfg.Name
	}
	if s := l.reported.Load(); s != nil && *s != "" {
		return *s
	}
	return l.base.Host
}

// setEnv replaces the host's state; the caller holds f.mu.
func (l *link) setEnv(env map[string]any) {
	l.env = env
	s, _ := env["host"].(string)
	l.reported.Store(&s)
}

// Handler serves the browser API (/api/h/<id>/… and /api/front/…, decision
// 0031) and the client.
func (f *Front) Handler() http.Handler {
	inner := f.browserRules(f.withAuth(f.mux()))
	return refuseActor(f.refuseTailscale(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the owner's login may arrive after the start (a tagged install's, by sync)
		httpguard.Browser(f.ownerLogin(), inner).ServeHTTP(w, r)
	})))
}

// serveStream serves one hub as a browser's stream; a revoke closes it
// (decision 0030 3.4).
func (f *Front) serveStream(hub *stream.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if p, ok := signedIn(r); ok {
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			defer f.streams.add(p.ID, cancel)()
			r = r.WithContext(ctx)
		}
		ch := hub.Subscribe()
		defer hub.Unsubscribe(ch)
		stream.Serve(w, r, ch, f.client.Load().build)
	}
}

// mux is the browser API's routes: a host's calls by its install id in the
// path, the front's own under /api/front/, and nothing else under /api/
// (decision 0031 2.8, V0.old-routes-end): there is no default host.
func (f *Front) mux() *routeMux {
	mux := &routeMux{ServeMux: http.NewServeMux()}
	mux.HandleFunc("GET /api/front/stream", f.serveStream(f.hub))
	mux.HandleFunc("GET /api/front/state", f.frontState)
	mux.HandleFunc("POST /api/front/command", f.frontCommandRoute)
	// V0.54, V0.85: device data lives on the front: presence (the shown
	// thread's host raises its weight) and push.
	mux.HandleFunc("POST /api/front/presence", f.presenceRoute2)
	mux.HandleFunc("POST /api/front/forward", f.forwardGrant) // V0.forwarded-ports
	mux.HandleFunc("GET /forward/open", f.forwardOpen)
	mux.HandleFunc(hostPrefix+"{id}/{rest...}", f.hostRoute) // decision 0031: the one handler of a host's calls
	if f.opt.Push != nil {
		pushRoutes(mux, f.opt.Push, func(r *http.Request, endpoint string) {
			if p, ok := signedIn(r); ok { // a revoke drops what this device's session subscribed
				f.noteSub(p.ID, endpoint)
			}
		})
	}
	f.authAPI(mux) // Settings → Devices (decision 0030 3.4)
	f.peerAPI(mux) // Settings → Hosts: the roster, the invite (3.6)
	mux.HandleFunc("/api/", notAHostRoute)
	mux.HandleFunc("/mcp", http.NotFound) // agents talk to their host
	if f.opt.WellKnown != nil {           // V0.123: who this install is, for the pairing
		mux.Handle("GET /.well-known/fyr", peer.WellKnownHandler(f.opt.WellKnown))
	}
	if f.opt.Web != nil {
		mux.Handle("/", f.webClient())
	}
	return mux
}

// focusCall is one focus message: the thread of a host, on or off.
type focusCall struct {
	l   *link
	id  string
	off bool
}

func (f *Front) sendFocus(device string, calls []focusCall) {
	if len(calls) == 0 {
		return
	}
	go func() {
		for _, c := range calls {
			l := c.l
			if l == nil {
				continue
			}
			f.mu.Lock()
			on := l.online
			f.mu.Unlock()
			if !on {
				continue
			}
			body, _ := json.Marshal(map[string]any{"device": device, "off": c.off})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			req, _ := http.NewRequestWithContext(ctx, "POST", l.base.String()+"/api/threads/"+url.PathEscape(c.id)+"/focus", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Fyr", "1")
			if resp, err := l.client.Do(req); err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
			cancel()
		}
	}()
}
