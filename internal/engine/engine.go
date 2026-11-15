// Package engine is the host's one path for state (SPEC 5, 0.5 V0.2,
// decision 0020): every change to a thread record is a core.Input, reduced
// by core.Reduce and stored, broadcast and acted on in apply. Command builds
// the owner's Inputs; the agent's tools, the agent manager and the pollers
// call Apply. The effect runner (effects.go) runs what a reduce returns.
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/api"
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
	mu     sync.Mutex
	// press serializes the commands that call out of the engine while they
	// run (message cancels, T3 adoption), so the receipt check, the work
	// and the receipt are one step. Everything else is one step under mu
	// in apply.
	press    sync.Mutex
	st       *store.Store
	threads  map[string]*core.Thread
	views    map[string]*core.View
	last     map[string]core.State    // last broadcast state, for time-driven changes
	revs     map[string]int64         // per-thread rev (SPEC 0.5 V0.2)
	projects map[string]*core.Project // V0.23
	// records are the host change records (#35): observed branches.
	records map[string]*core.Change

	// quietSent is the quiet hours the clients last got, per project (V0.46).
	quietSent map[string]string
	// ScratchRoots are the scratch folders' roots (V0.28): their threads get
	// no "New project from this thread"; nil: the defaults.
	ScratchRoots []string
	// ScratchDir is where thread.create makes a scratch folder (V0.42);
	// "": ~/.local/share/fyr/scratch.
	ScratchDir string
	// HostID names this host in folder sources (V0.23); default the hostname.
	HostID  string
	lastRev int64

	Agents  Agents
	Effects EffectRunner
	// Git reads checkouts for the git observer (#35); nil reads with git.
	Git GitReader
	// Scripts runs script actions of changes, the thread and its view (V0.60);
	// nil runs them with exec.
	Scripts ScriptRunner
	Hub     *Hub
	Now     func() time.Time
	// Adopt handles t3.adopt; set by main.
	Adopt func(ids []string) (int, error)
	// Attach stores files for the show tool (V0.19); Shooter takes screenshots.
	Attach  *attach.Store
	Shooter Shooter
	// Limits for agent tools live on the engine, not on one tool registration.
	shotLimit window
	newLimit  window
	sendLimit window
	// AttachmentExists checks ids in thread.create and thread.send (V0.18).
	AttachmentExists func(id string) bool
	// ProfileExists checks the target of thread.handover (V0.45); nil
	// accepts any.
	ProfileExists func(id string) bool
	// OnPR is called when a thread gets a new PR, so the poller looks now.
	OnPR func(threadID string)
	// OnAttention is called, outside the lock, when a thread's attention
	// state changes (push, SPEC 0.5 V0.15). prev is "" for a new thread.
	OnAttention func(d DTO, prev core.State)
}

func New(st *store.Store) *Engine {
	return &Engine{st: st, threads: map[string]*core.Thread{}, views: map[string]*core.View{},
		last: map[string]core.State{}, revs: map[string]int64{}, projects: map[string]*core.Project{}, records: map[string]*core.Change{}, quietSent: map[string]string{}, HostID: hostname(), Hub: NewHub(), Now: time.Now,
		shotLimit: window{max: 10, span: time.Minute}, newLimit: window{max: 5, span: time.Hour}, sendLimit: window{max: 30, span: time.Hour}}
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
	if err := e.loadRecords(); err != nil {
		return err
	}
	ps, err := e.st.Projects()
	if err != nil {
		return err
	}
	for _, p := range ps {
		e.projects[p.ID] = p
	}
	old, err := e.st.OldProjectSources()
	if err != nil {
		return err
	}
	if err := e.migrateFolders(old); err != nil {
		return err
	}
	if err := e.migrateAuto(); err != nil {
		return err
	}
	// V0.55: threads land in the project whose folder covers theirs; a
	// thread whose project is gone loses it. Only changed threads are
	// written.
	for _, t := range e.threads {
		pid, manual := t.ProjectID, t.ProjectManual
		e.assignProject(t)
		if t.ProjectID == pid && t.ProjectManual == manual {
			continue
		}
		if _, err := e.st.Write(func(tx *store.Tx) error { return tx.PutThread(t) }); err != nil {
			return err
		}
	}
	ids := slices.Collect(maps.Keys(e.threads))
	e.mu.Unlock()
	// V0.6, V0.33, V0.45: repair what the last run left, through the path
	// (no broadcast: no client is connected yet).
	for _, id := range ids {
		if _, err := e.apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindHostRestarted, Thread: id}, nil); err != nil {
			e.mu.Lock()
			return err
		}
	}
	e.mu.Lock()
	// Clients and push already know these states; only changes notify.
	now := e.Now()
	for id, t := range e.threads {
		e.last[id] = core.Compute(t, e.view(id), now).State
	}
	return nil
}

