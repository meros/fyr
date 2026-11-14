// fyrd is the Fyr host daemon (v0: one host, one desktop client on
// localhost; SPEC 0.5).
//
//	fyrd [serve] [-listen 127.0.0.1:7777] [-data DIR] [-web DIR]
//	fyrd import-t3 [-list]
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/deploy"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/github"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/push"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/store"
	"github.com/meros/fyr/internal/t3"
	"github.com/meros/fyr/internal/tailnet"
	"github.com/meros/fyr/web"
)

// buildID is set with -ldflags "-X main.buildID=…"; else from VCS info.
var buildID = ""

func BuildID() string {
	if buildID != "" {
		return buildID
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		rev, dirty := "", false
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if rev != "" {
			if len(rev) > 12 {
				rev = rev[:12]
			}
			if dirty {
				exe, _ := os.Executable()
				if st, err := os.Stat(exe); err == nil {
					rev += fmt.Sprintf("-dirty-%d", st.ModTime().Unix())
				}
			}
			return rev
		}
	}
	return fmt.Sprintf("dev-%d", time.Now().Unix())
}

type config struct {
	Listen          string          `json:"listen"`
	PermissionMode  string          `json:"permissionMode"`
	IdleStopMinutes int             `json:"idleStopMinutes"`
	OwnerLogin      string          `json:"ownerLogin"` // Tailscale login allowed over the tailnet (V0.14)
	Profiles        []agent.Profile `json:"profiles"`
}

func home() string { h, _ := os.UserHomeDir(); return h }

