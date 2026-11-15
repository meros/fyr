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
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/notify"
	"github.com/meros/fyr/internal/server"
)

// HostConfig is one host in a front's list.
type HostConfig struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Remote: the host runs on another machine. The browser's identity
	// headers are not passed on; the host checks the front's tailnet identity.
	Remote bool `json:"-"`
}

type Options struct {
	Name       string // this machine, shown in the rail
	BuildID    string // the client's build (V0.13 reload)
	OwnerLogin string // the browser guard's tailnet owner (V0.14)
	Web        fs.FS  // the built client
	// MinBackoff is the first wait before reconnecting to a host; default 500 ms.
	MinBackoff time.Duration
	// Client talks to the hosts; default a client without a timeout (the
	// stream is long-lived).
	Client *http.Client
	Logf   func(format string, args ...any)
	// Push sends this front's notifications to its devices (V0.54); nil
	// sends none.
	Push server.Pusher
	// NotifyDelay is how long done and blocked wait before they push;
	// default notify.Delay.
	NotifyDelay time.Duration
	// CacheDir keeps the last state per host across a restart (V0.54);
	// empty keeps it in memory only.
	CacheDir string
	// Local is the host in this process (role both): its deploy is this
	// front's own update, shown as the banner (V0.54). Empty: the first host.
	Local string
}

type Front struct {
	opt   Options
	links []*link
	hub   *engine.Hub

	mu       sync.Mutex // guards the caches of every link and objs
	presence *notify.Presence
	// objs: the host of an attachment or preview the front has routed by
	// ?host= or ?thread=, or uploaded (V0.54), so a preview's own files
	// reach the same host.
	objs map[string]*link
}

// link is a front's connection to one host and its cache of that host's
// records.
type link struct {
	cfg    HostConfig
	base   *url.URL
	client *http.Client
	proxy  *httputil.ReverseProxy

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
	f := &Front{opt: opt, hub: engine.NewHub(), presence: &notify.Presence{}, objs: map[string]*link{}}
	for _, h := range hosts {
		u, err := url.Parse(strings.TrimRight(h.URL, "/"))
		if err != nil || u.Host == "" {
			opt.Logf("front: host %s: bad url %q", h.Name, h.URL)
			continue
		}
		l := &link{cfg: h, base: u, client: opt.Client, raw: map[string]map[string]any{},
			pub: map[string]map[string]any{}, known: map[string]bool{}}
		l.proxy = &httputil.ReverseProxy{
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(l.base)
				// Previews build their CSP from the browser's Host (V0.47).
				pr.Out.Host = pr.In.Host
				if l.cfg.Remote {
					pr.Out.Header.Del("Tailscale-User-Login")
				}
			},
			Transport:     opt.Client.Transport,
			FlushInterval: -1,
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": l.name() + ": " + err.Error()})
			},
		}
		f.links = append(f.links, l)
		if opt.CacheDir != "" {
			f.restore(l)
		}
	}
	return f
}

// Run follows every host until ctx ends, and keeps the cache on disk.
func (f *Front) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, l := range f.links {
		wg.Add(1)
		go func() { defer wg.Done(); f.follow(ctx, l) }()
	}
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
	wg.Wait()
	f.saveDirty()
}

// isLocal: this link's deploy is the front's own update.
func (f *Front) isLocal(l *link) bool {
	if f.opt.Local != "" {
		return l.name() == f.opt.Local
	}
	return len(f.links) > 0 && f.links[0] == l
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
	req.Host = "localhost"
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
	req.Host = "localhost"
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
	for _, k := range []string{"actions", "profiles", "deploy", "user", "host"} {
		if v, ok := st[k]; ok {
			env[k] = v
		}
	}
	l.setEnv(env)
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
			m = engine.ApplyMergePatch(c, patch)
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
	default: // event and anything newer: the client ignores what it does not know
		f.hub.Publish(ev, data)
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
}

// Handler serves the browser API (unchanged, plus host stamps) and the client.
func (f *Front) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", f.state)
	mux.HandleFunc("GET /api/stream", func(w http.ResponseWriter, r *http.Request) {
		ch := f.hub.Subscribe()
		defer f.hub.Unsubscribe(ch)
		server.ServeStream(w, r, ch, f.opt.BuildID)
	})
	mux.HandleFunc("GET /api/threads/{id}", f.thread)
	mux.HandleFunc("/api/threads/{id}/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		f.forward(w, r, f.owner(r.PathValue("id")))
	})
	mux.HandleFunc("POST /api/command", f.command)
	// V0.54: device data lives on the front: presence and push.
	server.PresenceRoute(mux, f.presence)
	if f.opt.Push != nil {
		server.PushRoutes(mux, f.opt.Push)
	}
	mux.HandleFunc("POST /api/attachments", f.upload)
	mux.HandleFunc("GET /api/attachments/{id}", f.lookup)
	mux.HandleFunc("GET /api/previews/{id}", f.lookup)
	mux.HandleFunc("GET /api/previews/{id}/{path...}", f.lookup)
	mux.HandleFunc("GET /api/quota", f.quota)
	mux.HandleFunc("GET /api/profiles", f.profiles)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		f.forward(w, r, f.byName(r.URL.Query().Get("host")))
	})
	mux.HandleFunc("/mcp", http.NotFound) // agents talk to their host
	if f.opt.Web != nil {
		mux.Handle("/", server.WebClient(f.opt.Web))
	}
	return server.Browser(f.opt.OwnerLogin, mux)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// byName is the host with that name, else the first host.
