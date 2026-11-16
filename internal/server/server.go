// Package server is the v0 HTTP transport (SPEC 0.5 V0.2, decision 0002,
// docs/API-v0.md): JSON commands, a snapshot, an SSE stream, the agents'
// MCP endpoint. A front serves the web client and the device data (push,
// presence) since V0.51 and V0.54.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/gitdiff"
	"github.com/meros/fyr/internal/github"
	"github.com/meros/fyr/internal/hostfs"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/store"
	"github.com/meros/fyr/internal/stream"
)

type Server struct {
	Engine  *engine.Engine
	MCP     http.Handler // agents' MCP endpoint; optional
	BuildID string
	Host    string // host name shown in the rail; default os.Hostname
	// OwnerLogin is the Tailscale login allowed over the tailnet (V0.14).
	// Empty refuses every tailnet request.
	OwnerLogin string
	// Attach stores uploaded files (V0.18); optional.
	Attach   *attach.Store
	Profiles []provider.Profile
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
	// ProfileList is the current profiles (the providers, V0.50); it wins
	// over Profiles. Optional.
	ProfileList func() []provider.Profile
	// Providers serves Settings → Providers (V0.50); optional.
	Providers Providers
}

// Providers is what the provider endpoints need (*provider.Registry
// adapted in fyrd).
type Providers interface {
	List() any
	Found() any
}

// PRDetails is what the PR tab needs (*github.Details implements it).
type PRDetails interface {
	Get(ctx context.Context, url string, fresh bool) (github.Detail, error)
}

func (s *Server) Handler() http.Handler { return guard(s.OwnerLogin, withGzip(s.routes())) }

// PeerHandler serves the host protocol on a tailnet listener (V0.51): the
// same routes as Handler, guarded by the peer's Tailscale identity.
func (s *Server) PeerHandler(peers PeerAuth) http.Handler {
	return PeerGuard(s.OwnerLogin, peers, withGzip(s.routes()))
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("GET /api/stream", s.stream)
	mux.HandleFunc("GET /api/threads/{id}", s.thread)
	mux.HandleFunc("GET /api/threads/{id}/events", s.events)
	mux.HandleFunc("GET /api/threads/{id}/events/{seq}", s.fullEvent)
	mux.HandleFunc("GET /api/threads/{id}/changes", s.changes)
	mux.HandleFunc("GET /api/threads/{id}/pr", s.prDetail)
	mux.HandleFunc("GET /api/threads/{id}/handover", s.handoverPlan)
	mux.HandleFunc("GET /api/threads/{id}/commands", s.threadCommands)
	mux.HandleFunc("GET /api/commands", s.folderCommands)
	mux.HandleFunc("POST /api/command", s.command)
	mux.HandleFunc("GET /api/t3/candidates", s.t3)
	mux.HandleFunc("GET /api/quota", s.quota)
	// #42: the profiles alone, for the client after a provider changed.
	mux.HandleFunc("GET /api/profiles", func(w http.ResponseWriter, r *http.Request) { api.WriteJSON(w, 200, s.profiles()) })
	if s.Providers != nil {
		mux.HandleFunc("GET /api/providers", func(w http.ResponseWriter, r *http.Request) { api.WriteJSON(w, 200, s.Providers.List()) })
		mux.HandleFunc("GET /api/providers/found", func(w http.ResponseWriter, r *http.Request) { api.WriteJSON(w, 200, s.Providers.Found()) })
	}
	if s.Attach != nil {
		mux.HandleFunc("POST /api/attachments", s.upload)
		mux.HandleFunc("GET /api/attachments/{id}", s.download)
		mux.HandleFunc("GET /api/previews/{id}", s.preview)
		mux.HandleFunc("GET /api/previews/{id}/{path...}", s.previewFile)
	}
	if s.FS != nil {
		s.fsRoutes(mux)
	}
	if s.MCP != nil {
		mux.Handle("/mcp", s.MCP)
	}
	return mux
}