func loadConfig(path, t3Settings string) config {
	c := config{Listen: "127.0.0.1:7777", IdleStopMinutes: 10}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &c); err != nil {
			log.Fatalf("config %s: %v", path, err)
		}
	}
	if len(c.Profiles) == 0 {
		if ps, err := t3.Profiles(t3Settings); err == nil {
			c.Profiles = ps
		}
	}
	if len(c.Profiles) == 0 {
		c.Profiles = []agent.Profile{{ID: "claudeAlt", Name: "Claude", Binary: "claude", Color: "#d97757",
			Models: t3.DefaultModels, DefaultModel: t3.DefaultModels[0]}}
	}
	for i := range c.Profiles {
		if c.Profiles[i].PermissionMode == "" {
			c.Profiles[i].PermissionMode = c.PermissionMode
		}
	}
	return c
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("fyrd ")
	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	fl := flag.NewFlagSet(cmd, flag.ExitOnError)
	data := fl.String("data", filepath.Join(home(), ".local", "share", "fyr"), "data directory")
	cfgPath := fl.String("config", filepath.Join(home(), ".config", "fyr", "config.json"), "config file")
	listen := fl.String("listen", "", "listen address (loopback only), default from config or 127.0.0.1:7777")
	webDir := fl.String("web", "", "serve the client from this directory instead of the embedded build (development)")
	ownerLogin := fl.String("owner-login", "", "Tailscale login allowed over the tailnet (default from config ownerLogin)")
	t3Dir := fl.String("t3", filepath.Join(home(), ".t3", "userdata"), "T3 Code user data directory")
	list := fl.Bool("list", false, "import-t3: only list the candidates")
	fl.Parse(args)

	os.MkdirAll(*data, 0o700)
	// A systemd unit has a minimal PATH; agents need the user's tools.
	if u := os.Getenv("USER"); u != "" || home() != "" {
		if u == "" {
			u = filepath.Base(home())
		}
		os.Setenv("PATH", agent.UserPath(os.Getenv("PATH"), home(), u, agent.DirExists))
	}
	cfg := loadConfig(*cfgPath, filepath.Join(*t3Dir, "settings.json"))
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *ownerLogin != "" {
		cfg.OwnerLogin = *ownerLogin
	}
	if cfg.OwnerLogin == "" {
		// V0.14: the Tailscale user of this machine owns it over the tailnet.
		if login, dns, err := tailnet.Self(tailnet.DefaultSocket); err == nil {
			cfg.OwnerLogin = login
			log.Printf("tailnet owner %s on %s", login, dns)
		} else {
			log.Printf("tailnet: %v; serving loopback only", err)
		}
	}
	t3DB := filepath.Join(*t3Dir, "statev2.sqlite")

	st, err := store.Open(filepath.Join(*data, "fyr.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	eng := engine.New(st)
	if err := eng.Load(); err != nil {
		log.Fatal(err)
	}
	adopt := func(ids []string) (int, error) { return adoptT3(eng, t3DB, cfg.Profiles, ids) }

	switch cmd {
	case "import-t3":
		cs, err := t3.Candidates(t3DB, home())
		if err != nil {
			log.Fatal(err)
		}
		var ids []string
		for _, c := range cs {
			if eng.HasT3(c.T3ID) {
				continue
			}
			fmt.Printf("%s  %-14s %-40.40s %s\n", c.UpdatedAt.Local().Format("01-02 15:04"), c.Profile, c.Title, c.Cwd)
			ids = append(ids, c.T3ID)
		}
		if *list {
			fmt.Printf("%d threads can be adopted\n", len(ids))
			return
		}
		n, err := adopt(ids)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("adopted %d threads\n", n)
		return
	case "serve":
	default:
		log.Fatalf("unknown command %q", cmd)
	}

	host, _, err := net.SplitHostPort(cfg.Listen)
	if ip := net.ParseIP(host); err != nil || (host != "localhost" && (ip == nil || !ip.IsLoopback())) {
		log.Fatalf("listen %q: v0 listens on loopback only (decision 0002)", cfg.Listen)
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		log.Fatal(err)
	}
	mcpURL := "http://" + ln.Addr().String() + "/mcp"

	profileByID := map[string]agent.Profile{}
	for _, p := range cfg.Profiles {
		profileByID[p.ID] = p
	}
	att := &attach.Store{Dir: filepath.Join(*data, "attachments")}
	eng.AttachmentExists = func(id string) bool { _, err := att.Get(id); return err == nil }
	var mgr *agent.Manager
	mcpSrv := mcp.New(func(tok string) (string, bool) { return mgr.ThreadForToken(tok) })
	eng.Tools(mcpSrv)
	mgr = agent.New(eng, agent.Config{
		Profile: func(id string) agent.Profile {
			if p, ok := profileByID[id]; ok {
				return p
			}
			if id == "claude" {
				return cfg.Profiles[0]
			}
			return cfg.Profiles[0]
		},
		IdleStop:    time.Duration(cfg.IdleStopMinutes) * time.Minute,
		MCPURL:      mcpURL,
		LogDir:      filepath.Join(*data, "logs"),
		Attachments: att.Get,
	})
	eng.Agents = mgr
	eng.Adopt = adopt

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dep := &deploy.Deployer{Data: *data, DB: st, Host: eng, Quit: stop}
	eng.Effects = effects{dep: dep}

	poller := &github.Poller{Gh: github.Gh, Src: prSource{eng}}
	eng.OnPR = poller.Kick
	go poller.Run(ctx)
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				eng.Tick()
			}
		}
	}()

	// Web Push (V0.15). The VAPID subject goes to the push services, so it
	// carries no personal data.
	pusher := &push.Pusher{DB: st, Subject: "mailto:push@fyr.invalid"}
	if err := pusher.Init(); err != nil {
		log.Printf("push: %v", err)
	}
	eng.OnAttention = func(d engine.DTO, prev core.State) {
		st := d.Attention.State
		if st != core.Asking && st != core.Failed {
			return
		}
		meta, detail := attentionDetail(d)
		go pusher.Notify(core.Owner, push.PayloadFor(d.ID, d.Title, string(st), meta, detail))
	}

	var webFS fs.FS = web.Dist()
	if *webDir != "" {
		webFS = os.DirFS(*webDir)
	}
	srv := &server.Server{Engine: eng, MCP: mcpSrv, Web: webFS, BuildID: BuildID(), Profiles: cfg.Profiles, OwnerLogin: cfg.OwnerLogin, Push: pusher, Attach: att,
		T3Candidates: func() (any, error) {
			cs, err := t3.Candidates(t3DB, home())
			if err != nil {
				return nil, err
			}
			out := []t3.Candidate{}
			for _, c := range cs {
				if !eng.HasT3(c.T3ID) {
					out = append(out, c)
				}
			}
			return out, nil
		}}
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	log.Printf("build %s listening on http://%s (%d threads, %d profiles)", BuildID(), ln.Addr(), len(eng.Snapshot()), len(cfg.Profiles))
	deploy.MarkReady()
	dep.ReportPending()

	<-ctx.Done()
	log.Printf("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hs.Shutdown(sctx)
	mgr.StopAll()
	os.Exit(dep.ExitCode())
}

// effects runs approved async effects (SPEC 5.10).
type effects struct{ dep *deploy.Deployer }

