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

	JobTitle    = "title"    // V0.108: Regenerate title
	JobSetup    = "setup"    // the project setup run: icon, colour and name
	JobQuestion = "question" // V0.210: does a turn's end wait on the owner
	JobSearch   = "search"   // V0.ai-search: which threads fit the owner's words
	JobApproval = string(DecisionApproval) // V0.tool-approval-judge: allow or escalate a tool approval

	// ProfileSystemDefault is a sentinel profile value on a thread-defaulting
	// job (JobApproval) that makes it follow SystemAgents.Default instead of
	// the thread's own provider.
	ProfileSystemDefault = "$default"
)

// SystemJob is one kind of call Fyr starts itself.
type SystemJob struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	DefaultThread bool   `json:"defaultThread,omitempty"`
}

// SystemJobs are the jobs in the order Settings lists them.
var SystemJobs = []SystemJob{
	{ID: JobTitle, Name: "Thread titles"},
	{ID: JobSetup, Name: "Project setup"},
	{ID: JobQuestion, Name: "Questions in text"},
	{ID: JobSearch, Name: "Thread search"},
	{ID: JobApproval, Name: "Tool approvals", DefaultThread: true},
}

// IsSystemJob says whether Fyr knows the job.
func IsSystemJob(id string) bool {
	return slices.ContainsFunc(SystemJobs, func(j SystemJob) bool { return j.ID == id })
}

// JobUsesThread reports whether job defaults to running on each thread's own
// provider when no override is set.
func JobUsesThread(id string) bool {
	for _, j := range SystemJobs {
		if j.ID == id {
			return j.DefaultThread
		}
	}
	return false
}

// SystemAgentChoice is a provider (a profile id; "" is the host's default
// provider) and a model ("" is that provider's fast model).
type SystemAgentChoice struct {
	Profile string `json:"profile,omitempty"`
	Model   string `json:"model,omitempty"`
}

// SystemAgents is the setting: the default and the jobs that override it.
type SystemAgents struct {
	Default SystemAgentChoice            `json:"default"`
	Jobs    map[string]SystemAgentChoice `json:"jobs,omitempty"`
}

// UsesThread reports whether job currently runs on each thread's own provider
// (a DefaultThread job with no override).
func (s SystemAgents) UsesThread(job string) bool {
	if !JobUsesThread(job) {
		return false
	}
	o, ok := s.Jobs[job]
	return !ok || o == (SystemAgentChoice{})
}

// For is the choice of a job: its override, else the default. An override
// that names a provider is taken whole (its model, or that provider's
// fast model), since the default's model may not be on that provider; one that
// names only a model runs it on the default's provider. For a DefaultThread
// job with no override, For returns an empty SystemAgentChoice (check
// UsesThread first); ProfileSystemDefault resolves to s.Default.
func (s SystemAgents) For(job string) SystemAgentChoice {
	o, ok := s.Jobs[job]
	if JobUsesThread(job) {
		switch {
		case !ok || o == (SystemAgentChoice{}):
			return SystemAgentChoice{}
		case o.Profile == ProfileSystemDefault:
			if o.Model != "" {
				return SystemAgentChoice{Profile: s.Default.Profile, Model: o.Model}
			}
			return s.Default
		case o.Profile != "":
			return o
		default:
			return SystemAgentChoice{Model: o.Model}
		}
	}
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
