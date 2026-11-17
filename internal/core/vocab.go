package core

import "strings"

// The vocabularies of the thread model (#37). Each is a typed string whose
// constants carry the wire values; tool schemas, error messages and code use
// the constants, never the raw strings. A test greps the rest of the tree.

// Phase is how far a change got (SPEC 0.5 V0.59). Working, review, landed
// and live are in order; abandoned and reverted end a change.
type Phase string

const (
	PhaseWorking   Phase = "working"
	PhaseReview    Phase = "review"
	PhaseLanded    Phase = "landed"
	PhaseLive      Phase = "live"
	PhaseAbandoned Phase = "abandoned"
	PhaseReverted  Phase = "reverted"
)

// phaseOrder are the phases that progress, in order.
var phaseOrder = []Phase{PhaseWorking, PhaseReview, PhaseLanded, PhaseLive}

// phaseHelp explains a phase where its name alone does not.
var phaseHelp = map[Phase]string{
	PhaseReview: "waits for a review or an approval",
	PhaseLanded: "the source of truth has it: merged, applied, ran, published",
	PhaseLive:   "it runs where it should and is healthy",
}

// Phases are all phases: the four in order, then the two ends.
func Phases() []Phase { return append(append([]Phase{}, phaseOrder...), PhaseAbandoned, PhaseReverted) }

// PhasesAfter are the phases in order after p, ends excluded.
func PhasesAfter(p Phase) []Phase {
	r, ok := p.rank()
	if !ok {
		return nil
	}
	return append([]Phase{}, phaseOrder[r+1:]...)
}

