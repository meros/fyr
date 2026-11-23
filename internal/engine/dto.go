package engine

import (
	"sort"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
)

func (e *Engine) dto(t *core.Thread, now time.Time) api.Thread {
	d, _ := e.seen(t, now)
	return d
}

// seen builds a thread's DTO, records its attention as the one clients have
// and returns the attention they had before.
func (e *Engine) seen(t *core.Thread, now time.Time) (api.Thread, core.State) {
	d := e.build(t, now)
	prev := e.last[t.ID]
	e.last[t.ID] = d.Attention.State
	return d, prev
}

// build makes a DTO without recording it as seen by clients.
func (e *Engine) build(t *core.Thread, now time.Time) api.Thread {
	v := e.view(t.ID)
	un := map[string]string{}
	for name, a := range core.Registry {
		if name == "thread.create" {
			continue
		}
		if r := a.Available(t, v, now); r != "" {
			un[name] = r
		}
	}
	att := core.ComputeWith(t, v, now, e.autoMergeOn)
	vc := *v
	vc.ChangesLook = nil // V0.65: served with the Changes tab, not on every push
	vd := &api.View{View: &vc}
	vd.SnoozeUntil, vd.SnoozedAt = core.SnoozeOf(v)
	vd.SnoozeWhenDone = core.SnoozedWhenDone(v)
	changes := e.shown(t)
	running := t.Running || t.RunningTasks() > 0
	for i := range changes { // V0.97: derived, never stored
		c := &changes[i]
		if c.Phase.InFlight() && !c.Dismissed {
			c.Ready = core.Ready(c.PR, running)
		}
		c.Need = core.ChangeNeedWith(t, c, e.autoMergeOn) // V0.101: the client only reads it
		// V0.sensitive-pr: the agent's reason or the GitHub label.
		c.Sensitive = core.SensitiveWhy(c)
	}
	quiet := core.PushQuiet(t, att.State, e.autoMergeOn)
	return api.Thread{Thread: t, Rev: e.revs[t.ID], Attention: att, View: vd, Unavailable: un, NewProject: e.draft(t), QuietUntil: e.quietUntil(t.ProjectID, now), PushQuiet: quiet,
		PR: t.MainPR(), Project: e.projectName(t), LiveView: t.ViewOn(),
		SettledAt: t.Settle.At, SettleWhenDone: core.SettlesWhenDone(t), SettleWhenMerged: core.SettlesWhenMerged(t), SettleWhen: core.SettleWhenChange(t), Wakes: core.Wakes(t), Waits: core.Waits(t, now), Changes: changes,
		RootPrompt: e.threadRootPrompt(t), Usage: e.threadUsage(t.ID)}
}

// threadUsage is the thread's token total (V0.usage-ledger). Call it with
// e.mu held.
func (e *Engine) threadUsage(id string) *api.Usage {
	if u := e.usage[id]; u != nil {
		c := *u
		return &c
	}
	return nil
}

// SetThreadUsage takes a thread's token total from the meter
// (V0.usage-ledger). The meter calls it right after it wrote a report and
// before the agent reports the run's end, so that apply's DTO carries the
// new total. It calls no dependency and takes e.mu only to store the total.
func (e *Engine) SetThreadUsage(thread string, u api.Usage) {
	e.mu.Lock()
	e.usage[thread] = &u
	e.mu.Unlock()
}

// threadRootPrompt is why the thread's root run requests cannot ask for a
// password on this host, read only when it has one (V0.149 (4)).
func (e *Engine) threadRootPrompt(t *core.Thread) string {
	for _, a := range t.Offers {
		if a.Request && a.Root {
			return e.rootPrompt()
		}
	}
	return ""
}

// Snapshot returns every thread as DTOs, most recently updated first.
func (e *Engine) Snapshot() []api.Thread {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.Now()
	out := make([]api.Thread, 0, len(e.threads))
	for _, t := range e.threads {
		out = append(out, e.build(core.CloneThread(t), now))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}