// guard refuses requests that are neither local nor the owner over the
// tailnet, and POSTs without X-Fyr (cross-site form posts). The MCP endpoint
// uses its bearer token.
//
// Local: a loopback Host header (this also stops DNS rebinding).
// Tailnet (SPEC 0.5 V0.14): Tailscale Serve proxies from loopback, keeps
// the browser's Host (https://<host>.ts.net) and adds Tailscale-User-Login;
// it must equal owner. The header alone proves nothing: a DNS-rebinding page
// reaches loopback too and may set any header, but its Host is its own
// domain, never a ts.net name (review 2026-11-15).
func guard(owner string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if login := r.Header.Get("Tailscale-User-Login"); login != "" {
			if !tailnetHost(r.Host) {
				http.Error(w, "the tailnet identity header needs a ts.net host", http.StatusForbidden)
				return
			}
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

// tailnetHost reports whether a Host header names a MagicDNS host, the only
// name Tailscale Serve answers on with HTTPS.
func tailnetHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return strings.HasSuffix(host, ".ts.net") && len(host) > len(".ts.net")
}

// Browser wraps a browser-facing handler (the host's own, or a front's,
// V0.51) in the loopback and tailnet guard and gzip.
func Browser(owner string, next http.Handler) http.Handler { return guard(owner, withGzip(next)) }

// PeerAuth names the Tailscale login that owns the peer at a remote address
// (tailscaled whois; V0.51).
type PeerAuth interface {
	Login(ctx context.Context, remoteAddr string) (string, error)
}

// PeerGuard guards a host's tailnet listener (SPEC 0.5 V0.51): a front on
// another machine is served only when its connection's Tailscale identity is
// the owner's login. Headers are not trusted there: no Tailscale Serve sits in
// front of this listener. POSTs still need X-Fyr.
func PeerGuard(owner string, peers PeerAuth, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		login, err := peers.Login(ctx, r.RemoteAddr)
		cancel()
		if err != nil || owner == "" || login != owner {
			http.Error(w, "this Fyr host serves only its owner's devices", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path != "/mcp" && r.Header.Get("X-Fyr") != "1" {
			http.Error(w, "missing X-Fyr header", http.StatusForbidden)
			return
		}
		r.Header.Del("Tailscale-User-Login")
		next.ServeHTTP(w, r)
	})
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
	var acts []api.Action
	for _, a := range core.Ordered() {
		acts = append(acts, api.Action{Name: a.Name, Label: a.Label, Key: a.Key, Bulk: a.Bulk})
	}
	// V0.23, V0.55: projects with their identity and their one folder.
	projects := s.Engine.Projects()
	sort.Slice(projects, func(i, j int) bool { return projects[i].LastUsed.After(projects[j].LastUsed) })
	profiles := s.profiles()
	api.WriteJSON(w, 200, api.State[api.Thread, api.Project, provider.Profile]{
		Now: time.Now().UTC(), User: core.Owner, BuildID: s.BuildID, Host: s.hostName(),
		Threads: threads, Actions: acts, Profiles: profiles, Projects: projects,
		Partial: since > 0, Deploy: s.Engine.DeployState(time.Now()),
	})
}

// profiles are the current profiles as clients see them: ProfileList when
// set, else Profiles. A profile's env stays on the host (V0.50): it may name
// a secret.
func (s *Server) profiles() []provider.Profile {
	src := s.Profiles
	if s.ProfileList != nil {
		src = s.ProfileList()
	}
	out := make([]provider.Profile, 0, len(src))
	for _, p := range src {
		p.Env = nil
		out = append(out, p)
	}
	return out
}

func (s *Server) hostName() string {
	if s.Host != "" {
		return s.Host
	}
	h, _ := os.Hostname()
	return h
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	ch := s.Engine.Hub.Subscribe()
	defer s.Engine.Hub.Unsubscribe(ch)
	stream.Serve(w, r, ch, s.BuildID)
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
		api.WriteJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	if evs == nil {
		evs = []store.Event{} // JSON [] instead of null
	}
	api.WriteJSON(w, 200, map[string]any{"events": engine.LeanAll(evs)})
}

// defaultProfile is the profile a thread without one runs on: the first.
func (s *Server) defaultProfile() string {
	if ps := s.profiles(); len(ps) > 0 {
		return ps[0].ID
	}
	return ""
}

// threadCommands serves the composer's slash commands (V0.79 (2)).
func (s *Server) threadCommands(w http.ResponseWriter, r *http.Request) {
	cmds, from, ok := s.Engine.ThreadCommands(r.PathValue("id"), s.defaultProfile())
	if !ok {
		api.WriteJSON(w, 404, map[string]any{"error": "no such thread"})
		return
	}
	api.WriteJSON(w, 200, map[string]any{"commands": cmds, "from": from})
}

// folderCommands serves the newest commands of a provider and folder, for
// New thread (V0.79 (2)).
func (s *Server) folderCommands(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p := q.Get("profile")
	if p == "" {
		p = s.defaultProfile()
	}
	api.WriteJSON(w, 200, map[string]any{"commands": s.Engine.FolderCommands(p, q.Get("cwd"))})
}

// thread returns one thread's DTO (V0.38): a client whose copy does not
// match a patch's base asks for the whole thread.
func (s *Server) thread(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	for _, d := range s.Engine.Snapshot() {
		if d.ID == id {
			api.WriteJSON(w, 200, d)
			return
		}
	}
	api.WriteJSON(w, 404, map[string]any{"error": "no such thread"})
}

// fullEvent returns one event whole (V0.38): the stream and the list cut
// large tool output.
func (s *Server) fullEvent(w http.ResponseWriter, r *http.Request) {
	seq, err := strconv.ParseInt(r.PathValue("seq"), 10, 64)
	if err != nil || seq <= 0 {
		api.WriteJSON(w, 400, map[string]any{"error": "seq must be a positive number"})
		return
	}
	evs, err := s.Engine.Events(r.PathValue("id"), seq-1, 1)
	if err != nil {
		api.WriteJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	if len(evs) == 0 || evs[0].Seq != seq {
		api.WriteJSON(w, 404, map[string]any{"error": "no such event"})
		return
	}
	api.WriteJSON(w, 200, evs[0])
}

// changes serves the Changes tab (SPEC 0.5 V0.25): the branch diff of the
// thread's folder, or with ?path= one file's patch.
func (s *Server) changes(w http.ResponseWriter, r *http.Request) {
	t, ok := s.Engine.Thread(r.PathValue("id"))
	if !ok {
		api.WriteJSON(w, 404, map[string]any{"error": "no such thread"})
		return
	}
	if r.URL.Query().Has("path") {
		var p gitdiff.Patch
		var err error
		if r.URL.Query().Has("since") { // V0.65: from the seen head
			p, err = gitdiff.FilePatchSince(r.Context(), t.Cwd, r.URL.Query().Get("path"), r.URL.Query().Get("since"))
			if errors.Is(err, gitdiff.ErrBadSince) {
				api.WriteJSON(w, 400, map[string]any{"error": err.Error()})
				return
			}
		} else {
			p, err = gitdiff.FilePatch(r.Context(), t.Cwd, r.URL.Query().Get("path"))
		}
		if err != nil {
			api.WriteJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
		api.WriteJSON(w, 200, p)
		return
	}
	c, err := gitdiff.Summary(r.Context(), t.Cwd)
	if err != nil {
		api.WriteJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	if !c.Git {
		api.WriteJSON(w, 200, map[string]bool{"git": false})
		return
	}
	// V0.65: the owner's Viewed marks and what is new since the last look.
	look := s.Engine.ChangesLook(t.ID)
	if look != nil {
		lv := &core.View{ChangesLook: look}
		for i := range c.Files {
			c.Files[i].Viewed = core.FileViewed(lv, c.Files[i].Path, c.Files[i].Blob)
		}
		if look.Files != nil || look.Head != "" {
			since, err := gitdiff.SinceSeen(r.Context(), t.Cwd, c, gitdiff.Seen{Head: look.Head, Files: look.Files})
			if err != nil {
				api.WriteJSON(w, 500, map[string]any{"error": err.Error()})
				return
			}
			since.At = look.At
			for i := range since.Files {
				since.Files[i].Viewed = core.FileViewed(lv, since.Files[i].Path, since.Files[i].Blob)
			}
			c.Since = since
		}
	}
	api.WriteJSON(w, 200, c)
}

// prDetail serves the PR tab (SPEC 0.5 V0.43). Failures are 200 {error}, so
// the tab can say what went wrong.
func (s *Server) prDetail(w http.ResponseWriter, r *http.Request) {
	t, ok := s.Engine.Thread(r.PathValue("id"))
	if !ok {
		api.WriteJSON(w, 404, map[string]any{"error": "no such thread"})
		return
	}
	pr := t.MainPR()
	if pr == nil || pr.URL == "" {
		api.WriteJSON(w, 200, map[string]any{"error": "This thread has no PR."})
		return
	}
	if s.PR == nil {
		api.WriteJSON(w, 200, map[string]any{"error": "This host does not read PR details."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	d, err := s.PR.Get(ctx, pr.URL, r.URL.Query().Get("fresh") == "1")
	if err != nil {
		api.WriteJSON(w, 200, map[string]any{"error": err.Error()})
		return
	}
	api.WriteJSON(w, 200, d)
}

// handoverPlan answers which rung a handover to ?profile= would use (V0.45).
func (s *Server) handoverPlan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.Engine.Thread(id); !ok {
		api.WriteJSON(w, 404, map[string]any{"error": "no such thread"})
		return
	}
	if s.HandoverPlan == nil {
		api.WriteJSON(w, 200, agent.Plan{Rung: agent.RungAdopt})
		return
	}
	api.WriteJSON(w, 200, s.HandoverPlan(id, r.URL.Query().Get("profile")))
}

func (s *Server) command(w http.ResponseWriter, r *http.Request) {
	var c api.Cmd
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&c); err != nil {
		api.WriteJSON(w, 400, api.Reply{Error: "bad JSON: " + err.Error()})
		return
	}
	rep := s.Engine.Command(c) // #48: provider commands too
	if rep.Status == 0 {
		rep.Status = 200
	}
	api.WriteJSON(w, rep.Status, rep)
}

func (s *Server) t3(w http.ResponseWriter, r *http.Request) {
	if s.T3Candidates == nil {
		api.WriteJSON(w, 200, map[string]any{"threads": []any{}})
		return
	}
	c, err := s.T3Candidates()
	if err != nil {
		api.WriteJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	api.WriteJSON(w, 200, map[string]any{"threads": c})
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	name, err := url.PathUnescape(r.Header.Get("X-Fyr-Filename"))
	if err != nil {
		name = r.Header.Get("X-Fyr-Filename")
	}
	m, err := s.Attach.Save(name, r.Header.Get("Content-Type"), r.Body)
	if errors.Is(err, attach.ErrTooLarge) {
		api.WriteJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if err != nil {
		api.WriteJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	api.WriteJSON(w, 200, m)
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

// previewCDNs may serve scripts, models, decoders and fonts to a preview
// (V0.47); ajax.googleapis.com hosts model-viewer, www.gstatic.com the Draco
// and Basis decoders three.js and model-viewer load by default.
var previewCDNs = "https://cdnjs.cloudflare.com https://cdn.jsdelivr.net https://unpkg.com https://esm.sh https://ajax.googleapis.com https://www.gstatic.com"

var hostRe = regexp.MustCompile(`^[A-Za-z0-9.\-]+(:[0-9]+)?$|^\[[0-9A-Fa-f:.]+\](:[0-9]+)?$`)

// previewCSP is the policy of every preview response (V0.19, V0.47): a
// sandbox without allow-same-origin, so the page never has Fyr's origin and
// cannot call the API; it may load from its own folder and the CDN list.
// The folder is an absolute prefix: a page without an origin has no 'self'.
func previewCSP(r *http.Request, id string) string {
	base := ""
	if hostRe.MatchString(r.Host) {
		scheme := "http" // CSP3: an http source also matches https
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		base = scheme + "://" + r.Host + "/api/previews/" + id + "/ "
	}
	src := base + previewCDNs
	return "sandbox allow-scripts; default-src 'none'; " +
		"script-src " + src + " 'unsafe-inline' 'unsafe-eval' 'wasm-unsafe-eval' blob:; " +
		"style-src " + src + " 'unsafe-inline'; " +
		"img-src " + src + " data: blob:; " +
		"font-src " + src + " data:; " +
		"media-src " + src + " data: blob:; " +
		"connect-src " + src + " data: blob:; " +
		"worker-src " + base + "blob:; " +
		"frame-ancestors 'self'"
}

// previewTypes are the media types of a folder preview's files; others come
// from the extension table, then application/octet-stream.
var previewTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8",
	".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8",
	".json": "application/json", ".map": "application/json", ".wasm": "application/wasm", ".txt": "text/plain; charset=utf-8",
	".glb": "model/gltf-binary", ".gltf": "model/gltf+json", ".bin": "application/octet-stream", ".obj": "model/obj", ".stl": "model/stl",
	".usdz": "model/vnd.usdz+zip", ".hdr": "image/vnd.radiance", ".exr": "image/x-exr", ".ktx2": "image/ktx2", ".basis": "application/octet-stream",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif", ".svg": "image/svg+xml",
	".mp4": "video/mp4", ".webm": "video/webm", ".mp3": "audio/mpeg", ".ogg": "audio/ogg", ".wav": "audio/wav",
	".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf", ".otf": "font/otf",
}

func previewHeaders(w http.ResponseWriter, r *http.Request, id, mediaType string) {
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Security-Policy", previewCSP(r, id))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// The page has no origin: its module scripts and fetches of its own files
	// are cross-origin. The 80-bit id is the key, as for the page itself.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "private, max-age=86400")
}

// preview serves an HTML preview (V0.19) in a sandbox without an origin; a
// folder preview (V0.47) redirects to its page, so relative URLs resolve.
func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m, err := s.Attach.Get(id)
	if err != nil || m.MediaType != "text/html" {
		http.NotFound(w, r)
		return
	}
	if m.Entry != "" {
		http.Redirect(w, r, "/api/previews/"+id+"/"+(&url.URL{Path: m.Entry}).EscapedPath(), http.StatusFound)
		return
	}
	b, err := os.ReadFile(m.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	previewHeaders(w, r, id, "text/html; charset=utf-8")
	w.Write(b)
}

// previewFile serves one file of a folder preview (V0.47).
func (s *Server) previewFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m, err := s.Attach.Get(id)
	if err != nil || m.Entry == "" {
		http.NotFound(w, r)
		return
	}
	p, err := s.Attach.TreePath(m, r.PathValue("path"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	ext := strings.ToLower(filepath.Ext(p))
	mt := previewTypes[ext]
	if mt == "" {
		if mt = mime.TypeByExtension(ext); mt == "" {
			mt = "application/octet-stream"
		}
	}
	previewHeaders(w, r, id, mt)
	http.ServeContent(w, r, "", time.Time{}, f)
}

// fsRoutes are the host folder picker's calls (SPEC 0.5 V0.42): read-only
// except mkdir, confined to the owner's home folder; a refused path is 400.
func (s *Server) fsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/fs/list", func(w http.ResponseWriter, r *http.Request) {
		l, err := s.FS.List(r.URL.Query().Get("path"))
		if err != nil {
			api.WriteJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		api.WriteJSON(w, 200, l)
	})
	mux.HandleFunc("GET /api/fs/recent", func(w http.ResponseWriter, r *http.Request) {
		api.WriteJSON(w, 200, map[string]any{"dirs": s.FS.RecentDirs()})
	})
	mux.HandleFunc("GET /api/fs/repos", func(w http.ResponseWriter, r *http.Request) {
		api.WriteJSON(w, 200, map[string]any{"dirs": s.FS.Repos()})
	})
	mux.HandleFunc("POST /api/fs/mkdir", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Parent string `json:"parent"`
			Name   string `json:"name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&b); err != nil {
			api.WriteJSON(w, 400, map[string]any{"ok": false, "error": "bad JSON"})
			return
		}
		p, err := s.FS.Mkdir(b.Parent, b.Name)
		if err != nil {
			api.WriteJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		api.WriteJSON(w, 200, map[string]any{"ok": true, "path": p})
	})
}

// quota answers GET /api/quota (V0.44): due providers are read, at most 8 s.
func (s *Server) quota(w http.ResponseWriter, r *http.Request) {
	if s.Quota == nil {
		api.WriteJSON(w, 200, []any{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	api.WriteJSON(w, 200, s.Quota(ctx))
}