func (x effects) Run(ctx context.Context, threadID string, req core.EffectReq) (string, error) {
	switch req.Action {
	case "pr.merge":
		url, _ := req.Args["pr"].(string)
		method, _ := req.Args["method"].(string)
		return github.Merge(ctx, github.Gh, url, method)
	case "fyr.deploy":
		dir, _ := req.Args["dir"].(string)
		return x.dep.Deploy(ctx, threadID, req, dir)
	}
	return "", fmt.Errorf("unknown effect %q", req.Action)
}

type prSource struct{ e *engine.Engine }

func (s prSource) Snapshot() []github.PRThread {
	var out []github.PRThread
	for _, t := range s.e.Snapshot() {
		if t.PR != nil && t.PR.State == "OPEN" && t.ArchivedAt == nil {
			out = append(out, github.PRThread{ID: t.ID, URL: t.PR.URL})
		}
	}
	return out
}

func (s prSource) Observe(id string, pr core.PR) {
	s.e.Observe(id, func(t *core.Thread, now time.Time) {
		if t.PR != nil && t.PR.URL == pr.URL {
			core.PRObserved(t, pr, now)
		}
	})
}

func adoptT3(eng *engine.Engine, db string, profiles []agent.Profile, ids []string) (int, error) {
	cs, err := t3.Candidates(db, home())
	if err != nil {
		return 0, err
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	known := map[string]bool{}
	for _, p := range profiles {
		known[p.ID] = true
	}
	n := 0
	for _, c := range cs {
		if !want[c.T3ID] || eng.HasT3(c.T3ID) {
			continue
		}
		prof := c.Profile
		if !known[prof] {
			prof = profiles[0].ID
		}
		t := &core.Thread{ID: engine.NewID("t_"), Title: c.Title, Cwd: c.Cwd, Project: core.ProjectName(c.Cwd),
			Owner: core.Owner, Visibility: "private", Profile: prof, Model: c.Model, SessionID: c.SessionID,
			CreatedAt: c.UpdatedAt, UpdatedAt: c.UpdatedAt, ActivityAt: c.UpdatedAt, Source: "t3", T3ID: c.T3ID}
		if repo, num, ok := agent.ParsePR(c.PRURL); ok {
			t.PR = &core.PR{URL: c.PRURL, Repo: repo, Number: num, State: "OPEN", Checks: "pending"}
		}
		v := &core.View{User: core.Owner, Entity: t.ID, SeenAt: c.UpdatedAt}
		if c.SnoozedUntil != "" {
			if until, err := time.Parse(time.RFC3339Nano, c.SnoozedUntil); err == nil && until.After(time.Now()) {
				now := time.Now()
				v.SnoozeUntil, v.SnoozedAt = &until, &now
			}
		}
		hist, at, err := t3.History(home(), c.SessionID, 400)
		if err != nil {
			log.Printf("adopt %s: history: %v", c.T3ID, err)
		}
		if len(hist) > 0 {
			if s := lastSummary(hist); s != "" {
				t.Summary = s
			}
		}
		if err := eng.Insert(t, v, hist, at); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func lastSummary(h []agent.Ev) string {
	for i := len(h) - 1; i >= 0; i-- {
		m, _ := h[i].Data.(map[string]any)
		if m["type"] != "assistant" {
			continue
		}
		msg, _ := m["message"].(map[string]any)
		bs, _ := msg["content"].([]any)
		for _, x := range bs {
			if b, _ := x.(map[string]any); b["type"] == "text" {
				if s, _ := b["text"].(string); s != "" {
					r := []rune(s)
					if len(r) > 160 {
						r = r[:159]
					}
					return string(r)
				}
			}
		}
	}
	return ""
}

// attentionDetail is the meta word and the text of a push for asking or
// failed, matching the inbox row (design/mobile rule 12).
func attentionDetail(d engine.DTO) (meta, detail string) {
	if d.Failure != nil && d.Attention.State == core.Failed {
		return "Failed", d.Failure.Message
	}
	if len(d.Decisions) > 0 {
		x := d.Decisions[0]
		switch {
		case x.Kind == "ask" && len(x.Questions) > 0:
			meta = "1 question"
			if n := len(x.Questions); n > 1 {
				meta = fmt.Sprintf("%d questions", n)
			}
			return meta, x.Questions[0].Question
		case x.Kind == "effect" && x.Effect != nil:
			return "Fyr action", "approve " + x.Effect.Action
		case x.Kind == "approval":
			return "Approval", "allow " + x.Tool
		}
	}
	return "", d.Attention.Reason
}
