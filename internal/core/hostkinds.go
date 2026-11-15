package core

import "time"

// The kinds whose reply waits for their effect (#48, InputKind.Await): a
// message cancel needs the agent's answer before it can say the agent never
// sees the message (V0.33); T3 adoption (V0.9) and the provider commands
// (V0.50) change the host's own setup, outside the state, and report what
// they did. Each goes down the one path: the sender, the guard, the reduce,
// then the effect and the receipt with its outcome.
const (
	KindCancelMessage = "thread.cancelMessage"
	KindT3Adopt       = "t3.adopt"
	// KindThreadAdopted is one thread adopted from T3, with its history:
	// args Adopted.
	KindThreadAdopted = "thread.adopted"
)

// ProviderKinds are the provider registry's commands (3.16).
var ProviderKinds = []string{"provider.import", "provider.create", "provider.update", "provider.setKey",
	"provider.login", "provider.loginCode", "provider.loginCancel", "provider.delete"}

// The effects of the host kinds; Request carries the command.
const (
	EffectAdopt    EffectKind = "t3.adopt"
	EffectProvider EffectKind = "provider"
)

// Adopted is a thread T3 adoption made, its owner's view and its history
// with the times it happened.
type Adopted struct {
	Thread  *Thread
	View    *View
	History []Event
	At      []time.Time
}

func init() {
	command := func(eff EffectKind) func(Rec, Input, time.Time) (Result, error) {
		return func(_ Rec, in Input, _ time.Time) (Result, error) {
			return Result{Effects: []Effect{{Kind: eff, Request: &EffectReq{Action: in.Kind, Args: mapArgs(in)}}}}, nil
		}
	}
	RegisterKind(&InputKind{Name: KindT3Adopt, On: RecHost, From: ownerOnly, Await: true, Reduce: command(EffectAdopt)})
	for _, name := range ProviderKinds {
		RegisterKind(&InputKind{Name: name, On: RecHost, From: ownerOnly, Await: true, Reduce: command(EffectProvider)})
	}
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
	RegisterKind(&InputKind{Name: KindThreadAdopted, From: observed, Makes: true, Untouched: true, Reduce: func(r Rec, in Input, _ time.Time) (Result, error) {
		a, err := argsOf[Adopted](in)
		if err != nil {
			return Result{}, err
		}
		*r.T, *r.V = *CloneThread(a.Thread), *cloneView(a.View)
		r.T.ID, r.V.Entity = in.Thread, in.Thread
		return Result{Events: a.History, At: a.At}, nil
	}})
}
