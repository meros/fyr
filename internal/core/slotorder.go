package core

import (
	"strings"
	"time"
)

// The owner's order of the queue for heavy-job slots (SPEC V0.137): which
// queued thread gets the next free slot. Owner only (a host command with a
// receipt); no agent tool sends it, so an agent cannot jump the queue.
const (
	KindSlotOrderSet = "host.slotOrder"
	// EffectSlotOrderSet reorders the host's queue.
	EffectSlotOrderSet EffectKind = "slotOrder.set"
	maxSlotOrder                  = 500
)

func init() {
	RegisterKind(&InputKind{Name: KindSlotOrderSet, On: RecHost, From: ownerOnly, Await: true, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
		raw, ok := mapArgs(in)["owners"].([]any)
		if !ok || len(raw) > maxSlotOrder {
			return Result{}, reject(RejectBadArgs, "owners must be a list of at most %d queued thread ids", maxSlotOrder)
		}
		owners := []any{}
		for _, x := range raw {
			if s, _ := x.(string); strings.TrimSpace(s) != "" {
				owners = append(owners, strings.TrimSpace(s))
			}
		}
		return Result{Effects: []Effect{{Kind: EffectSlotOrderSet, Request: &EffectReq{Action: KindSlotOrderSet, Args: map[string]any{"owners": owners}}}}}, nil
	}})
}