// bump gives a thread a new rev. Call with e.mu held.
func (e *Engine) bump(id string) { e.revs[id] = e.nextRev() }

// nextRev is a new rev: wall-clock nanoseconds, strictly increasing, so it
// also increases across restarts. Call with e.mu held.
func (e *Engine) nextRev() int64 {
	r := time.Now().UnixNano()
	if r <= e.lastRev {
		r = e.lastRev + 1
	}
	e.lastRev = r
	return r
}

func (e *Engine) view(id string) *core.View {
	v := e.views[id]
	if v == nil {
		v = &core.View{User: core.Owner, Entity: id}
		e.views[id] = v
	}
	return v
}

// ChangesLook returns a copy of the owner's last look at the thread's
// Changes tab and the Viewed marks (V0.65), or nil.
func (e *Engine) ChangesLook(id string) *core.ChangesLook {
	e.mu.Lock()
	defer e.mu.Unlock()
	v := e.views[id]
	if v == nil || v.ChangesLook == nil {
		return nil
	}
	l := *v.ChangesLook
	l.Files = maps.Clone(l.Files)
	l.Viewed = maps.Clone(l.Viewed)
	return &l
}

// Command applies one owner command exactly once per CommandID (I7). It
// builds an Input and sends it down the one path; projects, T3 adoption and
// a message cancel keep their own (they wait on things outside the thread).
func (e *Engine) Command(c api.Cmd) api.Reply {
	if c.CommandID == "" {
		return api.Reply{Error: "commandId is required", Status: 400}
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
	if k := core.Kinds[c.Action]; k == nil || !slices.Contains(k.From, core.ByOwner) {
		return api.Reply{Error: "unknown action " + c.Action, Status: 400}
	}
	if c.Action == "thread.setProject" && c.Args.Str("projectId") != "" {
		e.mu.Lock()
		ok := e.projects[c.Args.Str("projectId")] != nil
		e.mu.Unlock()
		if !ok {
			return api.Reply{Error: "no project " + c.Args.Str("projectId"), Status: 400}
		}
	}
	if c.Action == "thread.handover" && e.ProfileExists != nil && c.Args.Str("profile") != "" && !e.ProfileExists(c.Args.Str("profile")) {
		return api.Reply{Error: "no profile " + c.Args.Str("profile"), Status: 400}
	}
	if c.Action == "thread.create" || c.Action == "thread.send" {
		for _, id := range core.AttachmentIDs(c.Args) {
			if e.AttachmentExists == nil || !e.AttachmentExists(id) {
				return api.Reply{Error: "unknown attachment " + id, Status: 400}
			}
		}
	}
	if c.Args == nil {
		c.Args = core.Args{}
	}
	var fresh *core.Thread
	var scratch string
	if c.Action == "thread.create" {
		if r, done := e.replay(c.CommandID); done {
			return r
		}
		now := e.Now()
		fresh = &core.Thread{ID: NewID("t_"), Owner: core.Owner, Visibility: "private", CreatedAt: now, Source: "fyr"}
		// V0.42: thread.create files into projectId; without a cwd it starts
		// in the project's folder (V0.55), else in a fresh scratch folder.
		args := maps.Clone(c.Args)
		if pid := args.Str("projectId"); pid != "" {
			e.mu.Lock()
			pr := e.projects[pid]
			e.mu.Unlock()
			if pr == nil {
				return api.Reply{Error: "no project " + pid, Status: 400}
			}
			if strings.TrimSpace(args.Str("cwd")) == "" && pr.Folder != "" {
				args["cwd"] = pr.Folder
			}
		}
		if strings.TrimSpace(args.Str("cwd")) == "" {
			dir, err := e.makeScratch(args.Str("text"), now)
			if err != nil {
				return api.Reply{Error: "could not make a scratch folder: " + err.Error(), Status: 500}
			}
			scratch = dir
			args["cwd"] = dir
		}
		c.Args = args
	}
	p, err := e.apply(core.Input{Source: core.FromOwner, Kind: c.Action, Thread: c.ThreadID, Args: c.Args, CmdID: c.CommandID}, fresh)
	if scratch != "" && (err != nil || p.replayed) {
		os.Remove(scratch)
	}
	if err != nil {
		return rejected(err)
	}
	e.publish(p)
	return p.reply
}

// rejected is a command's reply for an Input that did not apply.
func rejected(err error) api.Reply {
	var r *core.Rejection
	if !errors.As(err, &r) {
		return api.Reply{Error: err.Error(), Status: 500}
	}
	status := map[core.RejectCode]int{core.RejectBadArgs: 400, core.RejectNotFound: 404, core.RejectRights: 403}[r.Code]
	if status == 0 {
		status = 409 // not now; a transient rejection gets no receipt (A4)
	}
	return api.Reply{Error: r.Reason, Status: status}
}

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
	dto      *DTO
	evs      []store.Event
	newPR    bool
	replayed bool
}

