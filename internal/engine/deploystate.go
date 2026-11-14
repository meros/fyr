package engine

import "time"

// SPEC 0.5 V0.39: the latest deploy progress, for the state and the stream.
// A finished deploy (done or failed) stays two minutes, so a page that
// reloads onto the new build still sees how it ended.

type deployState struct {
	v     any
	phase string
	at    time.Time
}

// SetDeploy stores the progress and sends it on the stream.
func (e *Engine) SetDeploy(v any, phase string) {
	e.mu.Lock()
	e.deploy = &deployState{v: v, phase: phase, at: time.Now()}
	e.mu.Unlock()
	e.Hub.Deploy(v)
}

// DeployState is the progress to show at now, or nil.
func (e *Engine) DeployState(now time.Time) any {
	e.mu.Lock()
	defer e.mu.Unlock()
	d := e.deploy
	if d == nil {
		return nil
	}
	if (d.phase == "done" || d.phase == "failed") && now.Sub(d.at) > 2*time.Minute {
		return nil
	}
	return d.v
}
