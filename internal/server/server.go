// Package server is the v0 HTTP transport (SPEC 0.5 V0.2, decision 0002,
// docs/API-v0.md): JSON commands, a snapshot, an SSE stream, the agents'
// MCP endpoint and the web client.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/gitdiff"
	"github.com/meros/fyr/internal/github"
	"github.com/meros/fyr/internal/hostfs"
	"github.com/meros/fyr/internal/notify"
	"github.com/meros/fyr/internal/push"
	"github.com/meros/fyr/internal/store"
)

// Pusher is what the push endpoints need (*push.Pusher implements it).
type Pusher interface {
	PublicKey() string
	Subscribe(user string, sub push.Subscription, device string) error
	Unsubscribe(endpoint string) error
	Notify(user string, pl push.Payload) int
	Known(endpoint string) bool
}

type Server struct {
	Engine  *engine.Engine
	MCP     http.Handler // agents' MCP endpoint; optional
	Web     fs.FS        // the built web client
	BuildID string
	Host    string // host name shown in the rail; default os.Hostname
	// OwnerLogin is the Tailscale login allowed over the tailnet (V0.14).
	// Empty refuses every tailnet request.
	OwnerLogin string
	// Push serves the Web Push endpoints (V0.15); optional.
	Push Pusher
	// Presence records which thread each device shows (V0.20); optional.
	Presence *notify.Presence
	// Attach stores uploaded files (V0.18); optional.
	Attach   *attach.Store
	Profiles []agent.Profile
	// T3Candidates lists adoptable T3 threads; optional.
	T3Candidates func() (any, error)
	// FS serves the host folder picker (V0.42); optional.
	FS *hostfs.FS
	// PR reads PR details for the PR tab (V0.43); optional.
	PR PRDetails
	// Quota lists the profiles' provider quota (V0.44); optional.
	Quota func(ctx context.Context) any
	// HandoverPlan says which rung a handover would use (V0.45); optional.
	HandoverPlan func(threadID, profile string) agent.Plan
	// ProfileList is the current profiles (the providers, V0.47); it wins
	// over Profiles. Optional.
	ProfileList func() []agent.Profile
	// Providers serves Settings → Providers (V0.47); optional.
	Providers Providers
}

// Providers is what the provider endpoints need (*provider.Registry
// adapted in fyrd).
type Providers interface {
	List() any
	Found() any
	Command(c engine.Cmd) engine.Reply
}

