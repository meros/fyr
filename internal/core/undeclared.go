package core

import (
	"errors"
	"strings"
	"time"
)

// V0.164, V0.209: undeclared heavy work. The host's detector flags a thread
// that runs heavy work it was not admitted for; Fyr counts it and learns the
// command by itself. The owner's two commands remove a learned heavy command
// and set the thresholds.
const (
	KindHeavyRemove        = "host.heavyRemove"
	KindUndeclaredSettings = "host.undeclaredSettings"

	EffectHeavyRemove        EffectKind = "heavy.remove"
	EffectUndeclaredSettings EffectKind = "undeclared.settings"
)

func init() {
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
