// Package front is the part of fyrd that serves the client (decision 0016,
// SPEC 0.5 V0.51). A front is configured with a list of hosts only. It talks
// to each over the host protocol (the v0 HTTP API without the web client:
// state, stream, commands, thread calls), merges their state, sends each
// command to the host that owns the record, and keeps the last state of a
// host that went offline, marked read-only. It never touches an engine or a
// store, also when its host runs in the same process.
package front

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/meros/fyr/internal/httpguard"
	"io"
	"io/fs"
	"log"
	"maps"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/auth"
	"github.com/meros/fyr/internal/authn"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/notify"
	"github.com/meros/fyr/internal/peer"
	"github.com/meros/fyr/internal/peers"
	"github.com/meros/fyr/internal/stream"
)

// HostConfig is one host in a front's list.
type HostConfig struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Remote: the host runs on another machine. The browser's identity
	// headers are not passed on; the host checks the front's tailnet identity.
	Remote bool `json:"-"`
	// Found: discovery found it (V0.123), it is not in the config.
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
	BuildID      string // the client's build (V0.13 reload)
	OwnerLogin   string // the browser guard's tailnet owner (V0.14)
	// OwnerFn is the owner's login when OwnerLogin is empty: a tagged install has
	// none of its own and takes the one its roster carries (decision 0030 3.4),
	// which it learns by sync once it is paired.
	OwnerFn func() string
	// Peer is this install's peer side: the roster, the invite, the candidates
	// (Settings → Hosts); nil keeps none.
	Peer *peer.Env
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
	// Local is the host in this process (role both): its deploy is this
	// front's own update, shown as the banner (V0.54). Empty: the first host.
	Local string
	// Store keeps the owner's settings that span the hosts (V0.121: the
	// project order); nil keeps none.
	Store KV
	// WellKnown is this install's /.well-known/fyr answer (V0.123, decision
	// 0025), by which other installs find it; nil serves none.
	WellKnown func() peers.WellKnown
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
	opt       Options
	streams   streams
	// authTicks counts rule 3's looks at the modes (a test waits for some).
	authTicks atomic.Int64
	// oldRouteUses counts calls to the routes without a host prefix (decision 0031 2.8).
	oldRouteUses atomic.Int64
	subMu        sync.Mutex
	// links: the hosts followed, copy on write (a found host joins while the
	// front runs, V0.123); read with all().
	links atomic.Pointer[[]*link]
	// run: the context and wait group of Run, so a host added later is
	// followed as long as the ones from the start.
	runMu  sync.Mutex
	runCtx context.Context
	runWG  *sync.WaitGroup
	hub    *stream.Hub
	// hub2 is /api/front/stream: each frame of a host carries host: <id> (decision 0031 2.4).
	hub2 *stream.Hub
	// shown: the host of the thread each device last said it shows (the focus's off).
	shown map[string]*link
	// client is the web client served and its build (V0.13); a client-only
	// deploy swaps it (V0.81).
	client atomic.Pointer[clientBuild]

	mu       sync.Mutex // guards the caches of every link and objs
	presence *notify.Presence
	// objs: the host of an attachment or preview the front has routed by
	// ?host= or ?thread=, or uploaded (V0.54), so a preview's own files
	// reach the same host.
	objs map[string]*link
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

	raw      map[string]map[string]any // the host's thread DTOs, host revs
	pub      map[string]map[string]any // what the browser gets: stamped, front revs
	known    map[string]bool           // thread ids learned from command replies
	projects []map[string]any
	env      map[string]any // the rest of the host's state: actions, profiles, deploy, user, host
	// reported is the name the host reports (env["host"]), kept apart so
	// name() needs no lock: the proxy's error handler calls it (#40).
	reported atomic.Pointer[string]
	online   bool
	since    time.Time
	err      string
	dirty    bool // changed since the cache was written
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
		opt.Client = &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}
	}
	if opt.Logf == nil {
		opt.Logf = log.Printf
	}
	if opt.NotifyDelay <= 0 {
		opt.NotifyDelay = notify.Delay
	}
	f := &Front{opt: opt, hub: stream.NewHub(), hub2: stream.NewHub(), presence: &notify.Presence{}, objs: map[string]*link{}}
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

