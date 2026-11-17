package core

import (
	"errors"
	"strings"
	"time"
)

// V0.176: leftover processes and temporary files. The host ends a thread's
// leftovers by policy and leaves a line on its thread (leftover.ended, an
// EventSlot with "leftovers"; the agent hears it at its next run, never a
// wake); the owner's End and Keep of one leftover, and Remove of a listed
// worktree, are host commands (owner only).
const (
	KindLeftoverEnded = "leftover.ended"
	KindLeftoverEnd   = "host.leftoverEnd"
	KindLeftoverKeep  = "host.leftoverKeep"
	KindTempRemove    = "host.tempRemove"

	EffectLeftoverEnd  EffectKind = "leftover.end"
	EffectLeftoverKeep EffectKind = "leftover.keep"
	EffectTempRemove   EffectKind = "temp.remove"
)

// LeftoverEndedArgs is leftover.ended's: the line, and what the agent hears.
type LeftoverEndedArgs struct {
	Text string `json:"text"`
}

func init() {
	RegisterKind(&InputKind{Name: KindLeftoverEnded, From: observed, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
		a, err := argsOf[LeftoverEndedArgs](in)
		if err != nil || a.Text == "" {
			return Result{}, errors.New("leftover.ended needs the text")
		}
		return Result{Events: []Event{{EventSlot, map[string]any{"leftovers": a.Text}}}}, nil
	}})
	effect := func(name string, k EffectKind, check func(Args) (map[string]any, error)) {
		RegisterKind(&InputKind{Name: name, On: RecHost, From: ownerOnly, Await: true, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
			args, err := check(mapArgs(in))
			if err != nil {
				return Result{}, reject(RejectBadArgs, "%s", err.Error())
			}
			return Result{Effects: []Effect{{Kind: k, Request: &EffectReq{Action: name, Args: args}}}}, nil
		}})
	}
	id := func(m Args) (string, error) {
		s, _ := m["id"].(string)
		if s = strings.TrimSpace(s); s == "" || len(s) > 64 {
			return "", errors.New("id must name a leftover process")
		}
		return s, nil
	}
	effect(KindLeftoverEnd, EffectLeftoverEnd, func(m Args) (map[string]any, error) {
		s, err := id(m)
		return map[string]any{"id": s}, err
	})
	effect(KindLeftoverKeep, EffectLeftoverKeep, func(m Args) (map[string]any, error) {
		s, err := id(m)
		if err != nil {
			return nil, err
		}
		h := 24.0
		if v, ok := m["hours"]; ok {
			f, ok := v.(float64)
			if !ok || f <= 0 || f > 168 {
				return nil, errors.New("hours must be above 0 and at most 168")
			}
			h = f
		}
		return map[string]any{"id": s, "hours": h}, nil
	})
	effect(KindTempRemove, EffectTempRemove, func(m Args) (map[string]any, error) {
		p, _ := m["path"].(string)
		if p = strings.TrimSpace(p); !strings.HasPrefix(p, "/") || len(p) > 1000 {
			return nil, errors.New("path must be an absolute path of a listed folder")
		}
		return map[string]any{"path": p}, nil
	})
}
