package main

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/projicon"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/quota"
	"github.com/meros/fyr/internal/usage"
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
	w.quotaList = quotaList
}

// wireUsage makes the token meter on the host's store (V0.usage-ledger,
// decision 0041).
//
// Needs from earlier steps: quotaList (wireQuota).
// Makes: meter.
// Order: before wireAgents, which gives it to the agent manager.
func (w *wiring) wireUsage() {
	m := usage.New(w.h.st, BuildID(), nil)
	if err := m.Load(); err != nil {
		log.Printf("usage: load: %v", err)
	}
	// The engine keeps each thread's total for its DTO; the meter tells it
	// after each report, before the agent reports the run's end.
	put := func(thread string) {
		if c, ok := m.ThreadTotal(thread); ok {
			w.eng.SetThreadUsage(thread, apiUsage(c))
		}
	}
	for _, t := range m.Threads() {
		put(t)
	}
	m.OnRecord = put
	w.meter = m
}

// accountOf names a provider's account for the usage ledger: the quota
// identity (V0.44: two providers on one login are one account), hashed so
// no login folder or key source reaches a client, and how it is billed.
func (w *wiring) accountOf(profile string) (account, billing string) {
	for _, q := range w.quotaList() {
		if q.ID != profile {
			continue
		}
		id := q.Identity
		if id == "" {
			id = quota.Identity(quota.Resolve(q, home()))
		}
		sum := sha256.Sum256([]byte(id))
		account = "a_" + hex.EncodeToString(sum[:6])
		break
	}
	if p, ok := w.h.profile(profile); ok {
		billing = billingOf(p)
	}
	return account, billing
}

// billingOf: a CLI login is a subscription; a key is billed per token, by
// the provider's API or by another endpoint.
func billingOf(p provider.Profile) string {
	switch p.Auth.Kind {
	case cli.CLILogin:
		return "subscription"
	case cli.APIKey:
		if p.Endpoint.BaseURL != "" && !strings.Contains(p.Endpoint.BaseURL, "anthropic.com") {
			return "endpoint"
		}
		return "api-key"
	}
	return "none"
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
		ThreadSource:    eng.ThreadSource,  // V0.usage-jobs
		ProjectPrompt:   eng.ProjectPrompt, // V0.project-playbook
		// V0.tool-reload: a process from an older build that has not read
		// this build's tools ends at its next idle point.
		Tools: mcpSrv.Hash, ToolsCurrent: mcpSrv.Current, Build: BuildID(),
		Meter: w.meter, Account: w.accountOf, // V0.usage-ledger
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

// apiUsage is a thread's total as its DTO carries it (V0.usage-ledger).
func apiUsage(c cli.TokenCount) api.Usage {
	return api.Usage{Input: c.Input, Output: c.Output, CacheRead: c.CacheRead, CacheWrite5m: c.CacheWrite5m,
		CacheWrite1h: c.CacheWrite1h, Thinking: c.Thinking, Total: usage.Total(c)}
}
