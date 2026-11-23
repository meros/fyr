package engine

import (
	"encoding/json"
	"errors"
	"log"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
	"github.com/meros/fyr/internal/stream"
)

// Apply sends one Input down the path and returns the reduce's reply. The
// agent's tools, the agent manager and the pollers call it; Command builds
// the owner's. An Input of a host change record goes down the record's path
// (applyRecord).
func (e *Engine) Apply(in core.Input) (any, error) {
	if core.IsRecordKind(in.Kind) {
		ps, err := e.applyRecord(in)
		for _, p := range ps {
			e.publish(p)
		}
		return nil, err
	}
	p, err := e.apply(in, nil)
	if err != nil {
		return nil, err
	}
	e.publish(p)
	return p.res.Reply, nil
}

// applied is one Input that went down the path, ready to publish.
type applied struct {
	reply    api.Reply
	res      core.Result
	threadID string
	dto      *api.Thread
	prev     core.State // the attention clients had before
	evs      []store.Event
	newPR    bool
	replayed bool
	runEnded bool // V0.86: the thread's run ended in this reduce
	slept    bool // V0.176: the thread settled or was archived in this reduce
}

// apply is the one path (decision 0020): replay the receipt, load, reduce,
// store the records, their events, the receipt and the durable effects in
// one transaction. fresh is the new thread of thread.create; a kind that
// Makes its thread gets a new one with the Input's id. A
// thread the reduce leaves as it was is not written and keeps its
// UpdatedAt: an event alone does not move it up the list (#36).
func (e *Engine) apply(in core.Input, fresh *core.Thread) (*applied, error) {
	e.mu.Lock()
	if in.CmdID != "" {
		if r, done := e.replay(in.CmdID); done {
			e.mu.Unlock()
			if r.Status == 500 {
				return nil, errors.New(r.Error)
			}
			return &applied{reply: r, replayed: true}, nil
		}
	}
	cur := fresh
	if cur == nil {
		if cur = e.threads[in.Thread]; cur == nil {
			// A kind that makes its thread (trans-host handover) names the new id.
			if k := core.Kinds[in.Kind]; k == nil || !k.Makes || in.Thread == "" {
				e.mu.Unlock()
				return nil, &core.Rejection{Code: core.RejectNotFound, Reason: "no thread " + in.Thread}
			}
			fresh = &core.Thread{ID: in.Thread}
			cur = fresh
		}
	}
	s, err := e.stage(cur, in, e.Now(), fresh != nil)
	if err != nil {
		e.mu.Unlock()
		return nil, err
	}
	if s.idle() {
		e.mu.Unlock()
		return s.applied, nil
	}
	if _, err := e.st.Write(s.persist); err != nil {
		e.mu.Unlock()
		log.Printf("thread %s: %s: %v", cur.ID, in.Kind, err)
		return nil, err
	}
	e.commit(s, false)
	autoMerge := maps.Clone(e.autoMerge)
	lastAfter, hasLast := e.last[s.t.ID]
	e.mu.Unlock()
	e.checkThreadApplied(s, autoMerge, lastAfter, hasLast) // V0.thread-invariants
	return s.applied, nil
}

// staged is one thread's reduce, ready to store.
type staged struct {
	*applied
	in                             core.Input
	cur, t                         *core.Thread
	prevView, v                    *core.View
	now                            time.Time
	changed, viewChanged, feedOnly bool
	fromEffect                     bool
	doneFx                         string
}

// stage reduces one Input of a thread and works out what to store. Call it
// with e.mu held.
func (e *Engine) stage(cur *core.Thread, in core.Input, now time.Time, fresh bool) (*staged, error) {
	in.Thread = cur.ID
	prevView := cloneViewCopy(e.views[cur.ID], cur.ID)
	t, v, res, err := core.Reduce(cur, e.views[cur.ID], in, now)
	if err != nil {
		return nil, err
	}
	k := core.Kinds[in.Kind]
	if in.Source == core.FromOwner || fresh {
		e.assignProject(t)
	}
	s := &staged{in: in, cur: cur, t: t, prevView: prevView, v: v, now: now}
	was, _ := json.Marshal(cur)
	is, _ := json.Marshal(t)
	s.changed = fresh || string(was) != string(is)
	// The agent's notice queue is not news for the owner (#47): a change of
	// it alone is stored, never broadcast, and keeps UpdatedAt.
	s.feedOnly = s.changed && !fresh && string(sansFeed(cur)) == string(sansFeed(t))
	vwas, _ := json.Marshal(e.views[cur.ID])
	vis, _ := json.Marshal(v)
	s.viewChanged = string(vwas) != string(vis)
	if k.Touch || (s.changed && !s.feedOnly && !k.Untouched) {
		t.UpdatedAt = now
		s.changed, s.feedOnly = true, false
	}
	s.applied = &applied{res: res, threadID: t.ID, reply: api.Reply{OK: true, ThreadID: t.ID, Status: 200, Result: res.Reply}}
	for i := range s.res.Effects {
		ef := &s.res.Effects[i]
		ef.ID = NewID("fx_")
		// V0.33: the message id is known before the agent writes it, so the
		// client can follow its delivery.
		if ef.Kind == core.EffectSend && ef.MessageID == "" {
			ef.MessageID = NewMessageID()
			if res.Reply == nil {
				s.reply.Result = map[string]any{"messageId": ef.MessageID}
			}
		}
	}
	s.runEnded = cur.Running && !t.Running
	s.slept = !cur.Asleep() && t.Asleep()
	s.doneFx, s.fromEffect = strings.CutPrefix(string(in.Source), "effect:")
	return s, nil
}

