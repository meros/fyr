package agent

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/provider"
)

// SystemAgent is where a system job runs (V0.115): the host, the provider
// and the model. Every call Fyr starts itself (a title, a project setup)
// gets it from SystemAgent and runs it with SystemPrint; none names a model.
type SystemAgent struct {
	Host    string
	Profile provider.Profile
	Model   string
}

// SystemAgent reads the owner's setting for a job (core.JobTitle,
// core.JobSetup, core.JobQuestion, core.JobSearch, core.JobApproval): the
// job's override, else the default; no provider is the host's default
// provider, no model is that provider's fast model. For a thread-defaulting
// job (core.JobApproval) with no override, it returns an empty SystemAgent
// (use ResolveApprovalJudge with the thread's provider). A provider or model
// that cannot run is an error that says why; it never falls back to another
// provider or a bigger model.
func (m *Manager) SystemAgent(job string) (SystemAgent, error) {
	if !core.IsSystemJob(job) {
		return SystemAgent{}, fmt.Errorf("unknown system job %q", job)
	}
	var s core.SystemAgents
	if m.cfg.SystemAgents != nil {
		s = m.cfg.SystemAgents()
	}
	if s.UsesThread(job) {
		return SystemAgent{}, nil
	}
	c := s.For(job)
	if c.Profile != "" && m.cfg.ProfileExists != nil && !m.cfg.ProfileExists(c.Profile) {
		return SystemAgent{}, fmt.Errorf("the system agents' provider %q is not on this host: pick one in Settings → System agents", c.Profile)
	}
	p := m.cfg.Profile(c.Profile)
	if p.Unavailable != "" {
		return SystemAgent{}, fmt.Errorf("%s cannot run: %s", name(p), p.Unavailable)
	}
	model := c.Model
	if model == "" {
		if model = HaikuModel(p); model == "" {
			return SystemAgent{}, fmt.Errorf("%s has no Haiku model: pick a model in Settings → System agents", name(p))
		}
	}
	return SystemAgent{Host: m.cfg.HostName, Profile: p, Model: model}, nil
}

// ResolveApprovalJudge resolves the provider and model for core.JobApproval
// given a thread's own profile and model (V0.tool-approval-judge). When the
// owner has not overridden JobApproval, it uses the thread's own provider with
// its fast model (Haiku on Claude, Gemini 4 Hydrogen / Flash on Antigravity),
// falling back to the provider's DefaultModel or the thread's model for custom
// endpoints that have no dedicated fast model.
func (m *Manager) ResolveApprovalJudge(threadProfileID, threadModel string) (SystemAgent, error) {
	var s core.SystemAgents
	if m.cfg.SystemAgents != nil {
		s = m.cfg.SystemAgents()
	}
	if !s.UsesThread(core.JobApproval) {
		return m.SystemAgent(core.JobApproval)
	}
	p := m.cfg.Profile(threadProfileID)
	if p.Unavailable != "" {
		return SystemAgent{}, fmt.Errorf("%s cannot run: %s", name(p), p.Unavailable)
	}
	model := HaikuModel(p)
	if model == "" {
		model = p.DefaultModel
	}
	if model == "" {
		model = threadModel
	}
	if model == "" {
		return SystemAgent{}, fmt.Errorf("%s has no model to run the approval judge", name(p))
	}
	return SystemAgent{Host: m.cfg.HostName, Profile: p, Model: model}, nil
}

// SystemPrint runs one system job: a print run of its provider and model in
// the system temp folder, so no project instructions load, with system as
// the whole system prompt (V0.108). The job's error says why it did not run.
func (m *Manager) SystemPrint(ctx context.Context, job, system, prompt string) (string, error) {
	a, err := m.SystemAgent(job)
	if err != nil {
		return "", err
	}
	return m.printOn(ctx, a, system, prompt)
}

// printOn runs a one-shot Print call on a resolved SystemAgent, using low
// reasoning effort when the model advertises effort levels (e.g. Gemini 4
// Hydrogen defaults to high thinking).
func (m *Manager) printOn(ctx context.Context, a SystemAgent, system, prompt string) (string, error) {
	p := a.Profile
	ad, err := m.adapter(p)
	if err != nil {
		return "", err
	}
	pr, ok := ad.(cli.Printer)
	if !ok {
		return "", fmt.Errorf("the %s CLI cannot run a system job", ad.ID())
	}
	caps := p.ModelCaps
	if len(caps) == 0 {
		caps = ad.Caps().Models
	}
	l := cli.Launch{
		Binary:   p.Binary,
		Dir:      os.TempDir(),
		Auth:     authOf(p),
		Endpoint: p.Endpoint,
		Env:      p.Env,
		Model:    a.Model,
		Effort:   lowEffort(caps, a.Model),
	}
	if p.Key != nil {
		k, kerr := p.Key()
		if (kerr != nil || k == "") && (p.KeyRequired || l.Auth.Kind == cli.APIKey) {
			return "", fmt.Errorf("%s needs a key or a login token: add it in Settings → Providers", name(p))
		}
		l.Key = k
	}
	return pr.Print(ctx, l, system, prompt)
}

// lowEffort returns "low" when model explicitly declares "low" among its
// supported effort levels, and "" otherwise (e.g. Haiku or custom models).
func lowEffort(caps []cli.Model, model string) string {
	for _, m := range caps {
		if m.ID == model && slices.Contains(m.Efforts, "low") {
			return "low"
		}
	}
	return ""
}

// HaikuModel is a provider's default fast/small model for system jobs:
// Haiku on Claude (V0.117), Flash on Antigravity, delegated to the adapter's
// cli.Defaults.FastModel.
func HaikuModel(p provider.Profile) string {
	seen := map[string]bool{}
	var ms []cli.Model
	for _, id := range p.Models {
		if id != "" && !seen[id] {
			seen[id] = true
			ms = append(ms, cli.Model{ID: id})
		}
	}
	for _, m := range p.ModelCaps {
		if m.ID != "" && !seen[m.ID] {
			seen[m.ID] = true
			ms = append(ms, m)
		}
	}
	ad := provider.AdapterFor(p.CLI)
	if d, ok := ad.(cli.Defaults); ok {
		isEndpoint := p.Endpoint.BaseURL != "" || p.Auth.Kind == cli.APIKey
		return d.FastModel(ms, p.Endpoint.ModelEnv, isEndpoint)
	}
	return ""
}
