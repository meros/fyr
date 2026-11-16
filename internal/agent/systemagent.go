package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

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
// core.JobSetup): the job's override, else the default; no provider is the
// host's default provider, no model is that provider's Haiku model. A
// provider or model that cannot run is an error that says why; it never
// falls back to another provider or a bigger model.
func (m *Manager) SystemAgent(job string) (SystemAgent, error) {
	if !core.IsSystemJob(job) {
		return SystemAgent{}, fmt.Errorf("unknown system job %q", job)
	}
	var s core.SystemAgents
	if m.cfg.SystemAgents != nil {
		s = m.cfg.SystemAgents()
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

// SystemPrint runs one system job: a print run of its provider and model in
// the system temp folder, so no project instructions load, with system as
// the whole system prompt (V0.108). The job's error says why it did not run.
func (m *Manager) SystemPrint(ctx context.Context, job, system, prompt string) (string, error) {
	a, err := m.SystemAgent(job)
	if err != nil {
		return "", err
	}
	p := a.Profile
	ad, err := m.adapter(p)
	if err != nil {
		return "", err
	}
	pr, ok := ad.(cli.Printer)
	if !ok {
		return "", fmt.Errorf("the %s CLI cannot run a system job", ad.ID())
	}
	l := cli.Launch{Binary: p.Binary, Dir: os.TempDir(), Auth: authOf(p), Endpoint: p.Endpoint, Env: p.Env, Model: a.Model}
	if p.Key != nil {
		k, kerr := p.Key()
		if (kerr != nil || k == "") && (p.KeyRequired || l.Auth.Kind == cli.APIKey) {
			return "", fmt.Errorf("%s needs a key or a login token: add it in Settings → Providers", name(p))
		}
		l.Key = k
	}
	return pr.Print(ctx, l, system, prompt)
}

// HaikuModel is a provider's Haiku model: one in its list, else the model
// its endpoint names for Haiku, else "".
func HaikuModel(p provider.Profile) string {
	for _, id := range p.Models {
		if strings.Contains(strings.ToLower(id), "haiku") {
			return id
		}
	}
	return p.Endpoint.ModelEnv["ANTHROPIC_DEFAULT_HAIKU_MODEL"]
}
