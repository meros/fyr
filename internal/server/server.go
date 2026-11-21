// Package server is the v0 HTTP transport (SPEC 0.5 V0.2, decision 0002,
// docs/API-v0.md): JSON commands, a snapshot, an SSE stream, the agents'
// MCP endpoint. A front serves the web client and the device data (push,
// presence) since V0.51 and V0.54.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/meros/fyr/internal/machine"
	"io"
	"log"
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
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/authn"
	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/gitdiff"
	"github.com/meros/fyr/internal/github"
	"github.com/meros/fyr/internal/hostfs"
	"github.com/meros/fyr/internal/httpguard"
	"github.com/meros/fyr/internal/projicon"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/store"
	"github.com/meros/fyr/internal/stream"
)

type Server struct {
	legacyFronts atomic.Int64 // LegacyFronts
	Engine       *engine.Engine
	MCP          http.Handler // agents' MCP endpoint; optional
	BuildID      string
	// OnWhois hears each request served on the whois listener (the old link), which
	// the peer way in's enforce mode removes (decision 0030 slice 5).
	OnWhois func()
	// ID is this install's key id (V0.124, decision 0026), in the state.
	ID   string
	Host string // host name shown in the rail; default os.Hostname
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
	// Focus raises or drops the CPU weight of a thread a device shows
	// (V0.85); optional.
	Focus func(device, thread string, on bool)
	// Load reads the machine's and the threads' load (V0.80); optional.
	Load *cgroup.Sampler
	// HandoverPlan says which rung a handover would use (V0.45); optional.
	HandoverPlan func(threadID, profile string) agent.Plan
	// HandoverExport and HandoverImport carry sessions across hosts (decision 0039, V0.trans-host-handover); optional.
	HandoverExport func(threadID string, wantBrief bool, targetLabel string) (agent.ExportAgentData, error)
	HandoverImport func(threadID string, req agent.HandoverInReq) (agent.HandoverInResult, error)
	// ProfileList is the current profiles (the providers, V0.50); it wins
	// over Profiles. Optional.
	ProfileList func() []provider.Profile
	// Providers serves Settings → Providers (V0.50); optional.
	Providers Providers
	// Updates is the update record and state (V0.126); UpdateLog reads a
	// build's log. Optional.
	Updates   func() any
	UpdateLog func(build, name string) ([]byte, error)
	// UpdateState is this host's update state for its fronts
	// (V0.host-updates); nil sends none.
	UpdateState func() any
	// Setup reports this host's setup wizard checks (decision 0037). Optional.
	Setup func(ctx context.Context) any
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

// Handler is the host's loopback listener, behind the browser guard (V0.14).
// Fyr-Actor and Fyr-Origin mean nothing here: only the pipe and the peer
// listeners, where the channel proved which install speaks, read them
// (decision 0030 3.3).
func (s *Server) Handler() http.Handler {
	return httpguard.Guard(s.OwnerLogin, httpguard.Gzip(withDeadline(s.routes(true))))
}

// PipeHandler serves the in-process pipe between a front and its own host
// (V0.129 (3), decision 0030 3.2): only this process can dial it, so there is
// no browser guard, and the request must carry a well-formed Fyr-Actor (a
// missing or malformed one is 401, never a default). No /mcp: agents use the
// main port.
func (s *Server) PipeHandler() http.Handler {
	return withActor(actorPipe, s.ID, nil, httpguard.Gzip(withDeadline(s.routes(false))))
}

// PeerHandler serves the host protocol on a tailnet listener (V0.51): the
// same routes as Handler without /mcp, guarded by the peer's Tailscale
// identity and refusing a browser's Origin and a foreign Host (decision 0030
// slice 1). A front of an older build sends no Fyr-Actor and is still served,
// as a legacy front, and counted (LegacyFronts).
func (s *Server) PeerHandler(peers PeerAuth) http.Handler {
	inner := withActor(actorPeer, "", &s.legacyFronts, httpguard.Gzip(withDeadline(s.routes(false))))
	return PeerGuard(s.OwnerLogin, peers, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.OnWhois != nil { // a request over the old link: what the peer way's enforce will refuse
			s.OnWhois()
		}
		inner.ServeHTTP(w, r)
	}))
}

