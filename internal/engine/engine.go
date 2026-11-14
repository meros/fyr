// Package engine is the host's one command path (SPEC 5, 0.5 V0.2): every
// Action from every surface goes through Command; every fact from agents and
// GitHub goes through Observe. Both persist and broadcast in one place.
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

// Agents runs agent effects. *agent.Manager implements it.
type Agents interface {
	Do(threadID string, e core.Effect) error
}

// EffectRunner performs an approved async effect (SPEC 5.10), e.g. pr.merge.
type EffectRunner interface {
	Run(ctx context.Context, threadID string, req core.EffectReq) (string, error)
}

type Engine struct {
	mu      sync.Mutex
	st      *store.Store
	threads map[string]*core.Thread
	views   map[string]*core.View
	last    map[string]core.State // last broadcast state, for time-driven changes

	Agents  Agents
	Effects EffectRunner
	Hub     *Hub
	Now     func() time.Time
	// Adopt handles t3.adopt; set by main.
	Adopt func(ids []string) (int, error)
	// OnPR is called when a thread gets a new PR, so the poller looks now.
	OnPR func(threadID string)
}

func New(st *store.Store) *Engine {
	return &Engine{st: st, threads: map[string]*core.Thread{}, views: map[string]*core.View{},
		last: map[string]core.State{}, Hub: NewHub(), Now: time.Now}
}