// PRDetails is what the PR tab needs (*github.Details implements it).
type PRDetails interface {
	Get(ctx context.Context, url string, fresh bool) (github.Detail, error)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("GET /api/stream", s.stream)
	mux.HandleFunc("GET /api/threads/{id}", s.thread)
	mux.HandleFunc("GET /api/threads/{id}/events", s.events)
	mux.HandleFunc("GET /api/threads/{id}/events/{seq}", s.fullEvent)
	mux.HandleFunc("GET /api/threads/{id}/changes", s.changes)
	mux.HandleFunc("GET /api/threads/{id}/pr", s.prDetail)
	mux.HandleFunc("GET /api/threads/{id}/handover", s.handoverPlan)
	mux.HandleFunc("POST /api/command", s.command)
	mux.HandleFunc("GET /api/t3/candidates", s.t3)
	mux.HandleFunc("GET /api/quota", s.quota)
	if s.Providers != nil {
		mux.HandleFunc("GET /api/providers", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.Providers.List()) })
		mux.HandleFunc("GET /api/providers/found", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.Providers.Found()) })
	}
	if s.Presence != nil {
		mux.HandleFunc("POST /api/presence", func(w http.ResponseWriter, r *http.Request) {
			var b struct {
				ThreadID string `json:"threadId"`
				Device   string `json:"device"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&b); err != nil || b.Device == "" {
				http.Error(w, "threadId and device are required", 400)
				return
			}
			s.Presence.Beat(b.Device, b.ThreadID, time.Now())
			w.WriteHeader(http.StatusNoContent)
		})
	}
	if s.Attach != nil {
		mux.HandleFunc("POST /api/attachments", s.upload)
		mux.HandleFunc("GET /api/attachments/{id}", s.download)
		mux.HandleFunc("GET /api/previews/{id}", s.preview)
	}
	if s.Push != nil {
		mux.HandleFunc("GET /api/push/key", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]string{"publicKey": s.Push.PublicKey()})
		})
		mux.HandleFunc("POST /api/push/subscribe", s.pushSubscribe)
		mux.HandleFunc("POST /api/push/unsubscribe", s.pushUnsubscribe)
		mux.HandleFunc("POST /api/push/status", func(w http.ResponseWriter, r *http.Request) {
			var b struct {
				Endpoint string `json:"endpoint"`
			}
			json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&b)
			writeJSON(w, 200, map[string]bool{"known": b.Endpoint != "" && s.Push.Known(b.Endpoint)})
		})
		mux.HandleFunc("POST /api/push/test", func(w http.ResponseWriter, r *http.Request) {
			n := s.Push.Notify(core.Owner, push.Payload{Title: "Fyr", Body: "Notifications work on this device.", State: "test"})
			writeJSON(w, 200, map[string]int{"delivered": n})
		})
	}
	if s.FS != nil {
		s.fsRoutes(mux)
	}
	if s.MCP != nil {
		mux.Handle("/mcp", s.MCP)
	}
	if s.Web != nil {
		mux.Handle("/", noCacheIndex(http.FileServerFS(s.Web)))
	}
	return guard(s.OwnerLogin, withGzip(mux))
}

// guard refuses requests that are neither local nor the owner over the
// tailnet, and POSTs without X-Fyr (cross-site form posts). The MCP endpoint
// uses its bearer token.
//
// Local: a loopback Host header (this also stops DNS rebinding).
// Tailnet (SPEC 0.5 V0.14): Tailscale Serve proxies from loopback and adds
// Tailscale-User-Login; it must equal owner.
func guard(owner string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if login := r.Header.Get("Tailscale-User-Login"); login != "" {
			if owner == "" || login != owner {
				http.Error(w, "this Fyr belongs to another tailnet user", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodPost && r.URL.Path != "/mcp" && r.Header.Get("X-Fyr") != "1" {
				http.Error(w, "missing X-Fyr header", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "Fyr v0 serves loopback only", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path != "/mcp" && r.Header.Get("X-Fyr") != "1" {
			http.Error(w, "missing X-Fyr header", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func init() { mime.AddExtensionType(".webmanifest", "application/manifest+json") }

// noCacheIndex keeps the page, the manifest and the service worker fresh,
// so a new build is picked up at the next load (V0.13, V0.15).
func noCacheIndex(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/" || strings.HasSuffix(p, ".html") || p == "/sw.js" || p == "/manifest.webmanifest" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if p == "/manifest.webmanifest" {
			w.Header().Set("Content-Type", "application/manifest+json")
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

type actionMeta struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Key   string `json:"key,omitempty"`
	Bulk  bool   `json:"bulk,omitempty"`
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	threads := s.Engine.Snapshot()
	// V0.38: ?since=REV lists only threads changed after REV (revs grow
	// across restarts), for a client with a stored copy.
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	if since > 0 {
		changed := threads[:0:0]
		for _, d := range threads {
			if d.Rev > since {
				changed = append(changed, d)
			}
		}
		threads = changed
	}
	var acts []actionMeta
	for _, a := range core.Ordered() {
		acts = append(acts, actionMeta{a.Name, a.Label, a.Key, a.Bulk})
	}
	// V0.23: projects with their identity; cwd is the folder on this host
	// (the new-thread picker uses it).
	type project struct {
		engine.ProjectDTO
		Cwd string `json:"cwd"`
	}
	projects := []project{}
	for _, p := range s.Engine.Projects() {
		projects = append(projects, project{p, p.Folder(s.Engine.HostID)})
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].LastUsed.After(projects[j].LastUsed) })
	src := s.Profiles
	if s.ProfileList != nil {
		src = s.ProfileList()
	}
	// A profile's env stays on the host (V0.47): it may name a secret.
	profiles := make([]agent.Profile, 0, len(src))
	for _, p := range src {
		p.Env = nil
		profiles = append(profiles, p)
	}
	writeJSON(w, 200, map[string]any{
		"now": time.Now().UTC(), "user": core.Owner, "buildId": s.BuildID, "host": s.hostName(),
		"threads": threads, "actions": acts, "profiles": profiles, "projects": projects,
		"partial": since > 0, "deploy": s.Engine.DeployState(time.Now()),
	})
}

func (s *Server) hostName() string {
	if s.Host != "" {
		return s.Host
	}
	h, _ := os.Hostname()
	return h
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	ch := s.Engine.Hub.Subscribe()
	defer s.Engine.Hub.Unsubscribe(ch)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	hello, _ := json.Marshal(map[string]any{"now": time.Now().UTC(), "buildId": s.BuildID})
	fmt.Fprintf(w, "event: hello\ndata: %s\n\n", hello)
	fl.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	// V0.38: after the first whole thread, this connection gets merge patches
	// against what it already has; a thread goes out at most every 100 ms,
	// the newest version winning.
	sent := map[string]map[string]any{}
	lastAt := map[string]time.Time{}
	pending := map[string][]byte{}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	sendThread := func(data []byte) {
		var m map[string]any
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber() // revs are nanosecond counts above 2^53
		if dec.Decode(&m) != nil {
			fmt.Fprintf(w, "event: thread\ndata: %s\n\n", data)
			return
		}
		id, _ := m["id"].(string)
		lastAt[id] = time.Now()
		if prev, ok := sent[id]; ok {
			out, err := json.Marshal(map[string]any{"id": id, "rev": m["rev"], "base": prev["rev"], "patch": engine.MergePatchDiff(prev, m)})
			if err == nil && len(out) < len(data) {
				sent[id] = m
				fmt.Fprintf(w, "event: thread.patch\ndata: %s\n\n", out)
				return
			}
		}
		sent[id] = m
		fmt.Fprintf(w, "event: thread\ndata: %s\n\n", data)
	}
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				return // dropped as slow; the client reconnects
			}
			if f.Event == "thread" {
				var head struct {
					ID string `json:"id"`
				}
				json.Unmarshal(f.Data, &head)
				if time.Since(lastAt[head.ID]) < 100*time.Millisecond {
					pending[head.ID] = f.Data
					continue
				}
				delete(pending, head.ID)
				sendThread(f.Data)
				fl.Flush()
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", f.Event, f.Data)
			fl.Flush()
		case <-tick.C:
			if len(pending) == 0 {
				continue
			}
			for id, d := range pending {
				delete(pending, id)
				sendThread(d)
			}
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 2000 {
		limit = 400
	}
	var evs []store.Event
	var err error
	if before > 0 {
		evs, err = s.Engine.EventsBefore(r.PathValue("id"), before, limit)
	} else {
		evs, err = s.Engine.Events(r.PathValue("id"), after, limit)
	}
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	if evs == nil {
		evs = []store.Event{} // JSON [] instead of null
	}
	writeJSON(w, 200, map[string]any{"events": engine.LeanAll(evs)})
}

// thread returns one thread's DTO (V0.38): a client whose copy does not
// match a patch's base asks for the whole thread.
func (s *Server) thread(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	for _, d := range s.Engine.Snapshot() {
		if d.ID == id {
			writeJSON(w, 200, d)
			return
		}
	}
	writeJSON(w, 404, map[string]any{"error": "no such thread"})
}

// fullEvent returns one event whole (V0.38): the stream and the list cut
// large tool output.
func (s *Server) fullEvent(w http.ResponseWriter, r *http.Request) {
	seq, err := strconv.ParseInt(r.PathValue("seq"), 10, 64)
	if err != nil || seq <= 0 {
		writeJSON(w, 400, map[string]any{"error": "seq must be a positive number"})
		return
	}
	evs, err := s.Engine.Events(r.PathValue("id"), seq-1, 1)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	if len(evs) == 0 || evs[0].Seq != seq {
		writeJSON(w, 404, map[string]any{"error": "no such event"})
		return
	}
	writeJSON(w, 200, evs[0])
}

// changes serves the Changes tab (SPEC 0.5 V0.25): the branch diff of the
// thread's folder, or with ?path= one file's patch.
func (s *Server) changes(w http.ResponseWriter, r *http.Request) {
	t, ok := s.Engine.Thread(r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]any{"error": "no such thread"})
		return
	}
	if r.URL.Query().Has("path") {
		p, err := gitdiff.FilePatch(r.Context(), t.Cwd, r.URL.Query().Get("path"))
		if err != nil {
			writeJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, p)
		return
	}
	c, err := gitdiff.Summary(r.Context(), t.Cwd)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	if !c.Git {
		writeJSON(w, 200, map[string]bool{"git": false})
		return
	}
	writeJSON(w, 200, c)
}

// prDetail serves the PR tab (SPEC 0.5 V0.43). Failures are 200 {error}, so
// the tab can say what went wrong.
func (s *Server) prDetail(w http.ResponseWriter, r *http.Request) {
	t, ok := s.Engine.Thread(r.PathValue("id"))
	if !ok {
		writeJSON(w, 404, map[string]any{"error": "no such thread"})
		return
	}
	if t.PR == nil || t.PR.URL == "" {
		writeJSON(w, 200, map[string]any{"error": "This thread has no PR."})
		return
	}
	if s.PR == nil {
		writeJSON(w, 200, map[string]any{"error": "This host does not read PR details."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	d, err := s.PR.Get(ctx, t.PR.URL, r.URL.Query().Get("fresh") == "1")
	if err != nil {
		writeJSON(w, 200, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, d)
}

// handoverPlan answers which rung a handover to ?profile= would use (V0.45).
func (s *Server) handoverPlan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.Engine.Thread(id); !ok {
		writeJSON(w, 404, map[string]any{"error": "no such thread"})
		return
	}
	if s.HandoverPlan == nil {
		writeJSON(w, 200, agent.Plan{Rung: agent.RungAdopt})
		return
	}
	writeJSON(w, 200, s.HandoverPlan(id, r.URL.Query().Get("profile")))
}

func (s *Server) command(w http.ResponseWriter, r *http.Request) {
	var c engine.Cmd
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&c); err != nil {
		writeJSON(w, 400, engine.Reply{Error: "bad JSON: " + err.Error()})
		return
	}
	var rep engine.Reply
	if strings.HasPrefix(c.Action, "provider.") {
		if s.Providers == nil {
			writeJSON(w, 404, engine.Reply{Error: "no providers on this host"})
			return
		}
		rep = s.Providers.Command(c)
	} else {
		rep = s.Engine.Command(c)
	}
	if rep.Status == 0 {
		rep.Status = 200
	}
	writeJSON(w, rep.Status, rep)
}

func (s *Server) t3(w http.ResponseWriter, r *http.Request) {
	if s.T3Candidates == nil {
		writeJSON(w, 200, map[string]any{"threads": []any{}})
		return
	}
	c, err := s.T3Candidates()
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"threads": c})
}

func (s *Server) pushSubscribe(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Subscription push.Subscription `json:"subscription"`
		Device       string            `json:"device"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&b); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "bad JSON"})
		return
	}
	if err := s.Push.Subscribe(core.Owner, b.Subscription, b.Device); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) pushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&b); err != nil || b.Endpoint == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "endpoint is required"})
		return
	}
	s.Push.Unsubscribe(b.Endpoint)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	name, err := url.PathUnescape(r.Header.Get("X-Fyr-Filename"))
	if err != nil {
		name = r.Header.Get("X-Fyr-Filename")
	}
	m, err := s.Attach.Save(name, r.Header.Get("Content-Type"), r.Body)
	if errors.Is(err, attach.ErrTooLarge) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, m)
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	m, err := s.Attach.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(m.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", m.MediaType)
	w.Header().Set("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(m.Name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	http.ServeContent(w, r, m.Name, time.Time{}, f)
}

// preview serves an HTML preview (V0.19) in a sandbox without an origin:
// its scripts run, but it cannot call the API, read cookies or fetch.
func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	m, err := s.Attach.Get(r.PathValue("id"))
	if err != nil || m.MediaType != "text/html" {
		http.NotFound(w, r)
		return
	}
	b, err := os.ReadFile(m.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; media-src data: blob:; connect-src 'none'; frame-ancestors 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Write(b)
}

// fsRoutes are the host folder picker's calls (SPEC 0.5 V0.42): read-only
// except mkdir, confined to the owner's home folder; a refused path is 400.
func (s *Server) fsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/fs/list", func(w http.ResponseWriter, r *http.Request) {
		l, err := s.FS.List(r.URL.Query().Get("path"))
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, l)
	})
	mux.HandleFunc("GET /api/fs/recent", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"dirs": s.FS.RecentDirs()})
	})
	mux.HandleFunc("GET /api/fs/repos", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"dirs": s.FS.Repos()})
	})
	mux.HandleFunc("POST /api/fs/mkdir", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Parent string `json:"parent"`
			Name   string `json:"name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&b); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "bad JSON"})
			return
		}
		p, err := s.FS.Mkdir(b.Parent, b.Name)
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "path": p})
	})
}

// quota answers GET /api/quota (V0.44): due providers are read, at most 8 s.
func (s *Server) quota(w http.ResponseWriter, r *http.Request) {
	if s.Quota == nil {
		writeJSON(w, 200, []any{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	writeJSON(w, 200, s.Quota(ctx))
}