// AddHost follows one more host, found by discovery (V0.123, decision
// 0025): it behaves as a configured one. A host whose name or URL is
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
		pub: map[string]map[string]any{}, known: map[string]bool{}, settings: map[string]settingsRead{}}
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
		},
		Transport:     lt,
		FlushInterval: -1,
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
	f.saveDirty()
}

// isLocal: this link's deploy is the front's own update.
func (f *Front) isLocal(l *link) bool {
	if f.opt.Local != "" {
		return l.name() == f.opt.Local
	}
	ls := f.all()
	return len(ls) > 0 && ls[0] == l
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

// followOne follows a host until ctx ends or the host is dropped (removed from
// the roster).
func (f *Front) followOne(ctx context.Context, l *link) {
	lctx, cancel := context.WithCancel(ctx)
	f.mu.Lock()
	l.cancel = cancel
	gone := l.gone
	f.mu.Unlock()
	if gone {
		cancel()
		return
	}
	f.follow(lctx, l)
}

func (f *Front) follow(ctx context.Context, l *link) {
	backoff := f.opt.MinBackoff
	for {
		start := time.Now()
		err := f.session(ctx, l)
		if ctx.Err() != nil {
			return
		}
		f.mu.Lock()
		was := l.online
		f.mu.Unlock()
		if was {
			// A stream that ends (the host dropped a slow subscriber, or it
			// restarts) is tried again at once; the mark waits for that try.
			if err = f.session(ctx, l); ctx.Err() != nil {
				return
			}
		}
		f.setOffline(l, err)
		if time.Since(start) > 30*time.Second {
			backoff = f.opt.MinBackoff
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 15*time.Second)
	}
}

// idle is how long a stream may stay silent (the host pings every 25 s)
// before the front counts the host as gone.
const idle = 60 * time.Second

// session subscribes to the host's stream, loads its state and follows it
// until the stream ends (subscribe first, then the snapshot, as the client
// does: nothing is lost between them).
func (f *Front) session(ctx context.Context, l *link) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	watchdog := time.AfterFunc(idle, cancel)
	defer watchdog.Stop()
	req, _ := http.NewRequestWithContext(ctx, "GET", l.base.String()+"/api/stream", nil)
	req.Header.Set("Accept-Encoding", "identity") // each frame at once
	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("stream: %s", resp.Status)
	}
	rd := bufio.NewReader(resp.Body)
	next := func() (string, []byte, error) {
		var ev string
		var data []byte
		for {
			line, err := rd.ReadBytes('\n')
			if err != nil {
				return "", nil, err
			}
			watchdog.Reset(idle)
			line = bytes.TrimRight(line, "\r\n")
			switch {
			case bytes.HasPrefix(line, []byte("event: ")):
				ev = string(line[7:])
			case bytes.HasPrefix(line, []byte("data: ")):
				data = append([]byte(nil), line[6:]...)
			case len(line) == 0 && ev != "":
				return ev, data, nil
			}
		}
	}
	ev, _, err := next()
	if err != nil {
		return err
	}
	if ev != "hello" {
		return fmt.Errorf("stream began with %q, not hello", ev)
	}
	var st map[string]any
	if err := f.get(ctx, l, "/api/state", &st); err != nil {
		return err
	}
	f.load(l, st)
	// The host patches against what it sent on this connection (V0.38).
	conn := map[string]map[string]any{}
	for {
		ev, data, err := next()
		if err != nil {
			return err
		}
		f.apply(ctx, l, conn, ev, data)
	}
}

func (f *Front) get(ctx context.Context, l *link, path string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", l.base.String()+path, nil)
	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: %s", path, resp.Status)
	}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber() // revs are nanosecond counts above 2^53
	return dec.Decode(v)
}

func decode(b []byte) (map[string]any, error) {
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	err := dec.Decode(&m)
	return m, err
}

func revOf(m map[string]any) int64 {
	switch v := m["rev"].(type) {
	case json.Number:
		n, _ := v.Int64()
		return n
	case float64:
		return int64(v)
	case int64:
		return v
	}
	return 0
}

