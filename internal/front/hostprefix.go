package front

// Decision 0031 (the client names the host), slice 1: the front speaks both.
//
//	/api/h/<hostId>/<the host protocol's path after /api/>   one host's call
//	/api/front/…                                              what belongs to no host
//
// The old routes stay as they were, marked Deprecation: true and counted in
// oldRouteUses, until the release after the client stops using them.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/authn"
	"github.com/meros/fyr/internal/core"
)

const (
	hostPrefix  = "/api/h/"
	frontPrefix = "/api/front/"
)

// both registers a front route under its old path and under /api/front
// (decision 0031 2.3). path is the old path without the "/api" start.
func both(mux muxer, method, path string, h func(http.ResponseWriter, *http.Request)) {
	mux.HandleFunc(method+" /api"+path, h)
	mux.HandleFunc(method+" "+frontPrefix[:len(frontPrefix)-1]+path, h)
}

// isOldRoute: an /api/ path that is neither a host's (/api/h/) nor the
// front's (/api/front/).
func isOldRoute(p string) bool {
	return strings.HasPrefix(p, "/api/") && !strings.HasPrefix(p, hostPrefix) && !strings.HasPrefix(p, frontPrefix)
}

// withDeprecation marks and counts a call to an old route (decision 0031 2.8).
func (f *Front) withDeprecation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isOldRoute(r.URL.Path) {
			f.oldRouteUses.Add(1)
			w.Header().Set("Deprecation", "true")
		}
		next.ServeHTTP(w, r)
	})
}

// hostPreviewPath: a call for a preview's page or file under a host's prefix.
// A sandboxed page has no cookie, and the 80-bit id in its URL is its key
// (V0.19), as for /api/previews/.
func hostPreviewPath(p string) bool {
	rest, ok := strings.CutPrefix(p, hostPrefix)
	if !ok {
		return false
	}
	_, after, ok := strings.Cut(rest, "/")
	return ok && strings.HasPrefix(after, "previews/")
}

// idOfLocked is the install id a host is addressed by: the one it reports,
// else the one pinned when it was first seen; the caller holds f.mu.
func (f *Front) idOfLocked(l *link) string {
	if id := l.reportedID(); id != "" {
		return id
	}
	return f.pins[l.name()]
}

// byID is the host with that install id; nil for an id the front does not
// follow (never another host, decision 0031 2.2).
func (f *Front) byID(id string) *link {
	if id == "" {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.all() {
		if f.idOfLocked(l) == id {
			return l
		}
	}
	return nil
}

// linkOf is hosts[].link: how the front reaches the host and in what state
// (decision 0030 5b gives the same shape); the caller holds f.mu.
func (f *Front) linkOf(l *link) *api.Link {
	k := &api.Link{State: "down", Via: "whois", Opened: "here"}
	switch {
	case l.cfg.Dial != nil:
		k.Via = "pipe"
	case l.lt != nil && l.lt.hasPeer():
		k.Via = "peer"
	}
	if l.online {
		k.State = "up"
	}
	if !l.since.IsZero() {
		k.Since = l.since.UTC().Format(time.RFC3339)
	}
	if !l.online && l.err != "" {
		kind := "no-route"
		switch e := strings.ToLower(l.err); {
		case strings.Contains(e, "timeout"), strings.Contains(e, "deadline"):
			kind = "timeout"
		case strings.Contains(e, "refused"):
			kind = "refused"
		}
		if stalledErr(l.err) { // V0.181: the host is up but stuck
			kind = "stalled"
		}
		k.Error = &api.LinkError{Kind: kind, Text: l.err}
	}
	return k
}

// transport is how a call reaches one host: the link manager's one function per
// install (decision 0030 Amendment 1). Until that is on main it is the link's
// own transport; the reason is "" when there is one.
func (f *Front) transport(l *link) (http.RoundTripper, string) { return l.lt, "" }

// forwardTo sends a browser's request to a host as its principal. It is the
// only place the prefix handler reaches a host, so the link manager's
// transport replaces the proxy here and nowhere else. The body is never read.
func (f *Front) forwardTo(l *link, w http.ResponseWriter, r *http.Request) {
	if _, reason := f.transport(l); reason != "" {
		f.hostOffline(w, l)
		return
	}
	l.proxy.ServeHTTP(w, r.WithContext(authn.With(r.Context(), principalOf(r))))
}

// noHostID: the id names no host this front follows.
func noHostID(w http.ResponseWriter, id string) {
	api.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "this front follows no host " + id, "code": "no-host"})
}

