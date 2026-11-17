package core

import (
	"errors"
	"strings"
	"time"
)

// V0.164: undeclared heavy work. The host's detector flags a thread that runs
// heavy work it was not admitted for; the agent is told once (slot.undeclared,
// a Next notice, never a wake), and the owner's four commands (all owner
// only) dismiss a mark, add or ignore a suggested heavy command, remove an
// added one, and set the thresholds.
const (
	KindSlotUndeclared     = "slot.undeclared"
	KindUndeclaredDismiss  = "host.undeclaredDismiss"
	KindHeavySuggestion    = "host.heavySuggestion"
	KindHeavyRemove        = "host.heavyRemove"
	KindUndeclaredSettings = "host.undeclaredSettings"

	EffectUndeclaredDismiss  EffectKind = "undeclared.dismiss"
	EffectHeavySuggestion    EffectKind = "heavy.suggestion"
	EffectHeavyRemove        EffectKind = "heavy.remove"
	EffectUndeclaredSettings EffectKind = "undeclared.settings"
)

// SlotUndeclaredArgs is slot.undeclared's: the text the agent gets, and the
// use it names (bytes and cores) for the conversation's line.
type SlotUndeclaredArgs struct {
	Text string  `json:"text"`
	Mem  uint64  `json:"mem"`
	CPU  float64 `json:"cpu"`
}

func init() {
	RegisterKind(&InputKind{Name: KindSlotUndeclared, From: observed, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
		a, err := argsOf[SlotUndeclaredArgs](in)
		if err != nil || a.Text == "" {
			return Result{}, errors.New("slot.undeclared needs the text")
		}
		return Result{Events: []Event{{EventSlot, map[string]any{"undeclared": a.Text, "mem": a.Mem, "cpu": a.CPU}}}}, nil
	}})
	effect := func(name string, k EffectKind, check func(map[string]any) (map[string]any, error)) {
		RegisterKind(&InputKind{Name: name, On: RecHost, From: ownerOnly, Await: true, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
			args, err := check(mapArgs(in))
			if err != nil {
				return Result{}, reject(RejectBadArgs, "%s", err.Error())
			}
			return Result{Effects: []Effect{{Kind: k, Request: &EffectReq{Action: name, Args: args}}}}, nil
		}})
	}
	str := func(m map[string]any, k string, max int) string {
		s, _ := m[k].(string)
		if s = strings.TrimSpace(s); len(s) > max {
			return ""
		}
		return s
	}
	effect(KindUndeclaredDismiss, EffectUndeclaredDismiss, func(m map[string]any) (map[string]any, error) {
		th := str(m, "thread", 100)
		if th == "" {
			return nil, errors.New("thread must name a thread")
		}
		on, ok := m["on"].(bool)
		if !ok {
			on = true
		}
		return map[string]any{"thread": th, "on": on}, nil
	})
	effect(KindHeavySuggestion, EffectHeavySuggestion, func(m map[string]any) (map[string]any, error) {
		id, do := str(m, "id", 100), str(m, "do", 10)
		if id == "" || (do != "add" && do != "ignore") {
			return nil, errors.New("id and do (add or ignore) are needed")
		}
		out := map[string]any{"id": id, "do": do}
		if do == "add" {
			p := str(m, "pattern", 300)
			if p == "" {
				return nil, errors.New("pattern is needed (at most 300 characters)")
			}
			out["pattern"], out["kind"] = p, str(m, "kind", 10)
		}
		return out, nil
	})
	effect(KindHeavyRemove, EffectHeavyRemove, func(m map[string]any) (map[string]any, error) {
		p := str(m, "pattern", 300)
		if p == "" {
			return nil, errors.New("pattern must name an added pattern")
		}
		return map[string]any{"pattern": p}, nil
	})
	effect(KindUndeclaredSettings, EffectUndeclaredSettings, func(m map[string]any) (map[string]any, error) {
		out := map[string]any{}
		if v, ok := m["on"]; ok {
			b, ok := v.(bool)
			if !ok {
				return nil, errors.New("on must be true or false")
			}
			out["on"] = b
		}
		for _, k := range []string{"cpu", "memory"} {
			if v, ok := m[k]; ok {
				f, ok := v.(float64)
				if !ok || f <= 0 || f > 1 {
					return nil, errors.New(k + " must be a share above 0 and at most 1")
				}
				out[k] = f
			}
		}
		if v, ok := m["after"]; ok {
			f, ok := v.(float64)
			if !ok || f < 30 || f > 120 {
				return nil, errors.New("after must be 30 to 120 seconds")
			}
			out["after"] = f
		}
		if len(out) == 0 {
			return nil, errors.New("nothing to set")
		}
		return out, nil
	})
}
