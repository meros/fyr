package agent

import (
	"fmt"
	"log"
	"strings"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/usage"
)

// V0.usage-ledger (decision 0041): each result's usage goes to the meter
// with the dimensions it has now.

// triggerOf names what a message starts when it starts a turn.
func triggerOf(e core.Effect) string {
	switch e.From {
	case "":
		return usage.TriggerOwner
	case FromView:
		return usage.TriggerView
	case FromHandover:
		return usage.TriggerHandover
	}
	return usage.TriggerThread
}

// noticeTrigger names what the feed's notices start: a wake when one of
// them is a Rule's (a watch, a wake-up), else a notice.
func noticeTrigger(ns []core.Notice) string {
	for _, n := range ns {
		if strings.HasPrefix(n.About, "rule:") {
			return usage.TriggerWake
		}
	}
	return usage.TriggerNotice
}

// noteTrigger keeps what a message starts: the turn's trigger when it
// starts one now, else the message's, for when the CLI starts it.
func noteTrigger(s *session, id, trig string, first bool) {
	if first {
		s.trigger = trig
		return
	}
	if s.trigs == nil {
		s.trigs = map[string]string{}
	}
	s.trigs[id] = trig
}

// queuedStarted: a queued message starts a turn (command_lifecycle
// started); the turn takes its trigger. Call with m.mu held.
func queuedStarted(s *session, id string) {
	if t := s.trigs[id]; t != "" && !s.live {
		s.trigger = t
	}
	delete(s.trigs, id)
}

// meterResult records a result's usage. It runs for every result, a stray
// one too: a result outside a turn still used tokens.
func (m *Manager) meterResult(s *session, msg claude.Msg) {
	if m.cfg.Meter == nil {
		return
	}
	t, ok := m.host.Thread(s.thread)
	if !ok {
		return
	}
	p := m.cfg.Profile(t.Profile)
	ad, err := m.adapter(p)
	if err != nil {
		return
	}
	ur, ok := ad.(cli.UsageReader)
	if !ok {
		return
	}
	r, ok := ur.Usage(msg)
	if !ok {
		return
	}
	m.mu.Lock()
	trig, model := s.trigger, s.model
	m.mu.Unlock()
	if trig == "" {
		trig = usage.TriggerBackground
	}
	if model == "" {
		model = t.Model
	}
	id := r.ID
	if id == "" {
		// A CLI that names no result: the runner's sequence number names the
		// line in this process, so a line delivered again is the same one.
		if seq, _ := msg[claude.SeqKey].(int64); seq == 0 {
			id = newUUID()
		} else {
			id = fmt.Sprintf("%s:%d", s.proc, msg[claude.SeqKey])
		}
	}
	if r.Session == "" {
		r.Session = t.SessionID
	}
	effort := t.Effort
	if effort == "" {
		effort = s.effort
	}
	d := usage.Dims{Source: usage.SourceTurn, Trigger: trig, Thread: t.ID, Project: t.ProjectID, Provider: p.ID,
		Adapter: ad.ID(), MainModel: model, Effort: effort}
	if m.cfg.ThreadSource != nil {
		if src := m.cfg.ThreadSource(t.ID); src != "" {
			d.Source = src // V0.usage-jobs: a project's setup run is Fyr's job
		}
	}
	m.record(id, d, r)
}

// record gives a report its account and writes it.
func (m *Manager) record(id string, d usage.Dims, r cli.UsageReport) {
	if m.cfg.Account != nil {
		d.Account, d.Billing = m.cfg.Account(d.Provider)
	}
	if _, err := m.cfg.Meter.Record(id, d, r); err != nil {
		log.Printf("usage: %s %s: %v", d.Source, d.Thread, err)
	}
}

// jobResult is the Launch.Result of a system job's print run
// (V0.usage-jobs): it records the call as Fyr's own, for the thread it
// served ("" for none, as a search).
func (m *Manager) jobResult(job, thread string, a SystemAgent, ad cli.Adapter, effort string) func(cli.Msg) {
	ur, ok := ad.(cli.UsageReader)
	if m.cfg.Meter == nil || !ok {
		return nil
	}
	return func(msg cli.Msg) {
		r, ok := ur.Usage(msg)
		if !ok {
			return
		}
		// A print run is one session of one turn, never resumed: no
		// baseline to keep.
		r.Scope = cli.PerTurn
		d := usage.Dims{Source: usage.SourceJob(job), Thread: thread, Provider: a.Profile.ID, Adapter: ad.ID(),
			MainModel: a.Model, Effort: effort}
		if t, ok := m.host.Thread(thread); ok && thread != "" {
			d.Project = t.ProjectID
		}
		id := r.ID
		if id == "" {
			id = newUUID()
		}
		m.record("job:"+id, d, r)
	}
}