// load takes a host's whole state after (re)connecting.
func (f *Front) load(l *link, st map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	env := map[string]any{}
	for _, k := range []string{"actions", "profiles", "deploy", "stall", "user", "host", "hostId", "hostOrder", "machine", "os", "hostMarks"} {
		if v, ok := st[k]; ok {
			env[k] = v
		}
	}
	l.setEnv(env)
	f.pinLocked(l)
	raw := map[string]map[string]any{}
	ts, _ := st["threads"].([]any)
	for _, x := range ts {
		if d, ok := x.(map[string]any); ok {
			if id, _ := d["id"].(string); id != "" {
				raw[id] = d
			}
		}
	}
	// V0.54: a thread whose attention changed while the front was not
	// following the host (an outage, a front restart) still notifies.
	for id, m := range raw {
		if old := l.raw[id]; old != nil && revOf(m) > revOf(old) {
			f.noticeLocked(l, old, m)
		}
	}
	for id := range l.pub {
		if raw[id] == nil {
			delete(l.pub, id) // gone from the host
		}
	}
	l.raw = raw
	l.dirty = true
	l.projects = nil
	ps, _ := st["projects"].([]any)
	for _, x := range ps {
		if p, ok := x.(map[string]any); ok {
			l.projects = append(l.projects, f.stampProject(l, p))
		}
	}
	if !l.online {
		f.opt.Logf("front: host %s online (%d threads)", l.name(), len(raw))
	}
	l.online, l.since, l.err = true, time.Now(), ""
	for id := range raw {
		f.publishLocked(l, id)
	}
	f.catchUpLocked(l)      // V0.132
	f.catchUpMarksLocked(l) // V0.171
	f.publishHostsLocked()
}

func (f *Front) stampProject(l *link, p map[string]any) map[string]any {
	out := make(map[string]any, len(p)+1)
	for k, v := range p {
		out[k] = v
	}
	out["host"] = l.name()
	return out
}

// apply takes one frame of a host's stream.
func (f *Front) apply(ctx context.Context, l *link, conn map[string]map[string]any, ev string, data []byte) {
	switch ev {
	case "thread":
		m, err := decode(data)
		if err != nil {
			return
		}
		id, _ := m["id"].(string)
		conn[id] = m
		f.take(l, m)
	case "thread.patch":
		p, err := decode(data)
		if err != nil {
			return
		}
		id, _ := p["id"].(string)
		base, _ := p["base"].(json.Number)
		b, _ := base.Int64()
		patch, _ := p["patch"].(map[string]any)
		var m map[string]any
		if c := conn[id]; c != nil && revOf(c) == b && patch != nil {
			m = stream.ApplyMergePatch(c, patch)
		} else if err := f.get(ctx, l, "/api/threads/"+url.PathEscape(id), &m); err != nil {
			return
		}
		conn[id] = m
		f.take(l, m)
	case "project":
		p, err := decode(data)
		if err != nil {
			return
		}
		f.mu.Lock()
		if p["deleted"] == true { // V0.58: the host deleted it
			kept := l.projects[:0]
			for _, x := range l.projects {
				if x["id"] != p["id"] {
					kept = append(kept, x)
				}
			}
			l.projects, l.dirty = kept, true
			f.mu.Unlock()
			f.hub.Publish("project", data)
			return
		}
		sp := f.stampProject(l, p)
		replaced := false
		for i, x := range l.projects {
			if x["id"] == sp["id"] {
				l.projects[i], replaced = sp, true
			}
		}
		if !replaced {
			l.projects = append([]map[string]any{sp}, l.projects...)
		}
		l.dirty = true
		f.mu.Unlock()
		if b, err := json.Marshal(sp); err == nil {
			f.hub.Publish("project", b)
		}
	case "hostMarks": // V0.171: the host keeps newer host marks
		if d, err := decode(data); err == nil {
			f.mu.Lock()
			l.env["hostMarks"] = d
			l.dirty = true
			f.publishMarksLocked()
			f.mu.Unlock()
		}
	case "hostOrder": // V0.132: the host keeps a newer host order
		if d, err := decode(data); err == nil {
			f.mu.Lock()
			l.env["hostOrder"] = d
			l.dirty = true
			f.publishHostsLocked()
			f.mu.Unlock()
		}
	case "deploy":
		// V0.54: a deploy is per host (V0.24's drain runs on the host). Only
		// the local host's is this front's own update and its banner; another
		// host's shows on that host's row (hosts[].deploy).
		if d, err := decode(data); err == nil {
			f.mu.Lock()
			l.env["deploy"] = d
			l.dirty = true
			f.mu.Unlock()
		}
		if f.isLocal(l) {
			f.hub.Publish(ev, data)
		}
	case "stall":
		// V0.147: every host's stall shows, named by its host.
		if d, err := decode(data); err == nil {
			f.mu.Lock()
			l.env["stall"] = d
			l.dirty = true
			name := l.name()
			f.mu.Unlock()
			m := maps.Clone(d)
			m["host"] = name
			if b, err := json.Marshal(m); err == nil {
				f.hub.Publish(ev, b)
			}
		}
	default: // event and anything newer: the client ignores what it does not know
		f.hub.Publish(ev, data)
		f.publishFrame(l, ev, data)
	}
}