// hostOffline answers a call that needs a host the front cannot reach.
func (f *Front) hostOffline(w http.ResponseWriter, l *link) {
	f.mu.Lock()
	name, since, link := l.name(), l.since, f.linkOf(l)
	f.mu.Unlock()
	msg := name + " is offline"
	if !since.IsZero() {
		msg += " since " + since.Local().Format("15:04")
	}
	api.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": msg + "; its threads are read-only", "code": "host-offline", "link": link})
}

// stripHostPrefix is r as the host protocol sees it: /api/h/<id>/x is /api/x.
func stripHostPrefix(r *http.Request) *http.Request {
	after := strings.TrimPrefix(r.URL.EscapedPath(), hostPrefix)
	_, rest, _ := strings.Cut(after, "/")
	u := *r.URL
	u.RawPath = "/api/" + rest
	if p, err := url.PathUnescape(u.RawPath); err == nil {
		u.Path = p
	} else {
		u.Path = u.RawPath
	}
	r2 := r.Clone(r.Context())
	r2.URL = &u
	return r2
}

// hostRoute is the one handler of the prefix: it finds the host by its install
// id, strips the prefix and forwards the call. A host's state, a thread and the
// six settings reads come from the front's cache while the host is offline.
func (f *Front) hostRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	l := f.byID(id)
	if l == nil {
		noHostID(w, id)
		return
	}
	r2 := stripHostPrefix(r)
	path := r2.URL.Path
	get := r.Method == http.MethodGet
	if get && slices.Contains(settingsPaths, path) {
		f.settingsFor(w, r2, l, path, func() { f.hostOffline(w, l) })
		return
	}
	f.mu.Lock()
	on := l.online
	f.mu.Unlock()
	if !on {
		switch {
		case get && path == "/api/state":
			f.cachedState(w, r2, l)
		case get && strings.HasPrefix(path, "/api/threads/") && !strings.Contains(path[len("/api/threads/"):], "/"):
			f.cachedThread(w, l, strings.TrimPrefix(path, "/api/threads/"))
		default:
			f.hostOffline(w, l)
		}
		return
	}
	f.forwardTo(l, w, r2)
}

// readAt stamps an answer that came from the cache with when the host was last
// seen; the caller holds f.mu.
func readAt(w http.ResponseWriter, l *link) {
	at := l.since
	if at.IsZero() {
		at = time.Now()
	}
	w.Header().Set("X-Fyr-Read-At", at.UTC().Format(time.RFC3339))
}

