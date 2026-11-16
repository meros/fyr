package core

import (
	"slices"
	"strings"
	"time"
)

// System agents (V0.115): the model calls Fyr itself starts, not the owner's
// threads: a thread title (V0.108) and a project setup. The owner picks the
// provider and the model they run on, for all jobs and per job, on each host;
// the setting is the owner's alone (KindSystemAgentsSet, a host command with
// a receipt). No agent tool sends it.
const (
	KindSystemAgentsSet = "host.systemAgents"
	// EffectSystemAgentsSet stores the owner's setting on the host.
	EffectSystemAgentsSet EffectKind = "systemAgents.set"

	JobTitle = "title" // V0.108: Regenerate title
	JobSetup = "setup" // the project setup run: icon, colour and name
)

// SystemJob is one kind of call Fyr starts itself.
type SystemJob struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SystemJobs are the jobs in the order Settings lists them.
var SystemJobs = []SystemJob{{JobTitle, "Thread titles"}, {JobSetup, "Project setup"}}

// IsSystemJob says whether Fyr knows the job.
func IsSystemJob(id string) bool {
	return slices.ContainsFunc(SystemJobs, func(j SystemJob) bool { return j.ID == id })
}

// SystemAgentChoice is a provider (a profile id; "" is the host's default
// provider) and a model ("" is that provider's Haiku model).
type SystemAgentChoice struct {
	Profile string `json:"profile,omitempty"`
	Model   string `json:"model,omitempty"`
}

// SystemAgents is the setting: the default and the jobs that override it.
type SystemAgents struct {
	Default SystemAgentChoice            `json:"default"`
	Jobs    map[string]SystemAgentChoice `json:"jobs,omitempty"`
}

// For is the choice of a job: its override, else the default. An override
// that names a provider is taken whole (its model, or that provider's
// Haiku), since the default's model may not be on that provider; one that
// names only a model runs it on the default's provider.
func (s SystemAgents) For(job string) SystemAgentChoice {
	o, ok := s.Jobs[job]
	switch {
	case !ok || o == (SystemAgentChoice{}):
		return s.Default
	case o.Profile != "":
		return o
	default:
		return SystemAgentChoice{Profile: s.Default.Profile, Model: o.Model}
	}
}

func init() {
	RegisterKind(&InputKind{Name: KindSystemAgentsSet, On: RecHost, From: ownerOnly, Await: true, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
		a := mapArgs(in)
		job := strings.TrimSpace(a.Str("job"))
		if job != "" && !IsSystemJob(job) {
			return Result{}, reject(RejectBadArgs, "unknown system job %q", job)
		}
		clr, _ := a["clear"].(bool)
		args := map[string]any{"job": job, "clear": clr,
			"profile": strings.TrimSpace(a.Str("profile")), "model": strings.TrimSpace(a.Str("model"))}
		return Result{Effects: []Effect{{Kind: EffectSystemAgentsSet, Request: &EffectReq{Action: KindSystemAgentsSet, Args: args}}}}, nil
	}})
}