// take stores a newer version of a host's thread and publishes it.
func (f *Front) take(l *link, m map[string]any) {
	id, _ := m["id"].(string)
	if id == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	old := l.raw[id]
	if old != nil && revOf(m) < revOf(old) {
		return
	}
	f.noticeLocked(l, old, m)
	l.raw[id] = m
	delete(l.known, id)
	f.publishLocked(l, id)
	if b, err := json.Marshal(m); err == nil { // the host's own thread, whole
		f.publishFrameLocked(l, "thread", b)
	}
}

// publishLocked stamps a thread with its host (and hostOffline while the
// host is unreachable) and sends it to the browsers when it changed. The rev
// is the host's, raised past the last published one when only the stamp
// changed, so the client's keep-the-higher rule and ?since= hold (V0.51).
func (f *Front) publishLocked(l *link, id string) {
	raw := l.raw[id]
	if raw == nil {
		return
	}
	doc := make(map[string]any, len(raw)+2)
	for k, v := range raw {
		doc[k] = v
	}
	doc["host"] = l.name()
	if !l.online {
		doc["hostOffline"] = l.since.UTC().Format(time.RFC3339)
	}
	prev := l.pub[id]
	if prev != nil && sameExceptRev(prev, doc) {
		return
	}
	rev := revOf(raw)
	if prev != nil && revOf(prev)+1 > rev {
		rev = revOf(prev) + 1
	}
	doc["rev"] = json.Number(strconv.FormatInt(rev, 10))
	l.pub[id] = doc
	l.dirty = true
	if b, err := json.Marshal(doc); err == nil {
		f.hub.Publish("thread", b)
	}
}

func sameExceptRev(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		if k == "rev" {
			continue
		}
		bv, ok := b[k]
		if !ok || !reflect.DeepEqual(av, bv) {
			return false
		}
	}
	return true
}

// setOffline marks a host unreachable: its threads stay, marked read-only.
func (f *Front) setOffline(l *link, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if l.online || l.since.IsZero() {
		l.since = time.Now()
		msg := "unreachable"
		if err != nil {
			msg = err.Error()
		}
		f.opt.Logf("front: host %s offline: %s", l.name(), msg)
	}
	l.online = false
	l.dirty = true
	l.err = "unreachable"
	if err != nil && !errors.Is(err, io.EOF) {
		l.err = err.Error()
	}
	for id := range l.raw {
		f.publishLocked(l, id)
	}
	f.publishHostsLocked()
}

