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
	// V0.rerun-classes: add or remove a class the guard may kill and rerun.
	KindRerunClasses = "host.rerunClasses"

	EffectHeavyRemove        EffectKind = "heavy.remove"
	EffectUndeclaredSettings EffectKind = "undeclared.settings"

	// V0.services-not-jobs: the owner adds or removes a service pattern.
	KindServicePatterns              = "host.servicePatterns"
	EffectServicePatterns EffectKind = "service.patterns"
	EffectRerunClasses    EffectKind = "rerun.classes"
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
	effect(KindServicePatterns, EffectServicePatterns, func(m map[string]any) (map[string]any, error) {
		add, remove := str(m, "add", 300), str(m, "remove", 300)
		if add == "" && remove == "" {
			return nil, errors.New("add or remove a pattern")
		}
		return map[string]any{"add": add, "remove": remove}, nil
	})
	effect(KindRerunClasses, EffectRerunClasses, func(m map[string]any) (map[string]any, error) {
		add, remove := str(m, "add", 200), str(m, "remove", 200)
		switch {
		case add != "" && remove == "":
			return map[string]any{"add": add}, nil
		case remove != "" && add == "":
			return map[string]any{"remove": remove}, nil
		}
		return nil, errors.New("give add or remove: one class of 1 to 200 characters")
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
