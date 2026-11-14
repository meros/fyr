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
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/attach"
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
	deploy *deployState // V0.39
	mu       sync.Mutex
	st       *store.Store
	threads  map[string]*core.Thread
	views    map[string]*core.View
	last     map[string]core.State    // last broadcast state, for time-driven changes
	revs     map[string]int64         // per-thread rev (SPEC 0.5 V0.2)
	projects map[string]*core.Project // V0.23
	// workspaces are where threads run (V0.28), by id.
	workspaces map[string]*core.Workspace
	// ScratchRoots are the scratch folders' roots (V0.28); nil: the defaults.
	ScratchRoots []string
	// ScratchDir is where thread.create makes a scratch folder (V0.42);
	// "": ~/.local/share/fyr/scratch.
	ScratchDir string
	// repoOf is the normalised origin of each project folder on this host
	// (V0.42), "" for none; RefreshRepos fills it.
	repoOf map[string]string
	// HostID names this host in folder sources (V0.23); default the hostname.
	HostID  string
	lastRev int64

	Agents  Agents
	Effects EffectRunner
	Hub     *Hub
	Now     func() time.Time
	// Adopt handles t3.adopt; set by main.
	Adopt func(ids []string) (int, error)
	// Attach stores files for the show tool (V0.19); Shooter takes screenshots.
	Attach  *attach.Store
	Shooter Shooter
	// Limits for agent tools live on the engine, not on one tool registration.
	shotLimit showLimiter
	newLimit  hourly
	sendLimit hourly
	// AttachmentExists checks ids in thread.create and thread.send (V0.18).
	AttachmentExists func(id string) bool
	// OnPR is called when a thread gets a new PR, so the poller looks now.
	OnPR func(threadID string)
	// OnAttention is called, outside the lock, when a thread's attention
	// state changes (push, SPEC 0.5 V0.15). prev is "" for a new thread.
	OnAttention func(d DTO, prev core.State)
}

func New(st *store.Store) *Engine {
	return &Engine{st: st, threads: map[string]*core.Thread{}, views: map[string]*core.View{},
		last: map[string]core.State{}, revs: map[string]int64{}, projects: map[string]*core.Project{}, workspaces: map[string]*core.Workspace{}, HostID: hostname(), Hub: NewHub(), Now: time.Now,
		newLimit: hourly{max: 5}, sendLimit: hourly{max: 30}}
}

