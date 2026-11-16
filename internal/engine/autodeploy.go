package engine

import (
	"context"
	"log"
	"maps"
	"slices"
	"time"

	"github.com/meros/fyr/internal/core"
)

// autoDeployKey is the host kv key of the auto-deploy setting (V0.110):
// "off", or "on"; empty is on, the owner's default.
const autoDeployKey = "autoDeploy"

// AutoDeploySetting is GET /api/autoDeploy and the reply of host.autoDeploy.
type AutoDeploySetting struct {
	On bool `json:"on"`
}

// Where an open deploy ask stands for the auto-deploy pass (V0.110).
const (
	deployHeld      = "held"      // the gate refused: it waits for the owner
	deployWaiting   = "wait"      // another deploy ran; the line was shown
	deployApproving = "approving" // approved
)

// loadAutoDeploy reads the setting. Call it with e.mu held.
func (e *Engine) loadAutoDeploy() {
	raw, _ := e.st.GetKV(autoDeployKey)
	e.autoDeployOff = raw == "off"
}

// AutoDeploy is the setting.
func (e *Engine) AutoDeploy() AutoDeploySetting {
	e.mu.Lock()
	defer e.mu.Unlock()
	return AutoDeploySetting{On: !e.autoDeployOff}
}

// setAutoDeploy stores the owner's setting, then approves the deploy asks
// that already wait.
func (e *Engine) setAutoDeploy(on bool) error {
	v := "off"
	if on {
		v = "on"
	}
	if err := e.st.SetKV(autoDeployKey, v); err != nil {
		return err
	}
	e.mu.Lock()
	e.autoDeployOff = !on
	e.mu.Unlock()
	if on {
		e.AutoDeploySweep()
	}
	return nil
}

// AutoDeploySweep looks at the open deploy asks of every thread: the
// deployer calls it when no deploy runs any more, and fyrd at start.
func (e *Engine) AutoDeploySweep() {
	e.mu.Lock()
	ids := slices.Sorted(maps.Keys(e.threads))
	e.mu.Unlock()
	for _, id := range ids {
		e.autoDeploy(id)
	}
}

// deployAsks are the thread's open deploy asks that the pass has not
// settled: not held for the owner and not approved. Call it with e.mu held.
func (e *Engine) deployAsks(id string) []core.AutoDeployAsk {
	t := e.threads[id]
	if t == nil || len(t.Decisions) == 0 || e.autoDeployOff || e.DeployGate == nil {
		return nil
	}
	var out []core.AutoDeployAsk
	for _, a := range core.AutoDeployAsks(t) {
		if s := e.deploySeen[id+"/"+a.Decision]; s != deployHeld && s != deployApproving {
			out = append(out, a)
		}
	}
	return out
}

// autoDeploy starts the pass for the thread's open deploy asks (V0.110).
// publish calls it after every Input; the gate reads git, so the pass runs
// in the background.
func (e *Engine) autoDeploy(id string) {
	e.mu.Lock()
	asks := e.deployAsks(id)
	e.mu.Unlock()
	if len(asks) > 0 {
		go e.gateDeploys(id)
	}
}

// gateDeploys approves, holds or keeps waiting each open deploy ask of the
// thread. deployMu makes one pass at a time, so two asks never both see no
// deploy running.
func (e *Engine) gateDeploys(id string) {
	e.deployMu.Lock()
	defer e.deployMu.Unlock()
	e.mu.Lock()
	asks := e.deployAsks(id)
	e.mu.Unlock()
	for _, a := range asks {
		key := id + "/" + a.Decision
		e.mu.Lock()
		busy := e.deploys > 0
		prev := e.deploySeen[key]
		e.mu.Unlock()
		if !busy && e.DeployBusy != nil {
			busy = e.DeployBusy()
		}
		args := core.AutoDeployArgs{Decision: a.Decision}
		var state string
		switch {
		case busy && prev == deployWaiting:
			continue // the line is shown; the deployer's OnIdle comes back
		case busy:
			args.Wait, state = true, deployWaiting
		case a.Dir == "":
			args.Held, state = "the ask names no checkout", deployHeld
		default:
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			args.Commit, args.Held = e.DeployGate(ctx, a.Dir)
			cancel()
			state = deployHeld
			if args.Held == "" {
				state = deployApproving
			}
		}
		e.mu.Lock()
		e.deploySeen[key] = state
		if state == deployApproving {
			e.deploys++ // until the deploy effect ends (do)
		}
		e.mu.Unlock()
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindAutoDeploy, Thread: id, Args: args}); err != nil {
			log.Printf("thread %s: auto-deploy %s: %v", id, a.Decision, err)
			if state == deployApproving {
				e.mu.Lock()
				e.deploys--
				e.mu.Unlock()
			}
		}
	}
}

// deployStarted counts a deploy the owner pressed; an auto-approved one was
// counted when it was approved. The returned func counts it out and looks
// at the asks that wait.
func (e *Engine) deployStarted(req core.EffectReq) func() {
	e.mu.Lock()
	if on, _ := req.Args["autoDeploy"].(bool); !on {
		e.deploys++
	}
	e.mu.Unlock()
	return func() {
		e.mu.Lock()
		if e.deploys > 0 {
			e.deploys--
		}
		e.mu.Unlock()
		go e.AutoDeploySweep()
	}
}
