package engine

import (
	"encoding/json"
	"fmt"
	"log"
	"maps"

	"github.com/meros/fyr/internal/core"
)

// systemAgentsKey is the host kv key of the system agents setting (V0.115):
// a JSON core.SystemAgents; absent is Haiku on the host's default provider.
const systemAgentsKey = "systemAgents"

// SystemAgentsView is GET /api/systemAgents and the reply of
// host.systemAgents: the setting and what each job runs now.
type SystemAgentsView struct {
	Default core.SystemAgentChoice `json:"default"`
	Jobs    []SystemJobView        `json:"jobs"`
	// Providers are the host's providers the setting can pick.
	Providers []SystemProvider `json:"providers"`
}

// SystemProvider is a provider of the host as the setting offers it: its
// models, its Haiku model ("" when it has none) and why it cannot run.
type SystemProvider struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Models      []string `json:"models"`
	Haiku       string   `json:"haiku,omitempty"`
	Unavailable string   `json:"unavailable,omitempty"`
}

// SystemJobView is one job: its override, and the provider and model it
// runs on, or why it cannot run.
type SystemJobView struct {
	core.SystemJob
	Override bool                   `json:"override,omitempty"`
	Choice   core.SystemAgentChoice `json:"choice"`
	Profile  string                 `json:"profile,omitempty"`
	Model    string                 `json:"model,omitempty"`
	Error    string                 `json:"error,omitempty"`
}

// loadSystemAgents reads the setting. Call it with e.mu held.
func (e *Engine) loadSystemAgents() {
	e.systemAgents = core.SystemAgents{}
	raw, _ := e.st.GetKV(systemAgentsKey)
	if raw == "" {
		return
	}
	if err := json.Unmarshal([]byte(raw), &e.systemAgents); err != nil {
		log.Printf("system agents setting: %v", err)
		e.systemAgents = core.SystemAgents{}
	}
}

// SystemAgents is the setting.
func (e *Engine) SystemAgents() core.SystemAgents {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.systemAgents
	s.Jobs = maps.Clone(s.Jobs)
	return s
}

// SystemAgentsView is the setting with what each job resolves to.
func (e *Engine) SystemAgentsView() SystemAgentsView {
	s := e.SystemAgents()
	v := SystemAgentsView{Default: s.Default, Jobs: []SystemJobView{}, Providers: []SystemProvider{}}
	if e.SystemAgentProviders != nil {
		v.Providers = append(v.Providers, e.SystemAgentProviders()...)
	}
	for _, j := range core.SystemJobs {
		o, ok := s.Jobs[j.ID]
		jv := SystemJobView{SystemJob: j, Override: ok && o != (core.SystemAgentChoice{}), Choice: o}
		if e.SystemAgentResolve != nil && !s.UsesThread(j.ID) {
			p, m, err := e.SystemAgentResolve(j.ID)
			jv.Profile, jv.Model = p, m
			if err != nil {
				jv.Error = err.Error()
			}
		}
		v.Jobs = append(v.Jobs, jv)
	}
	return v
}

// setSystemAgents stores the owner's change: the default (job "") or a
// job's override, or clears a job's override.
func (e *Engine) setSystemAgents(job string, c core.SystemAgentChoice, drop bool) error {
	if c.Profile == core.ProfileSystemDefault {
		if !core.JobUsesThread(job) {
			return fmt.Errorf("provider %q is only valid for thread-defaulting jobs", c.Profile)
		}
	} else if c.Profile != "" && e.ProfileExists != nil && !e.ProfileExists(c.Profile) {
		return fmt.Errorf("provider %q is not on this host", c.Profile)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	next := e.systemAgents
	next.Jobs = maps.Clone(next.Jobs)
	switch {
	case job == "":
		next.Default = c
	case drop || c == (core.SystemAgentChoice{}):
		delete(next.Jobs, job)
	default:
		if next.Jobs == nil {
			next.Jobs = map[string]core.SystemAgentChoice{}
		}
		next.Jobs[job] = c
	}
	if len(next.Jobs) == 0 {
		next.Jobs = nil
	}
	v := ""
	if next.Default != (core.SystemAgentChoice{}) || next.Jobs != nil {
		b, _ := json.Marshal(next)
		v = string(b)
	}
	if err := e.st.SetKV(systemAgentsKey, v); err != nil {
		return err
	}
	e.systemAgents = next
	return nil
}