func NewID(prefix string) string {
	b := make([]byte, 8)
	rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// Load reads the projections and repairs live work that the last run of the
// host left behind (SPEC 0.5 V0.6): it becomes failed with class restart.
func (e *Engine) Load() error {
	ts, err := e.st.Threads()
	if err != nil {
		return err
	}
	vs, err := e.st.Views(core.Owner)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, t := range ts {
		e.threads[t.ID] = t
	}
	e.views = vs
	now := e.Now()
	for _, t := range e.threads {
		if !t.Running && !hasLiveDecision(t) {
			continue
		}
		msg := "Fyr restarted during the run"
		if hasLiveDecision(t) {
			msg = "Fyr restarted while a question was open; Retry asks again"
		}
		keep := effectDecisions(t)
		core.ProcessLost(t, "restart", msg, now)
		t.Decisions = keep // effect Decisions do not need the process (5.10)
		if _, err := e.st.Write(func(tx *store.Tx) error {
			if err := tx.PutThread(t); err != nil {
				return err
			}
			return tx.Append(t.ID, "run.ended", map[string]any{"failure": t.Failure}, now)
		}); err != nil {
			return err
		}
	}
	return nil
}

// hasLiveDecision is true when a Decision needs the agent process.
func hasLiveDecision(t *core.Thread) bool {
	for _, d := range t.Decisions {
		if d.Kind != "effect" {
			return true
		}
	}
	return false
}

func effectDecisions(t *core.Thread) []core.Decision {
	var out []core.Decision
	for _, d := range t.Decisions {
		if d.Kind == "effect" {
			out = append(out, d)
		}
	}
	return out
}

func (e *Engine) view(id string) *core.View {
	v := e.views[id]
	if v == nil {
		v = &core.View{User: core.Owner, Entity: id}
		e.views[id] = v
	}
	return v
}

// Cmd is one command from any surface.
type Cmd struct {
	CommandID string    `json:"commandId"`
	Action    string    `json:"action"`
	ThreadID  string    `json:"threadId,omitempty"`
	Args      core.Args `json:"args,omitempty"`
}

type Reply struct {
	OK       bool   `json:"ok"`
	ThreadID string `json:"threadId,omitempty"`
	Error    string `json:"error,omitempty"`
	Status   int    `json:"-"`
	Result   any    `json:"result,omitempty"`
}

func clone(t *core.Thread) *core.Thread {
	b, _ := json.Marshal(t)
	c := &core.Thread{}
	json.Unmarshal(b, c)
	return c
}

// Command applies one Action exactly once per CommandID (I7).
func (e *Engine) Command(c Cmd) Reply {
	if c.CommandID == "" {
		return Reply{Error: "commandId is required", Status: 400}
	}
	if c.Action == "t3.adopt" {
		return e.adopt(c)
	}
	act := core.Registry[c.Action]
	if act == nil {
		return Reply{Error: "unknown action " + c.Action, Status: 400}
	}
	e.mu.Lock()
	if prev, err := e.st.Receipt(c.CommandID); err != nil {
		e.mu.Unlock()
		return Reply{Error: err.Error(), Status: 500}
	} else if prev != nil {
		e.mu.Unlock()
		var r Reply
		json.Unmarshal(prev, &r)
		r.Status = 200
		return r
	}
	now := e.Now()
	var t *core.Thread
	if c.Action == "thread.create" {
		t = &core.Thread{ID: NewID("t_"), Owner: core.Owner, Visibility: "private", CreatedAt: now, Source: "fyr"}
	} else {
		cur := e.threads[c.ThreadID]
		if cur == nil {
			e.mu.Unlock()
			return Reply{Error: "no thread " + c.ThreadID, Status: 404}
		}
		t = clone(cur)
	}
	v := *e.view(t.ID)
	v.Entity = t.ID
	if r := act.Available(t, &v, now); r != "" {
		e.mu.Unlock()
		return Reply{Error: r, Status: 409} // transient: no receipt (A4)
	}
	if c.Args == nil {
		c.Args = core.Args{}
	}
	effects, err := act.Apply(t, &v, c.Args, now)
	if err != nil {
		e.mu.Unlock()
		status := 400
		if !errors.Is(err, core.ErrBadArgs) {
			status = 409
		}
		return Reply{Error: err.Error(), Status: status}
	}
	if t.Profile == "" {
		t.Profile = "claude"
	}
	t.UpdatedAt = now
	reply := Reply{OK: true, ThreadID: t.ID, Status: 200}
	evs, err := e.st.Write(func(tx *store.Tx) error {
		if err := tx.PutThread(t); err != nil {
			return err
		}
		if err := tx.PutView(&v); err != nil {
			return err
		}
		if err := tx.Append(t.ID, "command", map[string]any{"action": c.Action, "commandId": c.CommandID}, now); err != nil {
			return err
		}
		return tx.PutReceipt(c.CommandID, reply, now)
	})
	if err != nil {
		e.mu.Unlock()
		return Reply{Error: err.Error(), Status: 500}
	}
	e.threads[t.ID], e.views[t.ID] = t, &v
	dto := e.dto(t, now)
	e.mu.Unlock()
	e.Hub.Thread(dto)
	e.Hub.Events(evs)
	if len(effects) > 0 {
		go e.run(t.ID, effects)
	}
	return reply
}

func (e *Engine) run(threadID string, effects []core.Effect) {
	for _, ef := range effects {
		if ef.Kind == "effect" {
			e.runEffect(threadID, ef)
			continue
		}
		if e.Agents == nil {
			continue
		}
		if err := e.Agents.Do(threadID, ef); err != nil {
			log.Printf("thread %s: effect %s: %v", threadID, ef.Kind, err)
			e.Observe(threadID, func(*core.Thread, time.Time) {}, agent.Ev{Type: "effect.error",
				Data: map[string]any{"kind": ef.Kind, "error": err.Error()}})
		}
	}
}

// runEffect runs an approved or declined async effect and reports the result
// to the thread as context for the agent (SPEC 5.10, 0.5 V0.12).
func (e *Engine) runEffect(threadID string, ef core.Effect) {
	req := ef.Request
	if req == nil {
		return
	}
	ok, text := false, "The user declined it."
	if ef.Allow {
		if e.Effects == nil {
			text = "No effect runner is configured."
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			out, err := e.Effects.Run(ctx, threadID, *req)
			cancel()
			ok, text = err == nil, out
			if err != nil {
				text = err.Error()
			}
		}
	}
	e.Report(threadID, *req, ef.Allow, ok, text)
}

// Report records an effect result and sends it to the agent as context.
func (e *Engine) Report(threadID string, req core.EffectReq, allowed, ok bool, text string) {
	e.Observe(threadID, func(*core.Thread, time.Time) {}, agent.Ev{Type: "effect.result",
		Data: map[string]any{"action": req.Action, "args": req.Args, "ok": ok, "text": text}})
	t, found := e.Thread(threadID)
	if !found || t.ArchivedAt != nil || t.SettledAt != nil || e.Agents == nil {
		return
	}
	status := "failed"
	switch {
	case ok:
		status = "succeeded"
	case !allowed:
		status = "was declined by the user"
	}
	b, _ := json.Marshal(req.Args)
	msg := fmt.Sprintf("[Fyr] Your effect_request %s %s %s.\n%s", req.Action, string(b), status, text)
	if err := e.Agents.Do(threadID, core.Effect{Kind: "send", Text: msg}); err != nil {
		log.Printf("thread %s: deliver effect result: %v", threadID, err)
	}
}

// Observe applies a fact from an agent or a poller and appends its events.
func (e *Engine) Observe(threadID string, fn func(t *core.Thread, now time.Time), evs ...agent.Ev) {
	e.mu.Lock()
	cur := e.threads[threadID]
	if cur == nil {
		e.mu.Unlock()
		return
	}
	t := clone(cur)
	now := e.Now()
	hadPR := t.PR != nil
	fn(t, now)
	t.UpdatedAt = now
	written, err := e.st.Write(func(tx *store.Tx) error {
		if err := tx.PutThread(t); err != nil {
			return err
		}
		for _, ev := range evs {
			if err := tx.Append(threadID, ev.Type, ev.Data, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		e.mu.Unlock()
		log.Printf("observe %s: %v", threadID, err)
		return
	}
	e.threads[threadID] = t
	dto := e.dto(t, now)
	e.mu.Unlock()
	e.Hub.Thread(dto)
	e.Hub.Events(written)
	if !hadPR && t.PR != nil && e.OnPR != nil {
		e.OnPR(threadID)
	}
}

// Thread returns a copy of one thread.
func (e *Engine) Thread(id string) (core.Thread, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.threads[id]
	if t == nil {
		return core.Thread{}, false
	}
	return *clone(t), true
}

// DTO is a thread as clients see it (docs/API-v0.md).
type DTO struct {
	*core.Thread
	Attention   core.Attention    `json:"attention"`
	View        *core.View        `json:"view"`
	Unavailable map[string]string `json:"unavailable"`
}

func (e *Engine) dto(t *core.Thread, now time.Time) DTO {
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
	att := core.Compute(t, v, now)
	e.last[t.ID] = att.State
	vc := *v
	return DTO{Thread: t, Attention: att, View: &vc, Unavailable: un}
}

// Snapshot returns every thread as DTOs, most recently updated first.
func (e *Engine) Snapshot() []DTO {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.Now()
	out := make([]DTO, 0, len(e.threads))
	for _, t := range e.threads {
		out = append(out, e.dto(clone(t), now))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// Running lists the threads with a live run.
func (e *Engine) Running() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for id, t := range e.threads {
		if t.Running {
			out = append(out, id)
		}
	}
	return out
}

// Tick broadcasts threads whose attention changed with time alone (a snooze
// ended). Call it every few seconds.
func (e *Engine) Tick() {
	e.mu.Lock()
	now := e.Now()
	var changed []DTO
	for id, t := range e.threads {
		if core.Compute(t, e.view(id), now).State != e.last[id] {
			changed = append(changed, e.dto(clone(t), now))
		}
	}
	e.mu.Unlock()
	for _, d := range changed {
		e.Hub.Thread(d)
	}
}

// Events reads the timeline of one thread; after == 0 gives the newest limit.
func (e *Engine) Events(id string, after int64, limit int) ([]store.Event, error) {
	return e.st.EventsFor(id, after, limit, after == 0)
}

func (e *Engine) adopt(c Cmd) Reply {
	if e.Adopt == nil {
		return Reply{Error: "T3 adoption is not configured", Status: 409}
	}
	if prev, _ := e.st.Receipt(c.CommandID); prev != nil {
		var r Reply
		json.Unmarshal(prev, &r)
		r.Status = 200
		return r
	}
	raw, _ := c.Args["t3Ids"].([]any)
	ids := []string{}
	for _, x := range raw {
		if s, ok := x.(string); ok {
			ids = append(ids, s)
		}
	}
	n, err := e.Adopt(ids)
	if err != nil {
		return Reply{Error: err.Error(), Status: 500}
	}
	r := Reply{OK: true, Status: 200, Result: map[string]any{"adopted": n}}
	e.st.Write(func(tx *store.Tx) error { return tx.PutReceipt(c.CommandID, r, e.Now()) })
	return r
}

// Insert adds a thread made outside the command path (T3 adoption), with
// its history, in one transaction.
func (e *Engine) Insert(t *core.Thread, v *core.View, history []agent.Ev, at []time.Time) error {
	if _, err := e.st.Write(func(tx *store.Tx) error {
		if err := tx.PutThread(t); err != nil {
			return err
		}
		if err := tx.PutView(v); err != nil {
			return err
		}
		for i, ev := range history {
			if err := tx.Append(t.ID, ev.Type, ev.Data, at[i]); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	e.mu.Lock()
	e.threads[t.ID], e.views[t.ID] = t, v
	dto := e.dto(t, e.Now())
	e.mu.Unlock()
	e.Hub.Thread(dto)
	return nil
}

// HasT3 reports whether a T3 thread was already adopted.
func (e *Engine) HasT3(t3ID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, t := range e.threads {
		if t.T3ID == t3ID {
			return true
		}
	}
	return false
}
