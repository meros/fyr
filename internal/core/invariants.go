package core

import (
	"strings"
	"time"
)

// V0.load-invariants: the owner's two presses on a load invariant's card in
// Needs you. Fix it starts a thread in the host's Fyr checkout with the
// evidence; Dismiss closes the card, and no card of that invariant comes
// for a day. Host commands, the owner's only: fyrd never starts the fix by
// itself and no agent tool sends them.
const (
	KindInvariantFix     = "host.invariantFix"
	KindInvariantDismiss = "host.invariantDismiss"

	EffectInvariantFix     EffectKind = "invariant.fix"
	EffectInvariantDismiss EffectKind = "invariant.dismiss"
)

func init() {
	for name, k := range map[string]EffectKind{KindInvariantFix: EffectInvariantFix, KindInvariantDismiss: EffectInvariantDismiss} {
		RegisterKind(&InputKind{Name: name, On: RecHost, From: ownerOnly, Await: true, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
			id, _ := mapArgs(in)["id"].(string)
			if id = strings.TrimSpace(id); id == "" || len(id) > 64 {
				return Result{}, reject(RejectBadArgs, "id must name a load invariant")
			}
			return Result{Effects: []Effect{{Kind: k, Request: &EffectReq{Action: name, Args: map[string]any{"id": id}}}}}, nil
		}})
	}
}
