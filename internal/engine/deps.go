package engine

import (
	"context"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/slots"
)

// The engine's dependencies on the rest of the host (V0.182). The host's
// wiring (cmd/fyrd) sets each group once, as one value, before it serves;
// tests set the fields they need. Every group is embedded in Engine, so its
// fields read as e.X.
//
// The lock rule (V0.147): the engine calls every func and method in these
// groups without e.mu. They take their own locks (the agent manager's, the
// pool's, the job tracker's, the sampler's) and may call back into the
// engine; under e.mu that is the cycle that hung fyrd on 17 Nov (#206).
// TestV182DepsAreCalledWithoutTheEngineLock checks every func field.
//
// A field left nil turns its feature off, as each one says.

// AgentDeps are the agents and what the owner's thread commands check.
type AgentDeps struct {
	// Agents runs agent effects (*agent.Manager).
	Agents Agents
	// Titler makes a thread title from its conversation (V0.108); nil: this
	// host makes none.
	Titler Titler
	// Providers runs the provider commands' effect (3.16); nil: this host
	// has no providers.
	Providers Providers
	// Adopt runs t3.adopt's effect.
	Adopt func(ids []string) (int, error)
	// AttachmentExists checks ids in thread.create and thread.send (V0.18).
	AttachmentExists func(id string) bool
	// ProfileExists checks the target of thread.handover (V0.45); nil
	// accepts any.
	ProfileExists func(id string) bool
	// SystemAgentResolve names the provider and the model a system job runs
	// on now, or why it cannot run (V0.115); nil: Settings shows the
	// setting only.
	SystemAgentResolve func(job string) (profile, model string, err error)
	// SystemAgentProviders lists the providers the setting can pick.
	SystemAgentProviders func() []SystemProvider
}

// OutsideDeps are the effects outside the host and who hears of changes.
type OutsideDeps struct {
	// Effects performs approved async effects (SPEC 5.10), e.g. pr.merge.
	Effects EffectRunner
	// OnPR is called when a thread gets a new PR, so the poller looks now.
	OnPR func(threadID string)
	// OnAttention is called when a thread's attention state changes (push,
	// SPEC 0.5 V0.15). prev is "" for a new thread.
	OnAttention func(d api.Thread, prev core.State)
}

// DeployDeps are the deployer's (V0.83, V0.111, V0.126); nil: this host
// deploys nothing.
type DeployDeps struct {
	// DeployGate checks a checkout before Fyr approves a deploy ask by
	// itself (V0.111): its HEAD, or why the ask waits for the owner. nil:
	// no deploy ask is approved by itself.
	DeployGate func(ctx context.Context, dir string) (commit, why string)
	// DeployBusy: a deploy builds or a new build waits out its probation.
	DeployBusy func() bool
	// DeployNow runs host.deployNow's effect (V0.83): the deployer stops its
	// drain.
	DeployNow func() error
	// UpdateNow runs host.updateNow's effect (V0.126): install the newest
	// release now.
	UpdateNow func() error
}

// SlotDeps are the heavy-job measures and controls beside the pool
// (decision 0029). All optional.
type SlotDeps struct {
	// SlotActive tells when a held slot last ran a heavy job (#174: the
	// job tracker's last heavy command or busy reading).
	SlotActive func(owner string) (time.Time, bool)
	// SlotSignals and SlotGrowth feed decision 0029 S2's memory guard:
	// the host's measures and how much a holder's memory grew.
	SlotSignals func() slots.Signals
	SlotGrowth  func(owner string) uint64
	SlotPolicy  slots.Policy
	// Decision 0029 S3: SlotJob hears the command a slot_acquire names;
	// SlotOver says whether a holder's job is heavier than predicted (its
	// class, live and predicted cost); JobReset forgets a learned class
	// (the owner's Reset).
	SlotJob  func(thread, command string)
	SlotOver func(owner string) (class string, live, predicted float64, over bool)
	JobReset func(class string) bool
	// SlotAIMD adapts the pool's budget at each tick (decision 0029 S4).
	SlotAIMD *slots.AIMD
	// Decision 0029 S5: freeze and thaw a holder's job (its own cgroup, not
	// its CLI).
	SlotFreeze func(owner string) bool
	SlotThaw   func(owner string)
}

// UndeclaredDeps are V0.164's: heavy work nobody declared. Undeclared lists
// the flagged threads (the detector's published view); UndeclaredTold
// records when a thread's agent was told; UndeclaredFreeze and
// UndeclaredThaw move a thread's undeclared work in and out of its frozen
// cgroup; the others are the owner's commands. All optional.
type UndeclaredDeps struct {
	Undeclared         func() []slots.Undeclared
	UndeclaredTold     func(thread string, since, at time.Time)
	UndeclaredFreeze   func(thread string) bool
	UndeclaredThaw     func(thread string)
	UndeclaredDismiss  func(thread string, on bool) bool
	UndeclaredSettings func(args map[string]any) error
	HeavySuggestion    func(id, do, pattern, kind string) error
	HeavyRemove        func(pattern string) bool
	HeavyRefused       func(class string)
}

// LeftoverDeps are V0.176's: leftover processes and temporary files.
// LeftoverSweep looks now (a run ended, a thread settled); the others are
// the owner's commands and the agent's keep_process. All optional.
type LeftoverDeps struct {
	LeftoverSweep   func()
	LeftoverEnd     func(id string) error
	LeftoverKeep    func(id string, d time.Duration) error
	LeftoverDeclare func(thread string, pid int, match string, d time.Duration, why string) error
	TempRemove      func(path string) error
}