// Handler serves the browser API (unchanged, plus host stamps) and the client.
func (f *Front) Handler() http.Handler {
	inner := f.browserRules(f.withAuth(f.withDeprecation(f.mux())))
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

// mux is the browser API's routes.
func (f *Front) mux() *routeMux {
	mux := &routeMux{ServeMux: http.NewServeMux()}
	mux.HandleFunc("GET /api/state", f.state)
	mux.HandleFunc("GET /api/stream", f.serveStream(f.hub))
	mux.HandleFunc("GET /api/front/stream", f.serveStream(f.hub2))
	mux.HandleFunc("GET /api/front/state", f.frontState)
	mux.HandleFunc("POST /api/front/command", f.frontCommandRoute)
	mux.HandleFunc("POST /api/front/presence", f.presenceRoute2)
	mux.HandleFunc(hostPrefix+"{id}/{rest...}", f.hostRoute) // decision 0031: the one handler of a host's calls
	mux.HandleFunc("GET /api/threads/{id}", f.thread)
	mux.HandleFunc("/api/threads/{id}/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		f.forward(principalOf(r), w, r, f.owner(r.PathValue("id")))
	})
	mux.HandleFunc("POST /api/command", f.command)
	// V0.54: device data lives on the front: presence and push.
	presenceRoute(mux, f.presence, f.focus) // V0.85: the shown thread's host raises its weight
	if f.opt.Push != nil {
		pushRoutes(mux, f.opt.Push, func(r *http.Request, endpoint string) {
			if p, ok := signedIn(r); ok { // a revoke drops what this device's session subscribed
				f.noteSub(p.ID, endpoint)
			}
		})
	}
	f.authAPI(mux) // Settings → Devices (decision 0030 3.4)
	f.peerAPI(mux) // Settings → Hosts: the roster, the invite (3.6)
	mux.HandleFunc("POST /api/attachments", f.upload)
	mux.HandleFunc("GET /api/attachments/{id}", f.lookup)
	mux.HandleFunc("GET /api/previews/{id}", f.lookup)
	mux.HandleFunc("GET /api/previews/{id}/{path...}", f.lookup)
	// V0.116: a project's icon lives on the project's host.
	mux.HandleFunc("/api/projects/{id}/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		l := f.projectOwner(r.PathValue("id"))
		if l == nil {
			http.NotFound(w, r)
			return
		}
		f.forward(principalOf(r), w, r, l)
	})
	mux.HandleFunc("GET /api/quota", f.quota)
	mux.HandleFunc("GET /api/load", f.hostLoad)
	mux.HandleFunc("GET /api/profiles", f.profiles)
	for _, p := range settingsPaths { // V0.127: a host's settings, also while it is offline
		mux.HandleFunc("GET "+p, f.settingsRead)
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if f.badHost(w, r) {
			return
		}
		f.forward(principalOf(r), w, r, f.byName(r.URL.Query().Get("host")))
	})
	mux.HandleFunc("/mcp", http.NotFound) // agents talk to their host
	if f.opt.WellKnown != nil {           // V0.123: how other installs find this one
		mux.Handle("GET /.well-known/fyr", peers.Handler(f.opt.WellKnown))
	}
	if f.opt.Web != nil {
		mux.Handle("/", f.webClient())
	}
	return mux
}

// byName is the host with that name; "" is the first host in the owner's
// order (V0.132). A name the
// front does not follow is nil (V0.127): a call meant for one host never
// goes to another.
func (f *Front) byName(name string) *link {
	if name != "" {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, l := range f.all() {
			if l.name() == name {
				return l
			}
		}
		return nil
	}
	f.mu.Lock()
	ls := f.orderedLocked()
	f.mu.Unlock()
	if len(ls) == 0 {
		return nil
	}
	return ls[0]
}

// owner is the host that runs a thread (the id-to-host map), else the first.
func (f *Front) owner(id string) *link {
	f.mu.Lock()
	for _, l := range f.all() {
		if l.raw[id] != nil || l.known[id] {
			f.mu.Unlock()
			return l
		}
	}
	f.mu.Unlock()
	return f.byName("")
}

func (f *Front) projectOwner(id string) *link {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.all() {
		for _, p := range l.projects {
			if p["id"] == id {
				return l
			}
		}
	}
	return nil
}

// noHost refuses a call that names a host this front does not follow (V0.127).
func noHost(w http.ResponseWriter, name string) {
	api.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": fmt.Sprintf("this front follows no host %s", name)})
}