// PeerGate is what the host asks of the roster on the peer port (decision 0030
// 3.3, 3.7): which install a connection is, whether a device was revoked, and
// whether the install may drive this host.
type PeerGate interface {
	// Member is the install whose key the connection proved, when the roster
	// holds it.
	Member(r *http.Request) (id string, ok bool)
	DeviceRevoked(id string) bool
	MayDrive(install string) bool
	// DeviceKnown: a member signed this device in (a roster device-add).
	DeviceKnown(id string) bool
	// Enforcing: the peer way in is in enforce mode (decision 0030 5.1): a device
	// the roster does not know, and the legacy principal, are refused.
	Enforcing() bool
	// Track registers a request so that removing the install closes it (a stream is
	// one long request); done unregisters it.
	Track(install string, cancel context.CancelFunc) (done func())
	// Served counts a request served over the link (a success of the pinned way).
	Served()
}

// LinkHandler serves the host protocol on the peer port (V0.158): the channel
// proved which install speaks, so Fyr-Actor says which device it acts for. The
// header is read as on the pipe; a device the roster says was revoked is
// refused (401) whichever front forwards it; a call that changes things from an
// install the owner switched off here is refused (403). No /mcp.
func (s *Server) LinkHandler(g PeerGate) http.Handler {
	inner := withActor(actorLink, "", nil, httpguard.Gzip(withDeadline(s.routes(false))))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := g.Member(r)
		if !ok {
			http.Error(w, "this install is not in the roster", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !g.MayDrive(id) {
			api.WriteJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "this host does not take commands from that install"})
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer g.Track(id, cancel)()
		defer cancel()
		g.Served()
		inner.ServeHTTP(w, r.WithContext(context.WithValue(ctx, linkKey{}, linkInfo{id, g})))
	})
}

type linkKey struct{}
type linkInfo struct {
	install string
	gate    PeerGate
}