func NewID(prefix string) string {
	b := make([]byte, 8)
	rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// NewMessageID is a random UUID; the CLI takes it as a user message's uuid
// (SPEC 0.5 V0.33).
func NewMessageID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
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
		e.bump(t.ID)
	}
	e.views = vs
	ps, err := e.st.Projects()
	if err != nil {
		return err
	}
	for _, p := range ps {
		e.projects[p.ID] = p
	}
	ws, err := e.st.Workspaces()
	if err != nil {
		return err
	}
	for _, w := range ws {
		e.workspaces[w.ID] = w
	}
	if err := e.migrateAuto(); err != nil {
		return err
	}
	// Threads from before workspaces (V0.28) get one now; a thread without
	// a project gets the one matching finds, if any.
	for _, t := range e.threads {
		if e.workspaces[t.WorkspaceID] != nil && (t.ProjectID == "" || e.projects[t.ProjectID] != nil) {
			if p := e.projects[t.ProjectID]; p != nil {
				t.Project = p.Name
			}
			continue
		}
		nw := e.assignProject(t, e.Now())
		if _, err := e.st.Write(func(tx *store.Tx) error {
			if nw != nil {
				if err := tx.PutWorkspace(nw); err != nil {
					return err
				}
			}
			return tx.PutThread(t)
		}); err != nil {
			return err
		}
	}
	// Clients and push already know these states; only changes notify.
	defer func() {
		now := e.Now()
		for id, t := range e.threads {
			e.last[id] = core.Compute(t, e.view(id), now).State
		}
	}()
	now := e.Now()
	for _, t := range e.threads {
		// V0.33: the agent's queue died with the old host; unread messages
		// are lost, never resent by themselves.
		if len(t.Outbox) == 0 {
			continue
		}
		lost := t.Outbox
		t.Outbox = nil
		if _, err := e.st.Write(func(tx *store.Tx) error {
			if err := tx.PutThread(t); err != nil {
				return err
			}
			for _, m := range lost {
				if err := tx.Append(t.ID, "message.state", map[string]any{"id": m.ID, "state": "lost"}, now); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	for _, t := range e.threads {
		if !t.Running && !hasLiveDecision(t) && t.RunningTasks() == 0 {
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

// bump gives a thread a new rev: wall-clock nanoseconds, strictly increasing,
// so it also increases across restarts. Call with e.mu held.
func (e *Engine) bump(id string) {
	r := time.Now().UnixNano()
	if r <= e.lastRev {
		r = e.lastRev + 1
	}
	e.lastRev = r
	e.revs[id] = r
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
	if strings.HasPrefix(c.Action, "project.") {
		return e.projectCommand(c)
	}
	if c.Action == "thread.cancelMessage" {
		return e.cancelMessage(c)
	}
	act := core.Registry[c.Action]
	if act == nil {
		return Reply{Error: "unknown action " + c.Action, Status: 400}
	}
	if c.Action == "thread.setProject" && c.Args.Str("projectId") != "" {
		e.mu.Lock()
		ok := e.projects[c.Args.Str("projectId")] != nil
		e.mu.Unlock()
		if !ok {
			return Reply{Error: "no project " + c.Args.Str("projectId"), Status: 400}
		}
	}
	if c.Action == "thread.create" || c.Action == "thread.send" {
		for _, id := range core.AttachmentIDs(c.Args) {
			if e.AttachmentExists == nil || !e.AttachmentExists(id) {
				return Reply{Error: "unknown attachment " + id, Status: 400}
			}
		}
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
	// V0.42: thread.create files into projectId; without a cwd it starts in
	// the project's main folder here, else in a fresh scratch folder.
	var fileIn, scratch string
	if c.Action == "thread.create" {
		if pid := c.Args.Str("projectId"); pid != "" {
			pr := e.projects[pid]
			if pr == nil {
				e.mu.Unlock()
				return Reply{Error: "no project " + pid, Status: 400}
			}
			fileIn = pid
			if strings.TrimSpace(c.Args.Str("cwd")) == "" {
				if f := pr.Folder(e.HostID); f != "" {
					c.Args["cwd"] = f
				}
			}
		}
		if strings.TrimSpace(c.Args.Str("cwd")) == "" {
			dir, err := e.makeScratch(c.Args.Str("text"), now)
			if err != nil {
				e.mu.Unlock()
				return Reply{Error: "could not make a scratch folder: " + err.Error(), Status: 500}
			}
			scratch = dir
			c.Args["cwd"] = dir
		}
	}
	effects, err := act.Apply(t, &v, c.Args, now)
	if err == nil && fileIn != "" {
		t.ProjectID, t.ProjectManual = fileIn, true
	}
	if err != nil && scratch != "" {
		os.Remove(scratch)
	}
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
	newWS := e.assignProject(t, now)
	t.UpdatedAt = now
	reply := Reply{OK: true, ThreadID: t.ID, Status: 200}
	for i := range effects {
		// V0.33: the message id is known before the agent writes it, so the
		// client can follow its delivery.
		if effects[i].Kind == "send" && effects[i].MessageID == "" {
			effects[i].MessageID = NewMessageID()
			reply.Result = map[string]any{"messageId": effects[i].MessageID}
		}
	}
	evs, err := e.st.Write(func(tx *store.Tx) error {
		if newWS != nil {
			if err := tx.PutWorkspace(newWS); err != nil {
				return err
			}
		}
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
	e.bump(t.ID)
	dto := e.dto(t, now)
	e.mu.Unlock()
	e.Hub.Thread(dto)
	e.Hub.Events(evs)
	e.notify(dto)
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

// EffectTimeout is the deadline of one approved effect. A deploy builds,
// tests and then drains for up to 30 minutes (SPEC 0.5 V0.13).
func EffectTimeout(action string) time.Duration {
	if action == "fyr.deploy" {
		return 60 * time.Minute
	}
	return 10 * time.Minute
}

// runEffect runs an approved or declined async effect and reports the result
// to the thread as context for the agent (SPEC 5.10, 0.5 V0.12).
func (e *Engine) runEffect(threadID string, ef core.Effect) {
	req := ef.Request
	if req == nil {
		return
	}
	if ef.ByOwner {
		e.runOwnerEffect(threadID, *req)
		return
	}
	ok, text := false, "The user declined it."
	if ef.Allow {
		if e.Effects == nil {
			text = "No effect runner is configured."
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), EffectTimeout(req.Action))
			out, err := e.Effects.Run(ctx, threadID, *req)
			cancel()
			if errors.Is(err, core.ErrReportLater) {
				return
			}
			ok, text = err == nil, out
			if err != nil {
				text = err.Error()
			}
		}
	}
	e.Report(threadID, *req, ef.Allow, ok, text)
}

// runOwnerEffect runs a PR action the owner started in the PR tab (SPEC 0.5
// V0.43). The result is a line in the thread; the agent is not woken. The
// poller looks at the PR again at once.
func (e *Engine) runOwnerEffect(threadID string, req core.EffectReq) {
	ok, text := false, "No effect runner is configured."
	if e.Effects != nil {
		ctx, cancel := context.WithTimeout(context.Background(), EffectTimeout(req.Action))
		out, err := e.Effects.Run(ctx, threadID, req)
		cancel()
		ok, text = err == nil, out
		if err != nil {
			text = err.Error()
		}
	}
	text = ownerLine(req, ok, text)
	e.Observe(threadID, func(*core.Thread, time.Time) {}, agent.Ev{Type: "effect.result",
		Data: map[string]any{"action": req.Action, "args": req.Args, "ok": ok, "text": text, "by": "owner"}})
	if e.OnPR != nil {
		e.OnPR(threadID)
	}
}

// ownerLine starts an owner effect's result with one plain sentence.
func ownerLine(req core.EffectReq, ok bool, text string) string {
	url, _ := req.Args["pr"].(string)
	pr := "the PR"
	if _, n, found := agent.ParsePR(url); found {
		pr = fmt.Sprintf("#%d", n)
	}
	var first string
	switch {
	case req.Action == "pr.merge" && ok:
		method, _ := req.Args["method"].(string)
		first = fmt.Sprintf("You merged %s (%s).", pr, method)
	case req.Action == "pr.merge":
		first = fmt.Sprintf("Merging %s failed.", pr)
	case req.Action == "pr.rerunFailed" && ok:
		first = fmt.Sprintf("You re-ran the failed checks of %s.", pr)
	case req.Action == "pr.rerunFailed":
		first = fmt.Sprintf("Re-running the failed checks of %s failed.", pr)
	default:
		return text
	}
	if strings.TrimSpace(text) == "" {
		return first
	}
	return first + "\n\n" + strings.TrimSpace(text)
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
	e.bump(threadID)
	dto := e.dto(t, now)
	e.mu.Unlock()
	e.Hub.Thread(dto)
	e.Hub.Events(written)
	e.notify(dto)
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
	Rev         int64             `json:"rev"`
	Attention   core.Attention    `json:"attention"`
	View        *core.View        `json:"view"`
	Unavailable map[string]string `json:"unavailable"`
	// Suggest is the project a thread without one could get (V0.28).
	Suggest *core.Suggestion `json:"suggest,omitempty"`
	// NewProject is what "New project from this thread" starts with (V0.30).
	NewProject *core.Suggestion `json:"newProject,omitempty"`

	prev core.State // the state before this change; not sent
}

// notify reports an attention change; call it without e.mu held.
func (e *Engine) notify(d DTO) {
	if e.OnAttention != nil && d.Attention.State != d.prev {
		e.OnAttention(d, d.prev)
	}
}

func (e *Engine) dto(t *core.Thread, now time.Time) DTO {
	d := e.build(t, now)
	d.prev = e.last[t.ID]
	e.last[t.ID] = d.Attention.State
	return d
}

// build makes a DTO without recording it as seen by clients.
func (e *Engine) build(t *core.Thread, now time.Time) DTO {
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
	vc := *v
	return DTO{Thread: t, Rev: e.revs[t.ID], Attention: att, View: &vc, Unavailable: un, Suggest: e.suggestion(t), NewProject: e.draft(t)}
}

// Snapshot returns every thread as DTOs, most recently updated first.
func (e *Engine) Snapshot() []DTO {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.Now()
	out := make([]DTO, 0, len(e.threads))
	for _, t := range e.threads {
		out = append(out, e.build(clone(t), now))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// Running lists the threads with a live run or a running background task;
// a deploy waits for all of them (SPEC 0.5 V0.13, V0.16).
func (e *Engine) Running() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for id, t := range e.threads {
		if t.Running || t.RunningTasks() > 0 {
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
			e.bump(id)
			changed = append(changed, e.dto(clone(t), now))
		}
	}
	e.mu.Unlock()
	for _, d := range changed {
		e.Hub.Thread(d)
		e.notify(d)
	}
}

// EventsBefore pages back: the newest limit events older than before.
func (e *Engine) EventsBefore(id string, before int64, limit int) ([]store.Event, error) {
	return e.st.EventsBefore(id, before, limit)
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
	e.mu.Lock()
	nw := e.assignProject(t, e.Now())
	e.mu.Unlock()
	if _, err := e.st.Write(func(tx *store.Tx) error {
		if nw != nil {
			if err := tx.PutWorkspace(nw); err != nil {
				return err
			}
		}
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
	e.bump(t.ID)
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