// apply is the one path (decision 0020): replay the receipt, load, reduce,
// store the records, their events, the receipt and the durable effects in
// one transaction. fresh is the new thread of a kind that makes one. A
// thread the reduce leaves as it was is not written and keeps its
// UpdatedAt: an event alone does not move it up the list (#36).
func (e *Engine) apply(in core.Input, fresh *core.Thread) (*applied, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if in.CmdID != "" {
		if r, done := e.replay(in.CmdID); done {
			if r.Status == 500 {
				return nil, errors.New(r.Error)
			}
			return &applied{reply: r, replayed: true}, nil
		}
	}
	cur := fresh
	if cur == nil {
		if cur = e.threads[in.Thread]; cur == nil {
			return nil, &core.Rejection{Code: core.RejectNotFound, Reason: "no thread " + in.Thread}
		}
	}
	s, err := e.stage(cur, in, e.Now(), fresh != nil)
	if err != nil {
		return nil, err
	}
	if s.idle() {
		return s.applied, nil
	}
	if _, err := e.st.Write(s.persist); err != nil {
		log.Printf("thread %s: %s: %v", cur.ID, in.Kind, err)
		return nil, err
	}
	e.commit(s, false)
	return s.applied, nil
}

// staged is one thread's reduce, ready to store.
type staged struct {
	*applied
	in                             core.Input
	cur, t                         *core.Thread
	v                              *core.View
	now                            time.Time
	changed, viewChanged, feedOnly bool
	fromEffect                     bool
	doneFx                         string
}