// routes is the host protocol; mcp adds the agents' endpoint, which only the
// main port serves.
func (s *Server) routes(mcp bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("GET /api/stream", s.stream)
	mux.HandleFunc("GET /api/threads/{id}", s.thread)
	mux.HandleFunc("GET /api/threads/{id}/events", s.events)
	mux.HandleFunc("GET /api/threads/{id}/events/{seq}", s.fullEvent)
	mux.HandleFunc("GET /api/threads/{id}/changes", s.changes)
	mux.HandleFunc("GET /api/threads/{id}/pr", s.prDetail)
	mux.HandleFunc("GET /api/threads/{id}/handover", s.handoverPlan)
	mux.HandleFunc("POST /api/threads/{id}/handover/export", s.handoverExport)
	mux.HandleFunc("POST /api/threads/handover/import", s.handoverImport)
	mux.HandleFunc("GET /api/threads/{id}/commands", s.threadCommands)
	mux.HandleFunc("POST /api/threads/{id}/focus", s.focus)
	mux.HandleFunc("GET /api/commands", s.folderCommands)
	mux.HandleFunc("POST /api/command", s.command)
	mux.HandleFunc("GET /api/t3/candidates", s.t3)
	mux.HandleFunc("GET /api/quota", s.quota)
	mux.HandleFunc("GET /api/load", s.load)
	mux.HandleFunc("POST /api/board/visit", s.boardVisit)
	mux.HandleFunc("POST /api/search", s.search)
	// V0.102: the repos and the owner's auto-merge setting of each.
	mux.HandleFunc("GET /api/autoMerge", func(w http.ResponseWriter, r *http.Request) { api.WriteJSON(w, 200, s.Engine.AutoMergeRepos()) })
	// V0.111: the owner's auto-deploy setting.
	mux.HandleFunc("GET /api/systemAgents", func(w http.ResponseWriter, r *http.Request) { api.WriteJSON(w, 200, s.Engine.SystemAgentsView()) }) // V0.115
	mux.HandleFunc("GET /api/autoDeploy", func(w http.ResponseWriter, r *http.Request) { api.WriteJSON(w, 200, s.Engine.AutoDeploy()) })
	// Decision 0037: the host's setup wizard checks (real host state, never a stored flag).
	mux.HandleFunc("GET /api/setup", func(w http.ResponseWriter, r *http.Request) {
		if s.Setup == nil {
			api.WriteJSON(w, 200, map[string]any{"ready": true, "done": 0, "total": 0, "steps": []any{}})
			return
		}
		api.WriteJSON(w, 200, s.Setup(r.Context()))
	})
	// V0.126 (#151): the update record and the logs kept with each build.
	mux.HandleFunc("GET /api/updates", func(w http.ResponseWriter, r *http.Request) {
		if s.Updates == nil {
			api.WriteJSON(w, 200, map[string]any{"updates": []any{}})
			return
		}
		api.WriteJSON(w, 200, s.Updates())
	})
	mux.HandleFunc("GET /api/updates/{build}/{name}", func(w http.ResponseWriter, r *http.Request) {
		if s.UpdateLog == nil {
			http.NotFound(w, r)
			return
		}
		b, err := s.UpdateLog(r.PathValue("build"), r.PathValue("name"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(b)
	})
	// #42: the profiles alone, for the client after a provider changed.
	mux.HandleFunc("GET /api/profiles", func(w http.ResponseWriter, r *http.Request) { api.WriteJSON(w, 200, s.profiles()) })
	if s.Providers != nil {
		mux.HandleFunc("GET /api/providers", func(w http.ResponseWriter, r *http.Request) { api.WriteJSON(w, 200, s.Providers.List()) })
		mux.HandleFunc("GET /api/providers/found", func(w http.ResponseWriter, r *http.Request) { api.WriteJSON(w, 200, s.Providers.Found()) })
	}
	// V0.116: a project's image icon.
	mux.HandleFunc("GET /api/projects/{id}/icon", s.projectIcon)
	mux.HandleFunc("POST /api/projects/{id}/icon", s.projectIconUpload)
	if s.Attach != nil {
		mux.HandleFunc("POST /api/attachments", s.upload)
		mux.HandleFunc("GET /api/attachments/{id}", s.download)
		mux.HandleFunc("GET /api/previews/{id}", s.preview)
		mux.HandleFunc("GET /api/previews/{id}/{path...}", s.previewFile)
	}
	if s.FS != nil {
		s.fsRoutes(mux)
	}
	if s.MCP != nil && mcp {
		mux.Handle("/mcp", s.MCP)
	}
	return mux
}

// PeerAuth names the Tailscale login that owns the peer at a remote address
// (tailscaled whois; V0.51).
type PeerAuth interface {
	Login(ctx context.Context, remoteAddr string) (string, error)
}

// PeerGuard guards a host's tailnet listener (SPEC 0.5 V0.51): a front on
// another machine is served only when its connection's Tailscale identity is
// the owner's login. Headers are not trusted there: no Tailscale Serve sits in
// front of this listener. POSTs still need X-Fyr. Decision 0030 slice 1: a
// request with an Origin header is a browser's (a front sends none), and one
// whose Host is not an address or a name of this node is a rebinding page's;
// both get nothing, before the whois is asked.
func PeerGuard(owner string, peers PeerAuth, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" {
			http.Error(w, "this Fyr host serves fronts, not browsers", http.StatusForbidden)
			return
		}
		if !peerHostOK(r, peers) {
			http.Error(w, "this Fyr host answers only to its own address", http.StatusForbidden)
			return
		}
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

// PeerNames is implemented by a PeerAuth that knows this node's own MagicDNS
// names (tailnet.Peers does), which a front may use as the host.
type PeerNames interface {
	Names(ctx context.Context) []string
}

// peerHostOK: the Host is an IP literal, a ts.net name, or one of this node's
// own names. A rebinding page's Host is its own domain.
func peerHostOK(r *http.Request, peers PeerAuth) bool {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(strings.ToLower(strings.TrimSuffix(host, ".")), "[]")
	if net.ParseIP(host) != nil || httpguard.TailnetHost(host) {
		return true
	}
	if pn, ok := peers.(PeerNames); ok {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		for _, n := range pn.Names(ctx) {
			if strings.EqualFold(n, host) {
				return true
			}
		}
	}
	return false
}

type actorMode int

const (
	actorPipe actorMode = iota // required
	actorPeer                  // optional: an older front sends none
	actorLink                  // required, and the install is the key the link proved
)

var legacyLogged atomic.Int64 // unix seconds of the last log line

// LegacyFronts counts the requests this host served without a Fyr-Actor from
// a front of an older build (decision 0030 slice 1: still served, and
// counted; `fyr auth status` shows it from slice 2). V0.183: the host's
// count, which fyrd gives its front, not a package global.
func (s *Server) LegacyFronts() int64 { return s.legacyFronts.Load() }

var originRe = regexp.MustCompile(`^https?://[A-Za-z0-9.\-]+(:[0-9]+)?$|^https?://\[[0-9A-Fa-f:.]+\](:[0-9]+)?$`)

// prefixRe is a front's host prefix (decision 0031): /api/h/<install id>.
var prefixRe = regexp.MustCompile(`^/api/h/[A-Za-z0-9_.~-]+$`)

// withActor reads Fyr-Actor, Fyr-Origin and Fyr-Prefix into the request's context. It is
// only used on the pipe and the peer listener, where the channel says which
// install speaks. self is this install's id, which names the front on the
// pipe; legacy counts the fronts served without one (actorPeer; nil: none).
func withActor(mode actorMode, self string, legacy *atomic.Int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p authn.Principal
		if h := r.Header.Get(authn.Header); h != "" {
			var err error
			if p, err = authn.Parse(h); err != nil {
				api.WriteJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "malformed " + authn.Header})
				return
			}
		} else if mode == actorPipe || mode == actorLink {
			api.WriteJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "missing " + authn.Header})
			return
		} else {
			p = authn.Principal{Kind: authn.KindInstall, ID: "legacy", How: authn.HowLegacy}
			if legacy != nil {
				legacy.Add(1)
			}
			if now := time.Now().Unix(); now-legacyLogged.Load() > 600 {
				legacyLogged.Store(now)
				log.Printf("auth (observe): a front without %s was served (%s %s); update that install", authn.Header, r.Method, r.URL.Path)
			}
		}
		if mode == actorPipe {
			p.Front = self
		}
		if mode == actorLink {
			li, _ := r.Context().Value(linkKey{}).(linkInfo)
			// 0030 Amendment 1: over a link the owner's own machine is not a caller, and an
			// install speaks as itself: the id the handshake proved, nothing else.
			if p.Kind == authn.KindLocal || (p.Kind == authn.KindInstall && p.ID != li.install) {
				api.WriteJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "that principal is not taken over a link"})
				return
			}
			p.Front = li.install // the key the link proved, whatever the header says
			if p.Kind == authn.KindDevice && li.gate != nil {
				switch {
				case li.gate.DeviceRevoked(p.ID):
					api.WriteJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "that device was revoked"})
					return
				case li.gate.Enforcing() && (p.How == authn.HowLegacy || !li.gate.DeviceKnown(p.ID)):
					api.WriteJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "no member signed that device in"})
					return
				}
			}
		}
		ctx := authn.With(r.Context(), p)
		if o := r.Header.Get(authn.OriginHeader); o != "" && originRe.MatchString(o) {
			ctx = authn.WithOrigin(ctx, o)
		}
		if p := r.Header.Get(authn.PrefixHeader); p != "" && prefixRe.MatchString(p) {
			ctx = authn.WithPrefix(ctx, p)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
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
		Now: time.Now().UTC(), User: core.Owner, BuildID: s.BuildID, Host: s.hostName(), HostID: s.ID, HostOrder: s.hostOrder(), Machine: machine.Kind(), OS: machine.OS(), HostMarks: s.hostMarks(), AgentMark: s.agentMark(), Update: s.updateState(),
		Threads: threads, Actions: acts, Profiles: profiles, Projects: projects,
		Partial: since > 0, Deploy: s.Engine.DeployState(time.Now()), Stall: s.Engine.StallState(time.Now()),
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
	actor, front := "", ""
	if p, ok := authn.From(r.Context()); ok { // the pipe and the peer listener (decision 0030 3.3)
		actor, front = string(p.Kind)+":"+p.ID, p.Front
	}
	rep := s.Engine.CommandAs(c, actor, front) // #48: provider commands too
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

// projectIcon serves a project's image icon (V0.116): ?size=128 for the
// larger square, else 64. The client's URL carries the version (?v=), so
// the browser may keep it.
func (s *Server) projectIcon(w http.ResponseWriter, r *http.Request) {
	n := 64
	if r.URL.Query().Get("size") == "128" {
		n = 128
	}
	p := s.Engine.ProjectIconPath(r.PathValue("id"), n)
	f, err := os.Open(p)
	if p == "" || err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	if r.URL.Query().Get("v") != "" {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, "icon.png", time.Time{}, f)
}

// projectIconUpload is the owner's upload of a project's icon (V0.116):
// the body is the picture, at most 2 MB.
func (s *Server) projectIconUpload(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(io.LimitReader(r.Body, projicon.MaxBytes+1))
	if err != nil {
		api.WriteJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(b) > projicon.MaxBytes {
		api.WriteJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"ok": false, "error": "the picture is over 2 MB"})
		return
	}
	rep := s.Engine.UploadProjectImage(r.PathValue("id"), b)
	if rep.Status == 0 {
		rep.Status = 200
	}
	api.WriteJSON(w, rep.Status, rep)
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
	// V0.download-assets: ?download=1 saves the file under its name instead of showing it.
	disposition := "inline"
	if r.URL.Query().Get("download") != "" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", disposition+"; filename*=UTF-8''"+url.PathEscape(m.Name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	http.ServeContent(w, r, m.Name, time.Time{}, f)
}