func (f *Front) byName(name string) *link {
	if name != "" {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, l := range f.links {
			if l.name() == name {
				return l
			}
		}
	}
	if len(f.links) == 0 {
		return nil
	}
	return f.links[0]
}

// owner is the host that runs a thread (the id-to-host map), else the first.
func (f *Front) owner(id string) *link {
	f.mu.Lock()
	for _, l := range f.links {
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
	for _, l := range f.links {
		for _, p := range l.projects {
			if p["id"] == id {
				return l
			}
		}
	}
	return nil
}

// unavailable answers for a host that cannot take the call now.
func (f *Front) unavailable(w http.ResponseWriter, l *link) bool {
	if l == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "this front has no hosts"})
		return true
	}
	f.mu.Lock()
	on, since, name := l.online, l.since, l.name()
	f.mu.Unlock()
	if on {
		return false
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false,
		"error": fmt.Sprintf("%s is offline since %s; its threads are read-only", name, since.Local().Format("15:04"))})
	return true
}

func (f *Front) forward(w http.ResponseWriter, r *http.Request, l *link) {
	if f.unavailable(w, l) {
		return
	}
	l.proxy.ServeHTTP(w, r)
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
	threads := []map[string]any{}
	projects := []map[string]any{}
	hosts := []map[string]any{}
	profiles := []any{}
	seenProfile := map[any]bool{}
	var actions, deploy any
	for _, l := range f.links {
		for _, d := range l.pub {
			if since <= 0 || revOf(d) > since {
				threads = append(threads, d)
			}
		}
		projects = append(projects, l.projects...)
		h := map[string]any{"name": l.name(), "online": l.online}
		if !l.since.IsZero() {
			h["since"] = l.since.UTC().Format(time.RFC3339)
		}
		if !l.online && l.err != "" {
			h["error"] = l.err
		}
		if ps, ok := l.env["profiles"].([]any); ok {
			h["profiles"] = ps
			for _, p := range ps {
				pm, _ := p.(map[string]any)
				if pm == nil || seenProfile[pm["id"]] {
					continue
				}
				seenProfile[pm["id"]] = true
				profiles = append(profiles, p)
			}
		}
		if d := l.env["deploy"]; d != nil {
			h["deploy"] = d
			if f.isLocal(l) {
				deploy = d
			}
		}
		if actions == nil && l.env["actions"] != nil {
			actions = l.env["actions"]
		}
		hosts = append(hosts, h)
	}
	if actions == nil {
		actions = []any{}
	}
	out := map[string]any{"now": time.Now().UTC(), "user": "owner", "buildId": f.opt.BuildID, "host": f.opt.Name,
		"threads": threads, "projects": projects, "hosts": hosts, "partial": since > 0,
		"actions": actions, "profiles": profiles, "deploy": deploy}
	b, err := json.Marshal(out)
	f.mu.Unlock()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

func (f *Front) thread(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	f.mu.Lock()
	var b []byte
	for _, l := range f.links {
		if d := l.pub[id]; d != nil {
			b, _ = json.Marshal(d)
		}
	}
	f.mu.Unlock()
	if b == nil {
		writeJSON(w, 404, map[string]any{"error": "no such thread"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

// command sends a command to the host that owns its record (V0.51). The
// client's commandId goes along unchanged, so a retry stays exactly once.
func (f *Front) command(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<20))
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "bad body: " + err.Error()})
		return
	}
	var c struct {
		Action   string         `json:"action"`
		ThreadID string         `json:"threadId"`
		Args     map[string]any `json:"args"`
	}
	if err := json.Unmarshal(body, &c); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "bad JSON: " + err.Error()})
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
		l = f.byName(host)
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
	req.Host = "localhost"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Fyr", "1")
	resp, err := l.client.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": l.name() + ": " + err.Error()})
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
	l := f.target(r)
	if l == nil {
		l = f.byName("")
	}
	if f.unavailable(w, l) {
		return
	}
	rec := &recorder{ResponseWriter: w}
	l.proxy.ServeHTTP(rec, r)
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
	f.forward(w, r, l)
}

// quota merges the provider quota of every online host (V0.54): profiles
// are per host (0015); a profile id on two hosts shows the first's. With
// ?host= it is that host's only.
func (f *Front) quota(w http.ResponseWriter, r *http.Request) {
	if l := f.target(r); l != nil {
		f.forward(w, r, l)
		return
	}
	writeJSON(w, 200, f.gather(r.Context(), "/api/quota", "profile"))
}

// profiles asks each online host for its profiles (#42), merged by id with
// the first host's winning, as in the state.
func (f *Front) profiles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, f.gather(r.Context(), "/api/profiles", "id"))
}

// gather GETs a list from each online host and joins them in host order,
// keeping the first item for each value of key.
func (f *Front) gather(ctx context.Context, path, key string) []map[string]any {
	var on []*link
	f.mu.Lock()
	for _, l := range f.links {
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
				parts[i] = q
			}
		}()
	}
	wg.Wait()
	out := []map[string]any{}
	seen := map[any]bool{}
	for _, p := range parts {
		for _, q := range p {
			if !seen[q[key]] {
				seen[q[key]] = true
				out = append(out, q)
			}
		}
	}
	return out
}