// badHost refuses a call whose ?host= names a host the front does not
// follow; true when it did.
func (f *Front) badHost(w http.ResponseWriter, r *http.Request) bool {
	if h := r.URL.Query().Get("host"); h != "" && f.byName(h) == nil {
		noHost(w, h)
		return true
	}
	return false
}

// unavailable answers for a host that cannot take the call now.
func (f *Front) unavailable(w http.ResponseWriter, l *link) bool {
	if l == nil {
		api.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "this front has no hosts"})
		return true
	}
	f.mu.Lock()
	on, since, name := l.online, l.since, l.name()
	f.mu.Unlock()
	if on {
		return false
	}
	api.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false,
		"error": fmt.Sprintf("%s is offline since %s; its threads are read-only", name, since.Local().Format("15:04"))})
	return true
}

// forward sends the browser's request to a host as p (decision 0030 3.3): there
// is no form without a principal. The proxy takes it from the request's
// context.
func (f *Front) forward(p authn.Principal, w http.ResponseWriter, r *http.Request, l *link) {
	if f.unavailable(w, l) {
		return
	}
	l.proxy.ServeHTTP(w, r.WithContext(authn.With(r.Context(), p)))
}

// state merges the hosts' states. The records are per host and stamped;
// the rest (V0.54): actions are the same everywhere (the first host's);
// profiles are per host (0015), listed per host in hosts[].profiles and
// merged by id at the top (the first host's wins), so a thread on any host
// finds its profile; deploy is per host in hosts[].deploy, and the top-level
// deploy is the local host's (this front's own update).
func (f *Front) state(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	f.mu.Lock()
	st := api.State[map[string]any, map[string]any, any]{Now: time.Now().UTC(), User: core.Owner, BuildID: f.client.Load().build, Host: f.opt.Name, HostID: f.opt.ID,
		Threads: []map[string]any{}, Projects: []map[string]any{}, Profiles: []any{}, Hosts: []api.Host{}, Partial: since > 0}
	seenProfile := map[any]bool{}
	var actions any
	for _, l := range f.orderedLocked() { // V0.132: hosts[] in the owner's order
		for _, d := range l.pub {
			if since <= 0 || revOf(d) > since {
				st.Threads = append(st.Threads, d)
			}
		}
		st.Projects = append(st.Projects, l.projects...)
		h := f.hostLocked(l)
		if ps, ok := l.env["profiles"].([]any); ok {
			for _, p := range ps {
				pm, _ := p.(map[string]any)
				if pm == nil || seenProfile[pm["id"]] {
					continue
				}
				seenProfile[pm["id"]] = true
				st.Profiles = append(st.Profiles, p)
			}
		}
		if d := l.env["deploy"]; d != nil && f.isLocal(l) {
			st.Deploy = d
		}
		if m, ok := l.env["stall"].(map[string]any); ok { // V0.147
			c := maps.Clone(m)
			c["host"] = l.name()
			st.Stalls = append(st.Stalls, c)
		}
		if actions == nil && l.env["actions"] != nil {
			actions = l.env["actions"]
		}
		st.Hosts = append(st.Hosts, h)
	}
	st.Actions = actionsOf(actions)
	st.ProjectOrder = f.order
	if ms := f.newestMarksLocked().Marks; len(ms) > 0 { // V0.171: by host install id
		st.HostMarks = ms
	}
	b, err := json.Marshal(st)
	f.mu.Unlock()
	if err != nil {
		api.WriteJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	api.WriteJSON(w, 200, json.RawMessage(b))
}

// actionsOf reads the action list a host sent (decoded JSON).
func actionsOf(v any) []api.Action {
	out := []api.Action{}
	if b, err := json.Marshal(v); err == nil {
		json.Unmarshal(b, &out)
	}
	if out == nil {
		out = []api.Action{}
	}
	return out
}

func (f *Front) thread(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	f.mu.Lock()
	var b []byte
	for _, l := range f.all() {
		if d := l.pub[id]; d != nil {
			b, _ = json.Marshal(d)
		}
	}
	f.mu.Unlock()
	if b == nil {
		api.WriteJSON(w, 404, map[string]any{"error": "no such thread"})
		return
	}
	api.WriteJSON(w, 200, json.RawMessage(b))
}

// command sends a command to the host that owns its record (V0.51). The
// client's commandId goes along unchanged, so a retry stays exactly once.
func (f *Front) command(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<20))
	if err != nil {
		api.WriteJSON(w, 400, map[string]any{"ok": false, "error": "bad body: " + err.Error()})
		return
	}
	var c struct {
		CommandID string         `json:"commandId"`
		Action    string         `json:"action"`
		ThreadID  string         `json:"threadId"`
		Args      map[string]any `json:"args"`
	}
	if err := json.Unmarshal(body, &c); err != nil {
		api.WriteJSON(w, 400, map[string]any{"ok": false, "error": "bad JSON: " + err.Error()})
		return
	}
	if c.Action == KindProjectOrder { // V0.121: the front's own, no host's
		f.orderCommand(w, c.CommandID, c.Args)
		return
	}
	if c.Action == KindHostMark { // V0.171: the owner's, written to every host
		f.hostMarkCommand(w, c.CommandID, c.Args)
		return
	}
	if c.Action == KindHostOrder { // V0.132: the owner's, written to every host
		f.hostOrderCommand(w, c.CommandID, c.Args)
		return
	}
	if c.Action == KindHostID { // V0.124: the front's own, no host's
		f.hostIDCommand(w, c.CommandID, c.Args)
		return
	}
	var l *link
	host, _ := c.Args["host"].(string)
	pid, _ := c.Args["id"].(string)
	if !strings.HasPrefix(c.Action, "project.") {
		pid, _ = c.Args["projectId"].(string) // thread.create in a project (V0.54)
	}
	switch {
	case c.ThreadID != "":
		l = f.owner(c.ThreadID)
	case host != "":
		if l = f.byName(host); l == nil {
			noHost(w, host)
			return
		}
	case pid != "":
		if l = f.projectOwner(pid); l == nil {
			l = f.byName("")
		}
	default:
		l = f.byName("")
	}
	if f.unavailable(w, l) {
		return
	}
	req, _ := http.NewRequestWithContext(r.Context(), "POST", l.base.String()+"/api/command", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fyr", "1")
	setActor(req, principalOf(r), f.opt.ID, originOf(r))
	resp, err := l.client.Do(req)
	if err != nil {
		api.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": l.name() + ": " + err.Error()})
		return
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	var rep struct {
		OK       bool   `json:"ok"`
		ThreadID string `json:"threadId"`
	}
	if json.Unmarshal(out, &rep) == nil && rep.OK && rep.ThreadID != "" {
		f.mu.Lock()
		if l.raw[rep.ThreadID] == nil {
			l.known[rep.ThreadID] = true // until the stream brings it
		}
		f.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(out)
}

// target is the host a call names: ?thread= (the thread's host) or ?host=;
// nil when it names none.
func (f *Front) target(r *http.Request) *link {
	q := r.URL.Query()
	if id := q.Get("thread"); id != "" {
		return f.owner(id)
	}
	if h := q.Get("host"); h != "" {
		return f.byName(h)
	}
	return nil
}

// remember notes the host of an attachment or preview id.
func (f *Front) remember(id string, l *link) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.objs) > 10000 {
		clear(f.objs)
	}
	f.objs[id] = l
}