// stage reduces one Input of a thread and works out what to store. Call it
// with e.mu held.
func (e *Engine) stage(cur *core.Thread, in core.Input, now time.Time, fresh bool) (*staged, error) {
	in.Thread = cur.ID
	t, v, res, err := core.Reduce(cur, e.views[cur.ID], in, now)
	if err != nil {
		return nil, err
	}
	k := core.Kinds[in.Kind]
	if in.Source == core.FromOwner {
		e.assignProject(t)
	}
	s := &staged{in: in, cur: cur, t: t, v: v, now: now}
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
	for _, ev := range s.res.Events {
		if err := tx.Append(t.ID, ev.Type, ev.Data, s.now); err != nil {
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
	if s.changed || s.viewChanged || show {
		s.newPR = s.cur.MainPR() == nil && t.MainPR() != nil
		e.threads[t.ID], e.views[t.ID] = t, s.v
		e.bump(t.ID)
		d := e.dto(t, s.now)
		s.dto = &d
	}
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
		e.Hub.Thread(*p.dto)
	}
	e.Hub.Events(p.evs)
	if p.dto != nil {
		e.notify(*p.dto)
	}
	if p.newPR && e.OnPR != nil {
		e.OnPR(p.threadID)
	}
	e.start(p.threadID, p.res.Effects)
}

// record appends events to a thread without changing it.
func (e *Engine) record(threadID string, evs ...core.Event) {
	e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindRecord, Thread: threadID, Args: evs})
}

// replay returns the stored reply of a command id that already ran (I7),
// as a 200, and done; a store error is a 500, also done. Call it under the
// lock that also covers writing the command's receipt, so a command sent
// twice at once runs once.
func (e *Engine) replay(cmdID string) (api.Reply, bool) {
	prev, err := e.st.Receipt(cmdID)
	if err != nil {
		return api.Reply{Error: err.Error(), Status: 500}, true
	}
	if prev == nil {
		return api.Reply{}, false
	}
	var r api.Reply
	json.Unmarshal(prev, &r)
	r.Status = 200
	return r, true
}

// Thread returns a copy of one thread.
func (e *Engine) Thread(id string) (core.Thread, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.threads[id]
	if t == nil {
		return core.Thread{}, false
	}
	return *core.CloneThread(t), true
}

// DTO is a thread as clients see it (docs/API-v0.md).
type DTO struct {
	*core.Thread
	Rev         int64             `json:"rev"`
	Attention   core.Attention    `json:"attention"`
	View        *ViewDTO          `json:"view"`
	Unavailable map[string]string `json:"unavailable"`
	// NewProject is what "New project from this thread" starts with (V0.30,
	// V0.55).
	NewProject *NewProject `json:"newProject,omitempty"`
	// QuietUntil is the end of the quiet hours of the thread's project, while
	// they hold (V0.46).
	QuietUntil *time.Time `json:"quietUntil,omitempty"`
	// PR is the main change's PR, for the PR chip and tab (#32); the PR
	// itself lives on its change.
	PR *core.PR `json:"pr,omitempty"`
	// Project is the name of the thread's project (#33: derived, not
	// stored).
	Project string `json:"project"`
	// LiveView is live.enabled, the flag clients read (#33).
	LiveView bool `json:"liveView,omitempty"`
	// The settle as clients read it, derived from thread.settle and the
	// settle Rules (#33, #46): settledAt, settleWhenDone (a "done" Rule),
	// settleWhenMerged (the main change reaching landed) and settleWhen (a
	// change Rule).
	SettledAt        *time.Time     `json:"settledAt,omitempty"`
	SettleWhenDone   bool           `json:"settleWhenDone,omitempty"`
	SettleWhenMerged bool           `json:"settleWhenMerged,omitempty"`
	SettleWhen       *core.SettleOn `json:"settleWhen,omitempty"`
	// Settle shadows thread.settle, which the fields above carry.
	Settle *struct{} `json:"settle,omitempty"`
	// Feed shadows thread.feed: the notices are the agent's (#47).
	Feed *struct{} `json:"feed,omitempty"`
	// Changes are the thread's own changes and the host records it shows
	// (#35), each record once with the threads that show it.
	Changes []core.Change `json:"changes,omitempty"`

	prev core.State // the state before this change; not sent
}