// idle reports a reduce with nothing to store.
func (s *staged) idle() bool {
	return !s.changed && !s.viewChanged && len(s.res.Events) == 0 && s.in.CmdID == "" && !slices.ContainsFunc(s.res.Effects, core.Effect.Durable) && !s.fromEffect
}

// persist writes a staged reduce in tx: the thread, its view, events,
// durable effects, the applied effect and the receipt.
func (s *staged) persist(tx *store.Tx) error {
	t, from := s.t, len(tx.Events)
	if s.changed {
		if err := tx.PutThread(t); err != nil {
			return err
		}
	}
	if s.viewChanged {
		if err := tx.PutView(s.v); err != nil {
			return err
		}
	}
	for i, ev := range s.res.Events {
		at := s.now
		if i < len(s.res.At) && !s.res.At[i].IsZero() {
			at = s.res.At[i]
		}
		if err := tx.Append(t.ID, ev.Type, ev.Data, at); err != nil {
			return err
		}
	}
	s.evs = tx.Events[from:]
	for _, ef := range s.res.Effects {
		if ef.Durable() {
			if err := tx.PutEffect(ef.ID, t.ID, ef, s.now); err != nil {
				return err
			}
		}
	}
	if s.fromEffect {
		if err := tx.EffectDone(s.doneFx); err != nil {
			return err
		}
	}
	if s.in.CmdID != "" {
		return tx.PutReceipt(s.in.CmdID, s.reply, s.now)
	}
	return nil
}

// commit keeps a stored reduce in memory and builds the DTO to broadcast;
// show builds it also when only a record the thread shows moved. Call it
// with e.mu held.
func (e *Engine) commit(s *staged, show bool) {
	t := s.t
	if s.feedOnly && !s.viewChanged && !show {
		e.threads[t.ID] = t
		return
	}
	if s.cur.ProjectID != t.ProjectID || e.projects[t.ProjectID].Rank() != e.ProjectRank(t.ID) {
		e.threads[t.ID] = t
		e.publishRanksLocked() // V0.project-priority: a new thread, or one filed elsewhere
	}
	if s.changed || s.viewChanged || show {
		s.newPR = hasNewPR(s.cur, t)
		e.threads[t.ID], e.views[t.ID] = t, s.v
		e.bump(t.ID)
		d, prev := e.seen(t, s.now)
		s.dto, s.prev = &d, prev
	}
}

// hasNewPR reports whether t has any non-dismissed PR URL that cur did not
// have yet, so the poller wakes for a second or third PR on the thread too.
func hasNewPR(cur, t *core.Thread) bool {
	seen := map[string]bool{}
	if cur != nil {
		for i := range cur.Changes {
			if c := &cur.Changes[i]; c.PR != nil && !c.Dismissed && c.PR.URL != "" {
				seen[c.PR.URL] = true
			}
		}
	}
	for i := range t.Changes {
		if c := &t.Changes[i]; c.PR != nil && !c.Dismissed && c.PR.URL != "" && !seen[c.PR.URL] {
			return true
		}
	}
	return false
}

// sansFeed is a thread as JSON without its notice queue.
func sansFeed(t *core.Thread) []byte {
	c := *t
	c.Feed = core.Feed{}
	b, _ := json.Marshal(&c)
	return b
}

// publish broadcasts what apply stored, then starts its effects. Call it
// without e.mu held.
func (e *Engine) publish(p *applied) {
	if p.replayed {
		return
	}
	if p.dto != nil {
		e.Hub.Send(stream.FrameThread, *p.dto)
	}
	e.sendEvents(p.evs)
	if p.dto != nil {
		e.notify(*p.dto, p.prev)
	}
	e.publishQuotaResets(p)
	if p.newPR && e.OnPR != nil {
		e.OnPR(p.threadID)
	}
	if p.runEnded {
		e.slotRunEnded(p.threadID)
	}
	if (p.runEnded || p.slept) && e.LeftoverSweep != nil {
		go e.LeftoverSweep() // V0.176: it may wait seconds for a process to end
	}
	e.start(p.threadID, p.res.Effects)
	if p.threadID != "" {
		e.autoApprove(p.threadID) // V0.102
		e.autoDeploy(p.threadID)  // V0.111
	}
}

// record appends events to a thread without changing it.
func (e *Engine) record(threadID string, evs ...core.Event) {
	e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindRecord, Thread: threadID, Args: evs})
}