// cachedState is a host's state in the host's own shape from the front's
// cache: its threads with its own revs, its projects, profiles and actions.
func (f *Front) cachedState(w http.ResponseWriter, r *http.Request, l *link) {
	since := int64(0)
	if v := r.URL.Query().Get("since"); v != "" {
		since, _ = strconv.ParseInt(v, 10, 64)
	}
	f.mu.Lock()
	st := map[string]any{"now": time.Now().UTC(), "partial": since > 0}
	for k, v := range l.env {
		st[k] = v
	}
	threads := []any{}
	for _, d := range l.raw {
		if since <= 0 || revOf(d) > since {
			threads = append(threads, d)
		}
	}
	projects := []any{}
	for _, p := range l.projects {
		c := make(map[string]any, len(p))
		for k, v := range p {
			if k != "host" { // the front's stamp, not the host's
				c[k] = v
			}
		}
		projects = append(projects, c)
	}
	st["threads"], st["projects"] = threads, projects
	readAt(w, l)
	b, err := json.Marshal(st)
	f.mu.Unlock()
	if err != nil {
		api.WriteJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	api.WriteJSON(w, 200, json.RawMessage(b))
}

func (f *Front) cachedThread(w http.ResponseWriter, l *link, id string) {
	f.mu.Lock()
	d := l.raw[id]
	var b []byte
	if d != nil {
		readAt(w, l)
		b, _ = json.Marshal(d)
	}
	f.mu.Unlock()
	if b == nil {
		api.WriteJSON(w, 404, map[string]any{"error": "no such thread"})
		return
	}
	api.WriteJSON(w, 200, json.RawMessage(b))
}

// publishFrameLocked sends one frame of a host to /api/front/stream with the
// host's id on it (decision 0031 2.4); the caller holds f.mu.
func (f *Front) publishFrameLocked(l *link, ev string, data []byte) {
	m, err := decode(data)
	if err != nil || m == nil {
		f.hub2.Publish(ev, data)
		return
	}
	m["host"] = f.idOfLocked(l)
	if b, err := json.Marshal(m); err == nil {
		f.hub2.Publish(ev, b)
	}
}

// publishFront sends one of the front's own frames (hosts, projectOrder,
// hostMarks) on both streams.
func (f *Front) publishFront(ev string, data []byte) {
	f.hub.Publish(ev, data)
	f.hub2.Publish(ev, data)
}

func (f *Front) publishFrame(l *link, ev string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.publishFrameLocked(l, ev, data)
}

// frontState is /api/front/state: the front's own things and the list of
// hosts, in the owner's order. A host's records are its own (/api/h/<id>/state).
func (f *Front) frontState(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	hosts := []api.Host{}
	for _, l := range f.orderedLocked() {
		hosts = append(hosts, f.hostLocked(l))
	}
	order := f.order
	if order == nil {
		order = []string{}
	}
	out := map[string]any{"now": time.Now().UTC(), "user": core.Owner, "buildId": f.client.Load().build,
		"front": map[string]any{"id": f.opt.ID, "name": f.opt.Name}, "hosts": hosts, "projectOrder": order}
	if ms := f.shownMarksLocked(); len(ms) > 0 { // V0.171: by host install id
		out["hostMarks"] = ms
	}
	b, err := json.Marshal(out)
	f.mu.Unlock()
	if err != nil {
		api.WriteJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	api.WriteJSON(w, 200, json.RawMessage(b))
}

// frontCommandRoute is POST /api/front/command: the front's own commands, by
// host id. Anything else is a host's and goes to /api/h/<id>/command.
func (f *Front) frontCommandRoute(w http.ResponseWriter, r *http.Request) {
	var c struct {
		CommandID string         `json:"commandId"`
		Action    string         `json:"action"`
		Args      map[string]any `json:"args"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&c); err != nil {
		api.WriteJSON(w, 400, map[string]any{"ok": false, "error": "bad JSON: " + err.Error()})
		return
	}
	nameOf := func(id string) (string, bool) {
		l := f.byID(id)
		if l == nil {
			noHostID(w, id)
			return "", false
		}
		return l.name(), true
	}
	switch c.Action {
	case KindProjectOrder:
		f.orderCommand(w, c.CommandID, c.Args)
	case KindHostMark: // V0.171: keyed by install id already
		f.hostMarkCommand(w, c.CommandID, c.Args)
	case KindProjectLink: // decision 0034: by ref
		f.projectLinkCommand(w, c.CommandID, c.Args)
	case KindProjectUnlink:
		f.projectUnlinkCommand(w, c.CommandID, c.Args)
	case KindProjectShared:
		f.projectSharedCommand(w, c.CommandID, c.Args)
	case KindHostOrder:
		ids, ok := c.Args["ids"].([]any)
		if !ok {
			api.WriteJSON(w, 400, api.Reply{Error: "ids must be a list of host ids", Status: 400})
			return
		}
		names := make([]any, 0, len(ids))
		for _, x := range ids {
			id, _ := x.(string)
			n, ok := nameOf(id)
			if !ok {
				return
			}
			names = append(names, n)
		}
		f.hostOrderCommand(w, c.CommandID, map[string]any{"names": names})
	case KindHostID:
		id, _ := c.Args["host"].(string)
		n, ok := nameOf(id)
		if !ok {
			return
		}
		f.hostIDCommand(w, c.CommandID, map[string]any{"host": n})
	default:
		api.WriteJSON(w, 400, map[string]any{"ok": false, "error": c.Action + " is not a front command; a host's commands go to /api/h/<id>/command"})
	}
}

// presenceRoute2 is POST /api/front/presence {host, threadId, device}: the
// device shows that thread of that host, and the host hears the focus.
func (f *Front) presenceRoute2(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Host     string `json:"host"`
		ThreadID string `json:"threadId"`
		Device   string `json:"device"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&b); err != nil || b.Device == "" {
		http.Error(w, "host, threadId and device are required", 400)
		return
	}
	var l *link
	if b.ThreadID != "" {
		if l = f.byID(b.Host); l == nil {
			noHostID(w, b.Host)
			return
		}
	}
	prev := f.presence.Beat(b.Device, b.ThreadID, time.Now())
	f.mu.Lock()
	prevHost := f.shown[b.Device]
	if f.shown == nil {
		f.shown = map[string]*link{}
	}
	if l != nil {
		f.shown[b.Device] = l
	} else {
		delete(f.shown, b.Device)
	}
	f.mu.Unlock()
	var calls []focusCall
	if prev != "" && prevHost != nil && (prev != b.ThreadID || prevHost != l) {
		calls = append(calls, focusCall{prevHost, prev, true})
	}
	if l != nil {
		calls = append(calls, focusCall{l, b.ThreadID, false})
	}
	f.sendFocus(b.Device, calls)
	w.WriteHeader(http.StatusNoContent)
}
