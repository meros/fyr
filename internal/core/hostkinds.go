package core

import "time"

// The kinds whose reply waits for their effect (#48, InputKind.Await): a
// message cancel needs the agent's answer before it can say the agent never
// sees the message (V0.33); the provider commands (V0.50) and host setup
// commands change the host's own setup, outside the state, and report what
// they did. Each goes down the one path: the sender, the guard, the reduce,
// then the effect and the receipt with its outcome.
const (
	KindCancelMessage = "thread.cancelMessage"
	// KindThreadHandedIn creates a thread received from another host, with its
	// copied conversation history (decision 0039, V0.trans-host-handover):
	// args Adopted.
	KindThreadHandedIn = "thread.handedIn"
	// KindDeployNow is the owner's Restart now (V0.83): a deploy that
	// waits for running threads switches at once.
	KindDeployNow = "host.deployNow"
	// KindUpdateNow is the owner's Update now / Back to release (V0.126):
	// install the newest CI release at once, ending a local build's pause.
	KindUpdateNow = "host.updateNow"
	// KindHostSetupFix runs one setup wizard fix action on the host (decision 0037).
	KindHostSetupFix = "host.setupFix"
)

// KindRefreshModels asks a provider for its models now (V0.120).
const KindRefreshModels = "provider.refreshModels"

// ProviderKinds are the provider registry's commands (3.16).
var ProviderKinds = []string{"provider.import", "provider.create", "provider.update", "provider.setKey",
	"provider.login", "provider.loginCode", "provider.loginCancel", "provider.delete", KindRefreshModels}

// The effects of the host kinds; Request carries the command.
const (
	EffectProvider EffectKind = "provider"
	// EffectDeployNow stops a deploy's drain (V0.83).
	EffectDeployNow EffectKind = "deploy.now"
	// EffectUpdateNow starts the install of the newest release (V0.126).
	EffectUpdateNow EffectKind = "update.now"
	// EffectHostSetupFix runs a setup wizard fix action on the host (decision 0037).
	EffectHostSetupFix EffectKind = "host.setupFix"
)

// Adopted is a thread received with its owner's view and its history
// with the times it happened (trans-host handover). Project is the project
// the sender chose for it ("" for none): the reduce files the thread in it
// by hand, or by its folder when none is chosen (Filed,
// V0.handover-filed); Thread's own project is not read.
type Adopted struct {
	Thread  *Thread
	View    *View
	History []Event
	At      []time.Time
	Project string
}

func init() {
	command := func(eff EffectKind) func(Rec, Input, time.Time) (Result, error) {
		return func(_ Rec, in Input, _ time.Time) (Result, error) {
			return Result{Effects: []Effect{{Kind: eff, Request: &EffectReq{Action: in.Kind, Args: mapArgs(in)}}}}, nil
		}
	}
	for _, name := range ProviderKinds {
		RegisterKind(&InputKind{Name: name, On: RecHost, From: ownerOnly, Await: true, Reduce: command(EffectProvider)})
	}
	// V0.83: only the owner forces a deploy; no agent tool sends it.
	RegisterKind(&InputKind{Name: KindDeployNow, On: RecHost, From: ownerOnly, Await: true, Reduce: func(Rec, Input, time.Time) (Result, error) {
		return Result{Effects: []Effect{{Kind: EffectDeployNow}}}, nil
	}})
	// V0.126: only the owner leaves a local build or updates outside the
	// quiet period; an agent asks fyr.deploy instead.
	RegisterKind(&InputKind{Name: KindUpdateNow, On: RecHost, From: ownerOnly, Await: true, Reduce: func(Rec, Input, time.Time) (Result, error) {
		return Result{Effects: []Effect{{Kind: EffectUpdateNow}}}, nil
	}})
	// Decision 0037: the setup wizard's fixes are owner-only host commands.
	RegisterKind(&InputKind{Name: KindHostSetupFix, On: RecHost, From: ownerOnly, Await: true, Reduce: command(EffectHostSetupFix)})
	RegisterKind(&InputKind{Name: KindCancelMessage, From: ownerOnly, Await: true, Reduce: func(r Rec, in Input, _ time.Time) (Result, error) {
		id := mapArgs(in).Str("messageId")
		if id == "" {
			return Result{}, reject(RejectBadArgs, "messageId is required")
		}
		if OutboxState(r.T, id) == "" {
			return Result{}, reject(RejectConflict, "The agent has already read this message")
		}
		return Result{Effects: []Effect{{Kind: EffectCancelMessage, MessageID: id}}}, nil
	}})
	adoptReduce := func(r Rec, in Input, _ time.Time) (Result, error) {
		a, err := argsOf[Adopted](in)
		if err != nil {
			return Result{}, err
		}
		*r.T, *r.V = *CloneThread(a.Thread), *cloneView(a.View)
		r.T.ID, r.V.Entity = in.Thread, in.Thread
		// V0.handover-filed: the chosen project is a filing by hand; ReduceIn
		// files the thread (Filed) right after this reduce.
		r.T.ProjectID, r.T.ProjectManual = a.Project, a.Project != ""
		return Result{Events: a.History, At: a.At}, nil
	}
	RegisterKind(&InputKind{Name: KindThreadHandedIn, From: observed, Makes: true, Untouched: true, Reduce: adoptReduce})
}
