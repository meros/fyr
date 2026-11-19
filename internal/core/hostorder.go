package core

import (
	"strings"
	"time"
)

// The owner's host order (V0.132, decision 0016): which host is first, the
// default for anything that names none, on every front. It belongs to the
// owner, not to a front, so every front writes it to every host it follows
// and reads the newest back (`at` decides) until the roster of decision 0026
// carries it. Owner only (a host command with a receipt); no agent tool sends it.
const (
	KindHostOrderSet = "host.hostOrder"
	// EffectHostOrderSet stores the order on the host.
	EffectHostOrderSet EffectKind = "hostOrder.set"
	maxHostOrder                  = 100
)

func init() {
	RegisterKind(&InputKind{Name: KindHostOrderSet, On: RecHost, From: ownerOnly, Await: true, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
		a := mapArgs(in)
		raw, ok := a["names"].([]any)
		if !ok || len(raw) > maxHostOrder {
			return Result{}, reject(RejectBadArgs, "names must be a list of at most %d host names", maxHostOrder)
		}
		at, err := time.Parse(time.RFC3339Nano, a.Str("at"))
		if err != nil || at.IsZero() {
			return Result{}, reject(RejectBadArgs, "at must be the time the owner set the order (RFC 3339)")
		}
		names := cleanList(raw)
		args := map[string]any{"names": names, "at": at.UTC().Format(time.RFC3339Nano)}
		// V0.host-ids: the hosts by install id too (decision 0031 F17: the name is
		// config and can change, the id cannot). A front before it sends names only.
		if ids, ok := a["ids"].([]any); ok {
			if len(ids) > maxHostOrder {
				return Result{}, reject(RejectBadArgs, "ids must be a list of at most %d host ids", maxHostOrder)
			}
			args["ids"] = cleanList(ids)
		}
		return Result{Effects: []Effect{{Kind: EffectHostOrderSet, Request: &EffectReq{Action: KindHostOrderSet, Args: args}}}}, nil
	}})
}

// cleanList is the strings of raw, trimmed, without empties, long ones or repeats.
func cleanList(raw []any) []any {
	out := []any{}
	seen := map[string]bool{}
	for _, x := range raw {
		s, _ := x.(string)
		s = strings.TrimSpace(s)
		if s == "" || len(s) > 200 || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
