package main

import (
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/projicon"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/quota"
)

// wireQuota reads the quota profiles of the registry and makes the quota service.
//
// Needs from earlier steps: nothing.
// Makes: quotas.
// Order: first; wireAgents reads w.quotas.
func (w *wiring) wireQuota() {
	cfg := w.cfg
	data := w.data
	eng := w.eng
	reg := w.reg
	// V0.44: provider quota, read with the logins on this host.
	quotaList := func() []quota.Profile {
		if reg.Empty() {
			return quotaProfiles(cfg.Profiles)
		}
		return reg.QuotaProfiles()
	}
	quotas := quota.New(nil, home())
	quotas.Client = &http.Client{Timeout: quotaHTTPTimeout}
	quotas.Logf = log.Printf
	// V0.122: the data and the rate-limit wait survive a restart, so frequent deploys never mean frequent reads.
	quotas.StatePath = filepath.Join(data, "quota-state.json")
	quotas.OnUpdate = eng.SyncQuotaResets
	quotas.SetProfiles(quotaList())
	reg.OnChange = func() { quotas.SetProfiles(quotaList()) }
	w.quotas = quotas
}

// wireAgents makes the agent manager and the MCP server.
//
// Needs from earlier steps: quotas.
// Makes: mcpSrv, mgr.
func (w *wiring) wireAgents() {
	att := w.att
	cfg := w.cfg
	data := w.data
	eng := w.eng
	h := w.h
	hostName := w.hostName
	ln := w.ln
	quotas := w.quotas
	rn := w.rn
	tree := w.tree
	var mgr *agent.Manager
	mcpSrv := mcp.New(func(tok string) (string, bool) { return mgr.ThreadForToken(tok) })
	eng.Tools(mcpSrv)
	mgr = agent.New(eng, agent.Config{
		Profile: func(id string) provider.Profile {
			if p, ok := h.profile(id); ok {
				return p
			}
			return h.profiles()[0]
		},
		IdleStop: time.Duration(cfg.IdleStopMinutes) * time.Minute, MCPURL: "http://" + ln.Addr().String() + "/mcp",
		LogDir: filepath.Join(data, "logs"), RunDir: filepath.Join(data, "run"), Attachments: att.Get, OnRateLimit: quotas.Observe, OnCommands: eng.SetCommands, HandoverDir: filepath.Join(data, "handover"),
		Cgroups: tree, Runner: rn.client(),
		// V0.115: the system agents run on this host, on the owner's setting.
		HostName: hostName, ProfileExists: func(id string) bool { _, ok := h.profile(id); return ok }, SystemAgents: eng.SystemAgents,
		// V0.tool-approval-judge: project/thread/owner context for auto-judging tool approvals.
		ApprovalContext: eng.ApprovalContext,
		// V0.tool-reload: a process from an older build that has not read
		// this build's tools ends at its next idle point.
		Tools: mcpSrv.Hash, ToolsCurrent: mcpSrv.Current, Build: BuildID(),
	})
	eng.AgentDeps = engine.AgentDeps{
		Agents: mgr,
		AgentInspect: func() map[string]engine.AgentState { // V0.thread-invariants
			raw := mgr.Inspect()
			out := make(map[string]engine.AgentState, len(raw))
			for id, s := range raw {
				out[id] = engine.AgentState{Live: s.Live, Starting: s.Starting, TurnLive: s.TurnLive, Asks: s.Asks, Outbox: s.Outbox, BG: s.BG, LastOut: s.LastOut, OpenTools: s.OpenTools, Judging: s.Judging}
			}
			return out
		},
		Titler:           mgr.Title,         // V0.108
		TextJudge:        mgr.JudgeText,     // V0.210
		Searcher:         mgr.SearchThreads, // V0.ai-search
		Providers:        h.reg,             // #48: provider commands go down the engine's path
		AttachmentExists: func(id string) bool { _, err := att.Get(id); return err == nil },
		ProfileExists:    func(id string) bool { _, ok := h.profile(id); return ok },
		SystemAgentResolve: func(job string) (string, string, error) { // V0.115: Settings shows what each job runs
			a, err := mgr.SystemAgent(job)
			return a.Profile.ID, a.Model, err
		},
		SystemAgentProviders: func() []engine.SystemProvider {
			var out []engine.SystemProvider
			for _, p := range h.profiles() {
				out = append(out, engine.SystemProvider{ID: p.ID, Name: p.Name, Models: p.Models, Haiku: agent.HaikuModel(p), Unavailable: p.Unavailable})
			}
			return out
		},
		QuotaReset:    quotas.ResetOf,
		QuotaSnapshot: quotas.Snapshot,
	}
	w.mcpSrv = mcpSrv
	w.mgr = mgr
}

// wireProjects sets up project icons and the delayed setup runs.
//
// Needs from earlier steps: mgr.
// Makes: nothing on w (it wires what it made into eng, mgr and the others).
func (w *wiring) wireProjects() {
	data := w.data
	eng := w.eng
	mgr := w.mgr
	rn := w.rn
	// V0.116: project setup. The icons live with the host's data; a new
	// project with a folder gets a setup run, and each project from before
	// gets one once, a minute after start and 30 s apart. The run's provider
	// and model are the owner's System agents setting (SystemAgentResolve).
	eng.Icons = &projicon.Store{Dir: filepath.Join(data, "project-icons")}
	eng.AutoSetup = true
	if rn != nil {
		mgr.Adopt(rn.procs) // V0.90: before anything sends to a thread
	}
}