// upload stores a file on the host the client names (the thread's, or the
// host a new thread will start on), else the first host (V0.54).
func (f *Front) upload(w http.ResponseWriter, r *http.Request) {
	if f.badHost(w, r) {
		return
	}
	l := f.target(r)
	if l == nil {
		l = f.byName("")
	}
	if f.unavailable(w, l) {
		return
	}
	rec := &recorder{ResponseWriter: w}
	l.proxy.ServeHTTP(rec, r.WithContext(authn.With(r.Context(), principalOf(r))))
	var m struct {
		ID string `json:"id"`
	}
	if rec.status < 300 && json.Unmarshal(rec.body.Bytes(), &m) == nil && m.ID != "" {
		f.remember(m.ID, l)
	}
}

// recorder keeps a copy of a small reply (an upload's id) while passing it on.
type recorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = 200
	}
	if r.body.Len() < 64<<10 {
		r.body.Write(b)
	}
	return r.ResponseWriter.Write(b)
}

// lookup serves an attachment or preview from its host (V0.54): the one the
// client names with ?thread= or ?host=, else the one the front learned for
// that id (an upload, or the preview page whose files these are), else the
// first host. No host is tried in turn.
func (f *Front) lookup(w http.ResponseWriter, r *http.Request) {
	if f.badHost(w, r) {
		return
	}
	id := r.PathValue("id")
	l := f.target(r)
	if l != nil {
		f.remember(id, l)
	} else {
		f.mu.Lock()
		l = f.objs[id]
		f.mu.Unlock()
	}
	if l == nil {
		l = f.byName("")
	}
	f.forward(principalOf(r), w, r, l)
}