// PhaseGuide lists every phase with what it means, for a tool description.
func PhaseGuide() string {
	var parts []string
	for _, p := range Phases() {
		s := string(p)
		if h := phaseHelp[p]; h != "" {
			s += " (" + h + ")"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

func (p Phase) rank() (int, bool) {
	for i, x := range phaseOrder {
		if x == p {
			return i, true
		}
	}
	return 0, false
}

// Valid reports a known phase.
func (p Phase) Valid() bool { _, ok := p.rank(); return ok || p.Ended() }

// Ended reports a phase that stops a change.
func (p Phase) Ended() bool { return p == PhaseAbandoned || p == PhaseReverted }

// InFlight reports a phase before landed: the change can still fail, block
// and take new work.
func (p Phase) InFlight() bool { return p == PhaseWorking || p == PhaseReview }

// Reached reports that p has got at least as far as want.
func (p Phase) Reached(want Phase) bool {
	if p == want {
		return true
	}
	a, ok1 := p.rank()
	b, ok2 := want.rank()
	return ok1 && ok2 && a >= b
}

// CondType names a condition of a change. Fyr observes the constants below;
// an agent may report any other type (for example "apply").
type CondType string

const (
	CondChecks   CondType = "checks"
	CondReview   CondType = "review"
	CondConflict CondType = "conflict"
	CondDeploy   CondType = "deploy" // the production deployment (V0.66)
	CondPushed   CondType = "pushed"
	CondClean    CondType = "clean"
	CondApproval CondType = "approval"
	CondMerge    CondType = "mergeable" // GitHub refused a merge Fyr ran (V0.136)

	condActionPrefix = "action:"
	condDeployPrefix = "deploy:"
)

// CondAction is the condition of a script action on a change (V0.60).
func CondAction(id string) CondType { return CondType(condActionPrefix + id) }

// CondDeployTo is the condition of a deployment to a non-production
// environment (V0.66).
func CondDeployTo(env string) CondType { return CondType(condDeployPrefix + env) }

// CondStatus is the state of one condition.
type CondStatus string

const (
	CondOK      CondStatus = "ok"
	CondFail    CondStatus = "fail"
	CondPending CondStatus = "pending"
)

// CondStatuses are all condition statuses.
func CondStatuses() []CondStatus { return []CondStatus{CondOK, CondFail, CondPending} }

// Valid reports a known status.
func (s CondStatus) Valid() bool { return s == CondOK || s == CondFail || s == CondPending }

// ChangeKind is what a change changes.
type ChangeKind string

const (
	KindCode   ChangeKind = "code"
	KindInfra  ChangeKind = "infra"
	KindHost   ChangeKind = "host"
	KindData   ChangeKind = "data"
	KindDoc    ChangeKind = "doc"
	KindDesign ChangeKind = "design"
	KindOther  ChangeKind = "other"
)

// ChangeKinds are the kinds a change may have.
func ChangeKinds() []ChangeKind {
	return []ChangeKind{KindCode, KindInfra, KindHost, KindData, KindDoc, KindDesign, KindOther}
}

// Valid reports a known kind.
func (k ChangeKind) Valid() bool {
	for _, x := range ChangeKinds() {
		if x == k {
			return true
		}
	}
	return false
}

// DecisionKind is what a decision asks the owner.
type DecisionKind string

const (
	DecisionAsk      DecisionKind = "ask"      // AskUserQuestion: questions with options
	DecisionApproval DecisionKind = "approval" // a tool call that waits for permission
	DecisionEffect   DecisionKind = "effect"   // an effect Fyr runs once approved (SPEC 5.10)
)

// EffectKind is what an Effect does.
type EffectKind string

const (
	EffectSend          EffectKind = "send"
	EffectAnswer        EffectKind = "answer"
	EffectInterrupt     EffectKind = "interrupt"
	EffectStop          EffectKind = "stop"
	EffectSetModel      EffectKind = "setModel"
	EffectRefresh       EffectKind = "refresh"
	EffectCancelMessage EffectKind = "cancelMessage"
	EffectHandover      EffectKind = "handover"
	// EffectCancel withdraws an open question; the agent package sends it
	// to itself when the CLI cancels a request.
	EffectCancel EffectKind = "cancel"
	// EffectRun runs an approved EffectReq (SPEC 5.10): one of FyrEffects.
	EffectRun EffectKind = "effect"
	// EffectScript runs a script action's argv (V0.60).
	EffectScript EffectKind = "script.run"
	// EffectFeed asks the agent manager to deliver the thread's notices
	// (the agent feed, #47): a message that starts a turn when the agent is
	// idle; a running agent takes them at its next tool call.
	EffectFeed EffectKind = "agent.feed"
	// EffectThread starts a new thread with a first message (an agent
	// action pressed with in: "new").
	EffectThread EffectKind = "thread.create"
	// EffectGit reads git after a run (#35): the thread's folder facts it
	// carries, each workspace of its changes, and whether the branches of
	// the records it references merged or went. Each read lands as an
	// Input of a host change record.
	EffectGit EffectKind = "git.observe"
)

// RunKind is how an action runs (SPEC 0.5 V0.60 (2)).
type RunKind string

const (
	RunFyr    RunKind = "fyr"    // one of FyrEffects, only from Fyr
	RunScript RunKind = "script" // an exact command the agent declared
	RunAgent  RunKind = "agent"  // a prompt for this thread's agent or a new thread
)

// Holders of actions besides a change (V0.60 (1)).
const (
	OnThread = "thread"
	OnView   = "view"
)

// The effects Fyr runs itself (FyrEffects).
const (
	FyrMerge  = "pr.merge"
	FyrRerun  = "pr.rerunFailed"
	FyrDeploy = "fyr.deploy"
)

// ChangeAsk is what change_request asks the owner (V0.59 (5)). merge and
// deploy run a FyrEffect; the others report the owner's answer.
type ChangeAsk string

const (
	AskReview  ChangeAsk = "review"
	AskApprove ChangeAsk = "approve"
	AskMerge   ChangeAsk = "merge"
	AskApply   ChangeAsk = "apply"
	AskDeploy  ChangeAsk = "deploy"
	AskRevert  ChangeAsk = "revert"
)

// ChangeAsks are all asks, in the order the tool lists them.
func ChangeAsks() []ChangeAsk {
	return []ChangeAsk{AskReview, AskApprove, AskMerge, AskApply, AskDeploy, AskRevert}
}

// TaskStatus is the state of a subagent or background task, as the CLI
// reports it (V0.16). The CLI may report others; Fyr acts on running only.
type TaskStatus string

const (
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskStopped   TaskStatus = "stopped"
)

// Checks is the rollup of a PR's checks.
type Checks string

const (
	ChecksPass    Checks = "pass"
	ChecksFail    Checks = "fail"
	ChecksPending Checks = "pending"
	ChecksNone    Checks = "none"
)

// Names are the values of a vocabulary as plain strings.
func Names[T ~string](xs []T) []string {
	s := make([]string, len(xs))
	for i, x := range xs {
		s[i] = string(x)
	}
	return s
}

// OrList joins the values as "a, b or c", for a message.
func OrList[T ~string](xs []T) string {
	s := Names(xs)
	if len(s) < 2 {
		return strings.Join(s, "")
	}
	return strings.Join(s[:len(s)-1], ", ") + " or " + s[len(s)-1]
}