// previewRoot is where the browser reads previews: /api/h/<hostId>/previews/
// when its call named the host (decision 0031), else /api/previews/.
func previewRoot(r *http.Request) string {
	if p := authn.PrefixFrom(r.Context()); p != "" {
		return p + "/previews/"
	}
	return "/api/previews/"
}

// previewCDNs may serve scripts, models, decoders and fonts to a preview
// (V0.47); ajax.googleapis.com hosts model-viewer, www.gstatic.com the Draco
// and Basis decoders three.js and model-viewer load by default.
var previewCDNs = "https://cdnjs.cloudflare.com https://cdn.jsdelivr.net https://unpkg.com https://esm.sh https://ajax.googleapis.com https://www.gstatic.com"

// previewCSP is the policy of every preview response (V0.19, V0.47): a
// sandbox without allow-same-origin, so the page never has Fyr's origin and
// cannot call the API; it may load from its own folder and the CDN list.
// The folder is an absolute prefix: a page without an origin has no 'self'.
// It is the folder as the browser reads it: under the front's host prefix
// when the call came by one (V0.preview-host-prefix).
func previewCSP(r *http.Request, id string) string {
	base := ""
	folder := previewRoot(r) + id + "/ "
	if o, ok := authn.OriginFrom(r.Context()); ok {
		// Decision 0030 3.3: the origin the browser used, from the front, on a
		// channel that proved it is one.
		base = o + folder
	} else if httpguard.ValidHost(r.Host) {
		scheme := "http" // CSP3: an http source also matches https
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		base = scheme + "://" + r.Host + folder
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
		// V0.preview-host-prefix: relative to the page's own URL, so the
		// files are read from the host the page came from, by any front.
		w.Header().Set("Location", url.PathEscape(id)+"/"+(&url.URL{Path: m.Entry}).EscapedPath())
		w.WriteHeader(http.StatusFound)
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
// search answers POST /api/search (V0.ai-search): the threads of this host
// that fit the owner's words. The request's context ends the model call when
// the client gives up.
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Query string   `json:"query"`
		IDs   []string `json:"ids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&q); err != nil {
		api.WriteJSON(w, 400, map[string]any{"error": "bad JSON: " + err.Error()})
		return
	}
	hits, err := s.Engine.SearchThreads(r.Context(), q.Query, q.IDs)
	switch {
	case errors.Is(err, engine.ErrSearchBusy):
		api.WriteJSON(w, 429, map[string]any{"error": err.Error()})
	case err != nil:
		api.WriteJSON(w, 502, map[string]any{"error": err.Error()})
	default:
		api.WriteJSON(w, 200, map[string]any{"hits": hits})
	}
}

func (s *Server) quota(w http.ResponseWriter, r *http.Request) {
	if s.Quota == nil {
		api.WriteJSON(w, 200, []any{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	api.WriteJSON(w, 200, s.Quota(ctx))
}

// load answers GET /api/load (V0.80): this host's load as a list of one,
// so a front joins its hosts' lists.
func (s *Server) load(w http.ResponseWriter, r *http.Request) {
	if s.Load == nil {
		api.WriteJSON(w, 200, []any{})
		return
	}
	l := s.Load.Get()
	l.Host = s.hostName()
	api.WriteJSON(w, 200, []cgroup.Load{l})
}

// boardVisit is the owner opening the Changes board (V0.98 (3)): the host
// stores the time as boardSeenAt and returns the visit before, null at the
// first.
func (s *Server) boardVisit(w http.ResponseWriter, r *http.Request) {
	var seen any
	if prev := s.Engine.BoardVisit(); !prev.IsZero() {
		seen = prev
	}
	api.WriteJSON(w, 200, map[string]any{"seenAt": seen})
}

// focus takes a front's presence beat for one of this host's threads
// (V0.85): {device} raises its CPU weight, {device, off} drops it.
func (s *Server) focus(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Device string `json:"device"`
		Off    bool   `json:"off"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&b); err != nil || b.Device == "" {
		api.WriteJSON(w, 400, map[string]any{"error": "device is required"})
		return
	}
	id := r.PathValue("id")
	if _, ok := s.Engine.Thread(id); !ok {
		api.WriteJSON(w, 404, map[string]any{"error": "no such thread"})
		return
	}
	if s.Focus != nil {
		s.Focus(b.Device, id, !b.Off)
	}
	w.WriteHeader(http.StatusNoContent)
}

// hostMarks is the host's copy of the owner's host marks (V0.171), or nil.
func (s *Server) hostMarks() any {
	if m := s.Engine.HostMarks(); !m.At.IsZero() {
		return m
	}
	return nil
}

// updateState is this host's update state (V0.host-updates), or nil.
func (s *Server) updateState() any {
	if s.UpdateState == nil {
		return nil
	}
	return s.UpdateState()
}

// agentMark is what this host's agent set for its mark (V0.208), or nil.
func (s *Server) agentMark() any {
	if m := s.Engine.AgentMark(); m != (engine.AgentMark{}) {
		return m
	}
	return nil
}

// hostOrder is the host's copy of the owner's host order (V0.132), or nil.
func (s *Server) hostOrder() any {
	if o := s.Engine.HostOrder(); len(o.Names) > 0 {
		return o
	}
	return nil
}
