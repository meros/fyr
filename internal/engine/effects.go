package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

// The effect runner (decision 0020 (3), SPEC 5.10): a reduce never does
// work; it returns effects, and Fyr runs them here. A durable effect (a
// merge, a script, a deploy) was stored with the reduce that returned it;
// the runner claims it before the work, so it runs at most once, also
// across a restart, and its result comes back as an effect.result Input.

// start runs a reduce's effects in order, in the background.
func (e *Engine) start(threadID string, effs []core.Effect) {
	if len(effs) == 0 {
		return
	}
	go func() {
		for _, ef := range effs {
			e.runEffect(threadID, ef)
		}
	}()
}

func (e *Engine) runEffect(threadID string, ef core.Effect) {
	if ef.Durable() {
		won, err := e.st.MoveEffect(ef.ID, store.EffectPending, store.EffectStarted, nil)
		if err != nil {
			log.Printf("thread %s: effect %s: %v", threadID, ef.ID, err)
		}
		if !won {
			return // another runner has it, or it ran already
		}
	}
	res, later := e.do(threadID, ef)
	if later {
		e.st.MoveEffect(ef.ID, store.EffectStarted, store.EffectLater, nil)
		return
	}
	if res == nil {
		return
	}
	if ef.Durable() {
		// A restart in between landed it as lost: its result is not landed twice.
		if won, _ := e.st.MoveEffect(ef.ID, store.EffectStarted, store.EffectFinished, res); !won {
			return
		}
	}
	e.land(threadID, *res)
}

// land applies an effect's result through the path.
func (e *Engine) land(threadID string, r core.EffectResult) {
	if _, err := e.Apply(core.Input{Source: core.FromEffect(r.Effect.ID), Kind: core.KindEffectResult, Thread: threadID, Args: r}); err != nil {
		log.Printf("thread %s: result of effect %s: %v", threadID, r.Effect.ID, err)
	}
}

// do performs one effect. It returns the result to land, or nil when there
// is nothing to report, and later when the effect reports its own result
// (a deploy, from the next daemon).
func (e *Engine) do(threadID string, ef core.Effect) (*core.EffectResult, bool) {
	res := func(ok bool, exit int, text string) *core.EffectResult {
		return &core.EffectResult{Effect: ef, OK: ok, Exit: exit, Text: text}
	}
	switch ef.Kind {
	case core.EffectThread:
		r := e.Command(Cmd{CommandID: ef.ID, Action: "thread.create", Args: childArgs(e, threadID, core.Args{"cwd": ef.Cwd, "text": ef.Text, "projectId": ef.Project})})
		if !r.OK {
			return res(false, 0, r.Error), false
		}
		return res(true, 0, "Started thread "+r.ThreadID+"."), false
	case core.EffectScript:
		limit := time.Duration(ef.Script.TimeoutSec) * time.Second
		if limit == 0 {
			limit = 10 * time.Minute
		}
		ctx, cancel := context.WithTimeout(context.Background(), limit)
		run := e.Scripts
		if run == nil {
			run = runScript
		}
		out, exit, err := run(ctx, ef.Script.Dir, ef.Script.Argv)
		cancel()
		return res(err == nil, exit, out), false
	case core.EffectGit:
		e.observeGit(threadID, ef.Git)
		return nil, false
	case core.EffectRun:
		if ef.Request == nil {
			return nil, false
		}
		ok, text, later := e.runFyr(threadID, *ef.Request, ef.ID)
		return res(ok, 0, text), later
	}
	if e.Agents == nil {
		return nil, false
	}
	if err := e.Agents.Do(threadID, ef); err != nil {
		log.Printf("thread %s: effect %s: %v", threadID, ef.Kind, err)
		return res(false, 0, err.Error()), false
	}
	if ef.Press != nil { // an agent action sent to this thread: the press ends
		return res(true, 0, ""), false
	}
	return nil, false
}