// ViewDTO is the owner's view as clients read it: the snooze is derived
// from the View's snooze Rule (#46).
type ViewDTO struct {
	*core.View
	SnoozeUntil *time.Time `json:"snoozeUntil,omitempty"`
	SnoozedAt   *time.Time `json:"snoozedAt,omitempty"`
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
	vc.ChangesLook = nil // V0.65: served with the Changes tab, not on every push
	vd := &ViewDTO{View: &vc}
	vd.SnoozeUntil, vd.SnoozedAt = core.SnoozeOf(v)
	return DTO{Thread: t, Rev: e.revs[t.ID], Attention: att, View: vd, Unavailable: un, NewProject: e.draft(t), QuietUntil: e.quietUntil(t.ProjectID, now),
		PR: t.MainPR(), Project: e.projectName(t), LiveView: t.ViewOn(),
		SettledAt: t.Settle.At, SettleWhenDone: core.SettlesWhenDone(t), SettleWhenMerged: core.SettlesWhenMerged(t), SettleWhen: core.SettleWhenChange(t), Changes: e.shown(t)}
}

// Snapshot returns every thread as DTOs, most recently updated first.
func (e *Engine) Snapshot() []DTO {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.Now()
	out := make([]DTO, 0, len(e.threads))
	for _, t := range e.threads {
		out = append(out, e.build(core.CloneThread(t), now))
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

// quietKey is what the clients last got for a project's quiet hours.
func quietKey(u *time.Time) string {
	if u == nil {
		return ""
	}
	return u.UTC().Format(time.RFC3339)
}

// Tick fires the Rules that are due (a snooze ends, a deploy watch's time
// limit passes), broadcasts threads whose attention changed with time alone,
// and the projects whose quiet hours started or ended with their
// threads (V0.46; not an attention change, so never a push). Call it every
// few seconds.
func (e *Engine) Tick() {
	e.mu.Lock()
	now := e.Now()
	quiet := map[string]bool{}
	for pid := range e.projects {
		if k := quietKey(e.quietUntil(pid, now)); k != e.quietSent[pid] {
			e.quietSent[pid] = k
			quiet[pid] = true
		}
	}
	var changed []DTO
	var due []string
	for id, t := range e.threads {
		if at := core.NextCheck(t, e.view(id), now); at != nil && !at.After(now) {
			due = append(due, id)
		}
		if core.Compute(t, e.view(id), now).State != e.last[id] || quiet[t.ProjectID] {
			e.bump(id)
			changed = append(changed, e.dto(core.CloneThread(t), now))
		}
	}
	e.mu.Unlock()
	// The one timer of the Rules (#46): a Rule due at its nextCheck is
	// evaluated after the clock's Input.
	for _, id := range due {
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsClock), Kind: core.KindClock, Thread: id}); err != nil {
			log.Printf("thread %s: clock: %v", id, err)
		}
	}
	if len(quiet) > 0 {
		for _, p := range e.Projects() {
			if quiet[p.ID] {
				e.Hub.Project(p)
			}
		}
	}
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

func (e *Engine) adopt(c api.Cmd) api.Reply {
	if e.Adopt == nil {
		return api.Reply{Error: "T3 adoption is not configured", Status: 409}
	}
	e.press.Lock()
	defer e.press.Unlock()
	if r, done := e.replay(c.CommandID); done {
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
		return api.Reply{Error: err.Error(), Status: 500}
	}
	r := api.Reply{OK: true, Status: 200, Result: map[string]any{"adopted": n}}
	e.st.Write(func(tx *store.Tx) error { return tx.PutReceipt(c.CommandID, r, e.Now()) })
	return r
}

// Insert adds a thread made outside the command path (T3 adoption), with
// its history, in one transaction.
func (e *Engine) Insert(t *core.Thread, v *core.View, history []core.Event, at []time.Time) error {
	e.mu.Lock()
	e.assignProject(t)
	e.mu.Unlock()
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