// quota merges the provider quota of every online host (V0.54): profiles
// are per host (0015); a profile id on two hosts shows the first's. With
// ?host= it is that host's only.
func (f *Front) quota(w http.ResponseWriter, r *http.Request) {
	if f.badHost(w, r) {
		return
	}
	if l := f.target(r); l != nil {
		f.forward(principalOf(r), w, r, l)
		return
	}
	api.WriteJSON(w, 200, f.gather(r.Context(), "/api/quota", "profile", true))
}

// hostLoad joins the load of every online host (V0.80), one entry per host.
func (f *Front) hostLoad(w http.ResponseWriter, r *http.Request) {
	if f.badHost(w, r) {
		return
	}
	if l := f.target(r); l != nil {
		f.forward(principalOf(r), w, r, l)
		return
	}
	api.WriteJSON(w, 200, f.gather(r.Context(), "/api/load", "host", false))
}

// profiles asks each online host for its profiles (#42), merged by id with
// the first host's winning, as in the state.
func (f *Front) profiles(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, 200, f.gather(r.Context(), "/api/profiles", "id", false))
}

// gather GETs a list from each online host and joins them in host order,
// keeping the first item for each value of key. perHost (V0.127, the quota): each item is stamped with its
// host and kept once per host, so the same provider id on two hosts is two
// items.
func (f *Front) gather(ctx context.Context, path, key string, perHost bool) []map[string]any {
	var on []*link
	f.mu.Lock()
	for _, l := range f.all() {
		if l.online {
			on = append(on, l)
		}
	}
	f.mu.Unlock()
	parts := make([][]map[string]any, len(on))
	var wg sync.WaitGroup
	for i, l := range on {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var q []map[string]any
			if err := f.get(ctx, l, path, &q); err == nil {
				if perHost {
					for _, x := range q {
						x["host"] = l.name()
					}
				}
				parts[i] = q
			}
		}()
	}
	wg.Wait()
	out := []map[string]any{}
	seen := map[any]bool{}
	for _, p := range parts {
		for _, q := range p {
			k := q[key]
			if perHost {
				k = fmt.Sprint(q["host"], "\x00", q[key])
			}
			if !seen[k] {
				seen[k] = true
				out = append(out, q)
			}
		}
	}
	return out
}

// focusCall is one focus message: the thread of a host, on or off.
type focusCall struct {
	l   *link
	id  string
	off bool
}

// focus forwards a presence beat to the hosts it concerns (V0.85): off for
// the thread the device showed before, on for the one it shows now. It runs
// in the background; the beat's answer does not wait for a host. The old
// route finds the host by the thread's id; /api/front/presence names it.
func (f *Front) focus(device, prev, thread string) {
	var calls []focusCall
	if prev != "" && prev != thread {
		calls = append(calls, focusCall{f.owner(prev), prev, true})
	}
	if thread != "" {
		calls = append(calls, focusCall{f.owner(thread), thread, false})
	}
	f.sendFocus(device, calls)
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

// OldRouteUses is how many calls came to a route outside /api/h/ and /api/front/.
func (f *Front) OldRouteUses() int64 { return f.oldRouteUses.Load() }