// childArgs completes the args of a thread that another thread starts
// (V0.71): unless they name them, it follows the parent's provider, model and
// effort, and names the parent.
func childArgs(e *Engine, parent string, args core.Args) core.Args {
	p, ok := e.Thread(parent)
	if !ok {
		return args
	}
	args["parent"] = parent
	for k, v := range map[string]string{"profile": p.Profile, "model": p.Model, "effort": p.Effort} {
		if _, set := args[k]; !set && v != "" {
			args[k] = v
		}
	}
	return args
}

// runFyr runs one Fyr effect, whoever started it. The args are completed
// and the guard is checked again first, as when it was offered (SPEC 5.10
// (4)).
func (e *Engine) runFyr(threadID string, req core.EffectReq, id string) (ok bool, text string, later bool) {
	t, found := e.Thread(threadID)
	if !found {
		return false, "The thread is gone.", false
	}
	fx := core.FyrEffects[req.Action]
	defer func() {
		if fx != nil && fx.OnPR && e.OnPR != nil {
			e.OnPR(threadID)
		}
	}()
	switch {
	case fx == nil:
		return false, fmt.Sprintf("Fyr has no effect %q.", req.Action), false
	case fx.Check(req.Args) != nil:
		return false, fx.Check(req.Args).Error() + ".", false
	}
	if why := fx.Guard(&t, req.Args); why != "" {
		return false, why + ".", false
	}
	if e.Effects == nil {
		return false, "No effect runner is configured.", false
	}
	req.ID = id
	ctx, cancel := context.WithTimeout(context.Background(), fx.Timeout)
	out, err := e.Effects.Run(ctx, threadID, req)
	cancel()
	if errors.Is(err, core.ErrReportLater) {
		return false, "", true
	}
	if err != nil {
		return false, err.Error(), false
	}
	return true, out, false
}

// EffectTimeout is the deadline of one Fyr effect (core.FyrEffects).
func EffectTimeout(action string) time.Duration {
	if fx := core.FyrEffects[action]; fx != nil {
		return fx.Timeout
	}
	return 10 * time.Minute
}

// Report lands the result of a Fyr effect that reports later (a deploy,
// from the daemon that runs the new build, SPEC 0.5 V0.13).
func (e *Engine) Report(threadID string, req core.EffectReq, allowed, ok bool, text string) {
	id := req.ID
	if id == "" { // a deploy started before effects had ids
		id = NewID("fx_")
	}
	r := core.EffectResult{Effect: core.Effect{ID: id, Kind: core.EffectRun, Allow: allowed, Request: &req}, OK: ok, Text: text}
	e.st.MoveEffect(id, store.EffectLater, store.EffectFinished, r)
	e.land(threadID, r)
}

// ResumeEffects finishes the durable effects the last run of the host left
// (at most once): a pending one runs now; one that had started is not run
// again and lands as lost; a finished one's stored result lands; one that
// reports later waits for its reporter. Call it once, after Load, with the
// runners set.
func (e *Engine) ResumeEffects() error {
	open, err := e.st.OpenEffects()
	if err != nil {
		return err
	}
	for _, s := range open {
		var ef core.Effect
		if err := json.Unmarshal(s.Data, &ef); err != nil {
			log.Printf("effect %s: %v", s.ID, err)
			continue
		}
		switch s.State {
		case store.EffectPending:
			e.start(s.Thread, []core.Effect{ef})
		case store.EffectStarted:
			r := core.EffectResult{Effect: ef, Exit: -1, Text: core.LostText}
			e.st.MoveEffect(s.ID, store.EffectStarted, store.EffectFinished, r)
			e.land(s.Thread, r)
		case store.EffectFinished:
			var r core.EffectResult
			if err := json.Unmarshal(s.Result, &r); err != nil {
				log.Printf("effect %s: %v", s.ID, err)
				continue
			}
			e.land(s.Thread, r)
		}
	}
	return nil
}
