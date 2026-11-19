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
	"github.com/meros/fyr/internal/lockwatch"
	"log"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/projicon"
	"github.com/meros/fyr/internal/slots"
	"github.com/meros/fyr/internal/store"
	"github.com/meros/fyr/internal/stream"
)

// Agents runs agent effects. *agent.Manager implements it.
type Agents interface {
	Do(threadID string, e core.Effect) error
}

// Providers runs a provider command. *provider.Registry implements it.
type Providers interface {
	Run(c api.Cmd) api.Reply
}

// EffectRunner performs an approved async effect (SPEC 5.10), e.g. pr.merge.
type EffectRunner interface {
	Run(ctx context.Context, threadID string, req core.EffectReq) (string, error)
}

type Engine struct {
	deploy *deployState     // V0.39
	stall  *lockwatch.Stall // V0.147, under stallMu
	// mu is the core lock (V0.147: watched; held past lockwatch.ExitAfter,
	// fyrd dumps and exits). Under it the engine calls no other package's
	// locking method and no callback field: read them before or after.
	mu lockwatch.Mutex
	// press serializes the kinds whose reply waits for their effects
	// (await), so the receipt check, the work and the receipt are one step.
	// Everything else is one step under mu in apply.
	press    sync.Mutex
	st       *store.Store
	threads  map[string]*core.Thread
	views    map[string]*core.View
	last     map[string]core.State    // last broadcast state, for time-driven changes
	revs     map[string]int64         // per-thread rev (SPEC 0.5 V0.2)
	projects map[string]*core.Project // V0.23
	// records are the host change records (#35): observed branches.
	records map[string]*core.Change
	// autoMerge is the repos whose merge asks Fyr approves (V0.102), by
	// core.RepoKey.
	autoMerge map[string]bool
	// autoDeployOff is the owner's auto-deploy setting turned off (V0.111);
	// deploySeen is where each open deploy ask stands, by thread/decision,
	// and deploys counts the deploys between approval or press and their
	// end. deployMu makes one auto-deploy pass at a time.
	autoDeployOff bool
	// hostOrder is the owner's host order (V0.132).
	hostOrder HostOrder
	// hostMarks are the owner's host icons and colours by install id (V0.171).
	hostMarks HostMarks
	// agentMark is this host's own agent's mark and last setup run (V0.208).
	agentMark AgentMark
	// systemAgents is the owner's system agents setting (V0.115).
	systemAgents core.SystemAgents
	deploySeen   map[string]string
	deploys      int
	deployMu     sync.Mutex
	// cmds are each thread's commands from its session (V0.79), under cmdMu.
	cmdMu sync.Mutex
	cmds  map[string][]core.Command

	// quietSent is the quiet hours the clients last got, per project (V0.46).
	quietSent map[string]string
	// repos caches each project folder's git origin (decision 0034).
	repos map[string]folderRepo
	// ScratchRoots are the scratch folders' roots (V0.28): their threads get
	// no "New project from this thread"; nil: the defaults.
	ScratchRoots []string
	// ScratchDir is where thread.create makes a scratch folder (V0.42);
	// "": ~/.local/share/fyr/scratch.
	ScratchDir string
	// Project setup (V0.116): the image icons, the fetch of an icon URL
	// (nil: projicon.Fetch), and whether a new project with a folder gets a
	// run by itself. The run's provider and model are SystemAgentResolve's
	// for core.JobSetup.
	Icons     *projicon.Store
	FetchIcon func(ctx context.Context, url string) ([]byte, error)
	AutoSetup bool
	// HostID names this host in folder sources (V0.23); default the hostname.
	HostID string
	// InstallID is this install's key id (V0.124), the key of its host mark.
	InstallID string
	// HostName names this host in the heavy-job answers (V0.163).
	HostName string
	lastRev  int64

	// The parts of the host the engine calls (deps.go): each group is set
	// once by the host's wiring, and called without e.mu.
	AgentDeps
	OutsideDeps
	DeployDeps
	SlotDeps
	UndeclaredDeps
	LeftoverDeps
	QuietDeps
	// Git reads checkouts for the git observer (#35); nil reads with git.
	Git GitReader
	// Scripts runs script actions of changes, the thread and its view (V0.60);
	// nil runs them with exec.
	Scripts ScriptRunner
	// LiveScripts runs them streaming their output (V0.149); nil runs them
	// with exec. Scripts, when set, wins (older tests).
	LiveScripts LiveScriptRunner
	// FolderEnv wraps a script so it runs in its folder's environment (the
	// folder's direnv, V0.run-folder-env); nil runs it with fyrd's own.
	FolderEnv func(dir string, argv []string) []string
	// RootPrompt is why a root run request cannot ask for a password here,
	// or ""; nil reads this host's processes (V0.149 (4)).
	RootPrompt func() string
	pressMu    sync.Mutex
	presses    map[string]*pressRun
	rootWhy    string
	rootAt     time.Time
	Hub        *stream.Hub
	Now        func() time.Time
	// Attach stores files for the show tool (V0.19); Shooter takes screenshots.
	Attach  *attach.Store
	Shooter Shooter
	// Limits for agent tools live on the engine, not on one tool registration.
	shotLimit window
	newLimit  window
	sendLimit window
	// Slots is the host's pool of slots for heavy jobs (V0.86); set it with
	// UseSlots. nil: the slot tools say there are none.
	Slots *slots.Pool
	// Kept: the threads whose processes the launcher's runner kept across
	// the restart (V0.90); set before Load.
	Kept       map[string]bool
	slotIdle   map[string]time.Time  // under mu: held slots without a live run, since
	quietWaits map[string]*quietWait // under mu: V0.195, thread → its CPU watch
	// V0.209: the heavy commands that carry each thread's admission.
	heavyMu    sync.Mutex
	heavyCalls map[string]map[string]bool // under heavyMu: thread → its running foreground calls
	heavyBg    map[string]bool            // under heavyMu: thread → a job outlives its call
	// frozen is what the guard holds frozen now, the newest last (decision
	// 0029 S5; V0.multi-freeze: more than one), under mu.
	frozen       []frozenJob
	guardOK      time.Time            // under mu: since when memory is fine (zero: it is not)
	frozeHolds   map[string]time.Time // under mu: holds frozen once already (owner → since)
	queuedAt     map[string]time.Time // under mu: when each waiting thread joined the queue (V0.load-tuning)
	slotOverTold map[string]time.Time // under mu: owner → the hold already told
	undFroze     map[string]undFreeze // under mu: thread → what the guard froze of its episode
	guardLast    map[string]guardLine // under mu: the guard's log lines, to say a repeat once a minute
	guardShort   bool                 // under mu: memory was short at the last tick
	guardSince   time.Time            // under mu: since when it is short
	guardEvents  []slots.GuardEvent   // under mu: the guard's last decisions, for the front
	guardPub     atomic.Pointer[slots.Guard]
	ranks        atomic.Pointer[map[string]int] // V0.project-priority: thread id → its project's rank, the non-zero ones
	memGuard     bool                           // under mu: the guard told a thread in this episode
	titleMu      sync.Mutex
	titling      map[string]bool
	textAskSlots chan struct{} // V0.210: the question job's calls at once
	searchSlots  chan struct{} // V0.ai-search: the searches at once
}

func New(st *store.Store) *Engine {
	return &Engine{mu: lockwatch.Mutex{Critical: true, Name: "engine.mu"}, st: st, textAskSlots: make(chan struct{}, textAskCalls), searchSlots: make(chan struct{}, searchCalls), threads: map[string]*core.Thread{}, views: map[string]*core.View{},
		last: map[string]core.State{}, revs: map[string]int64{}, projects: map[string]*core.Project{}, records: map[string]*core.Change{}, deploySeen: map[string]string{}, cmds: map[string][]core.Command{}, quietSent: map[string]string{}, HostID: hostname(), Hub: stream.NewHub(), Now: time.Now,
		shotLimit: window{max: 10, span: time.Minute}, newLimit: window{max: 12, span: time.Hour}, sendLimit: window{max: 30, span: time.Hour}}
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
	if err := e.migrateDone(); err != nil { // V0.98 (7)
		return err
	}
	ps, err := e.st.Projects()
	if err != nil {
		return err
	}
	for _, p := range ps {
		e.projects[p.ID] = p
	}
	e.loadAutoMerge()
	e.loadAutoDeploy()   // V0.111
	e.loadHostOrder()    // V0.132
	e.loadHostMarks()    // V0.171
	e.loadAgentMark()    // V0.208
	e.loadSystemAgents() // V0.115
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
		in := core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindHostRestarted, Thread: id, Args: core.RestartArgs{Kept: e.Kept[id]}}
		if _, err := e.apply(in, nil); err != nil {
			e.mu.Lock()
			return err
		}
	}
	e.mu.Lock()
	// Clients and push already know these states; only changes notify.
	now := e.Now()
	for id, t := range e.threads {
		e.last[id] = core.ComputeWith(t, e.view(id), now, e.autoMergeOn).State
	}
	e.publishRanksLocked()
	return nil
}

// ProjectRank is thread's project priority as a number (V0.project-priority):
// 1 high, 0 normal, -1 low. It takes no lock: the slot pool and the cgroup
// tree call it under their own.
func (e *Engine) ProjectRank(thread string) int {
	if m := e.ranks.Load(); m != nil {
		return (*m)[thread]
	}
	return 0
}

// publishRanksLocked publishes every thread's project rank for ProjectRank.
// Call it with e.mu held, after a thread changes project or a project its
// priority.
func (e *Engine) publishRanksLocked() {
	m := map[string]int{}
	for id, t := range e.threads {
		if r := e.projects[t.ProjectID].Rank(); r != 0 {
			m[id] = r
		}
	}
	e.ranks.Store(&m)
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

// Command applies one owner command exactly once per CommandID (I7): it
// builds the owner's Input and submits it.
func (e *Engine) Command(c api.Cmd) api.Reply { return e.CommandAs(c, "", "") }

// CommandAs is Command with who acted (decision 0030 3.3): actor is
// "device:<id>", "local:owner" or "install:<id>", front the install whose front
// forwarded it. The command's event records both.
func (e *Engine) CommandAs(c api.Cmd, actor, front string) api.Reply {
	if c.CommandID == "" {
		return api.Reply{Error: "commandId is required", Status: 400}
	}
	return e.submit(core.Input{Source: core.FromOwner, Kind: c.Action, Thread: c.ThreadID, Args: c.Args, CmdID: c.CommandID, Actor: actor, Front: front})
}

// submit sends a command's Input down the one path: the owner's commands
// and the agent's thread_send and thread_new. It checks what only the host
// can know first (a project, a profile, an attachment, a folder on disk),
// makes the record of a kind that makes one, and takes the path of the
// kind's record: a thread, a project, or the host for a kind whose reply
// waits for its effects.
func (e *Engine) submit(in core.Input) api.Reply {
	k := core.Kinds[in.Kind]
	if k == nil || !slices.Contains(k.From, in.Source.Principal()) {
		return api.Reply{Error: "unknown action " + in.Kind, Status: 400}
	}
	var a core.Args
	switch x := in.Args.(type) {
	case core.Args:
		a = x
	case map[string]any:
		a = x
	}
	if a == nil {
		a = core.Args{}
	}
	in.Args = a
	if kind, ok := recordKinds[in.Kind]; ok {
		// V0.98 (4) Done and V0.101 Abandon on a host record.
		if r, ok := e.recordOwner(in, a, kind); ok {
			return r
		}
	}
	if k.Await {
		return e.await(in, k)
	}
	if in.Kind == core.KindHostSetupRun { // V0.208: the thread first
		return e.startHostSetup(in.CmdID)
	}
	if in.Kind == core.KindProjectSetupRun { // V0.116: the thread first
		return e.startSetup(a.Str("id"), in.Source, in.CmdID)
	}
	if k.On == core.RecProject {
		r := e.applyProject(in)
		if r.OK {
			e.afterProject(in, r)
		}
		return r
	}
	if in.Kind == "thread.setProject" && a.Str("projectId") != "" {
		e.mu.Lock()
		ok := e.projects[a.Str("projectId")] != nil
		e.mu.Unlock()
		if !ok {
			return api.Reply{Error: "no project " + a.Str("projectId"), Status: 400}
		}
	}
	if in.Kind == "thread.handover" && e.ProfileExists != nil && a.Str("profile") != "" && !e.ProfileExists(a.Str("profile")) {
		return api.Reply{Error: "no profile " + a.Str("profile"), Status: 400}
	}
	if in.Kind == "thread.create" || in.Kind == "thread.send" {
		for _, id := range core.AttachmentIDs(a) {
			if e.AttachmentExists == nil || !e.AttachmentExists(id) {
				return api.Reply{Error: "unknown attachment " + id, Status: 400}
			}
		}
	}
	var fresh *core.Thread
	var scratch string
	if in.Kind == "thread.create" {
		if r, done := e.replay(in.CmdID); done {
			return r
		}
		now := e.Now()
		fresh = &core.Thread{ID: NewID("t_"), Owner: core.Owner, Visibility: "private", CreatedAt: now, Source: "fyr"}
		// V0.42: thread.create files into projectId; without a cwd it starts
		// in the project's folder (V0.55), else in a fresh scratch folder.
		args := maps.Clone(a)
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
		in.Args = args
	}
	p, err := e.apply(in, fresh)
	if scratch != "" && (err != nil || p.replayed) {
		os.Remove(scratch)
	}
	if err != nil {
		return rejected(err)
	}
	e.publish(p)
	return p.reply
}

// await is the path of a kind whose reply waits for its effects
// (core.InputKind.Await): the reduce, then its effects in order, then the
// receipt with the outcome. press makes the receipt check, the work and the
// receipt one step. A failure that may pass on a retry (409, 5xx) stores no
// receipt (A4).
func (e *Engine) await(in core.Input, k *core.InputKind) api.Reply {
	e.press.Lock()
	defer e.press.Unlock()
	if r, done := e.replay(in.CmdID); done {
		return r
	}
	id := in.CmdID
	in.CmdID = "" // the receipt waits for the outcome
	var effs []core.Effect
	var reply api.Reply
	if k.On == core.RecHost {
		res, err := core.ReduceHost(in, e.Now())
		if err != nil {
			return rejected(err)
		}
		effs, reply = res.Effects, api.Reply{OK: true, Status: 200, Result: res.Reply}
	} else {
		p, err := e.apply(in, nil)
		if err != nil {
			return rejected(err)
		}
		effs, p.res.Effects = p.res.Effects, nil
		e.publish(p)
		reply = p.reply
	}
	for _, ef := range effs {
		if reply = e.awaited(in, ef, reply); !reply.OK {
			break
		}
	}
	if reply.Status != 409 && reply.Status < 500 {
		e.st.Write(func(tx *store.Tx) error { return tx.PutReceipt(id, reply, e.Now()) })
	}
	return reply
}

// awaited runs one effect of an Await kind and gives the reply its outcome.
func (e *Engine) awaited(in core.Input, ef core.Effect, reply api.Reply) api.Reply {
	fail := func(status int, msg string) api.Reply { return api.Reply{Error: msg, Status: status} }
	switch ef.Kind {
	case core.EffectCancelMessage:
		if e.Agents == nil {
			return fail(409, "No agent runs this thread")
		}
		if err := e.Agents.Do(in.Thread, ef); err != nil {
			return fail(409, err.Error())
		}
	case core.EffectAdopt:
		if e.Adopt == nil {
			return fail(409, "T3 adoption is not configured")
		}
		var ids []string
		raw, _ := ef.Request.Args["t3Ids"].([]any)
		for _, x := range raw {
			if s, ok := x.(string); ok {
				ids = append(ids, s)
			}
		}
		n, err := e.Adopt(ids)
		if err != nil {
			return fail(500, err.Error())
		}
		reply.Result = map[string]any{"adopted": n}
	case core.EffectDeployNow:
		if e.DeployNow == nil {
			return fail(409, "This host runs no deploy")
		}
		if err := e.DeployNow(); err != nil {
			return fail(409, err.Error())
		}
	case core.EffectUpdateNow:
		if e.UpdateNow == nil {
			return fail(409, "This host has no release source")
		}
		if err := e.UpdateNow(); err != nil {
			return fail(409, err.Error())
		}
	case core.EffectAutoMergeSet: // V0.102: the owner's setting
		repo, _ := ef.Request.Args["repo"].(string)
		on, _ := ef.Request.Args["on"].(bool)
		if err := e.setAutoMerge(repo, on); err != nil {
			return fail(500, err.Error())
		}
		reply.Result = e.AutoMergeRepos()
	case core.EffectAutoDeploySet: // V0.111: the owner's setting
		on, _ := ef.Request.Args["on"].(bool)
		if err := e.setAutoDeploy(on); err != nil {
			return fail(500, err.Error())
		}
		reply.Result = e.AutoDeploy()
	case core.EffectHostOrderSet: // V0.132: the owner's host order, newest wins
		var o HostOrder
		for _, x := range ef.Request.Args["names"].([]any) {
			o.Names = append(o.Names, x.(string))
		}
		if ids, ok := ef.Request.Args["ids"].([]any); ok { // V0.host-ids
			for _, x := range ids {
				o.IDs = append(o.IDs, x.(string))
			}
		}
		o.At, _ = time.Parse(time.RFC3339Nano, ef.Request.Args["at"].(string))
		kept, err := e.setHostOrder(o)
		if err != nil {
			return fail(500, err.Error())
		}
		reply.Result = kept
	case core.EffectHostMarksSet: // V0.171: the owner's host marks, newest wins
		kept, err := e.setHostMarks(hostMarksOf(ef.Request.Args))
		if err != nil {
			return fail(500, err.Error())
		}
		reply.Result = kept
	case core.EffectJobReset: // decision 0029 S3: the owner's Reset of a learned class
		c, _ := ef.Request.Args["class"].(string)
		if e.JobReset == nil || !e.JobReset(c) {
			return fail(404, "No learned job class "+c)
		}
		reply.Result = map[string]any{"class": c}
	case core.EffectLeftoverEnd: // V0.176
		id, _ := ef.Request.Args["id"].(string)
		if e.LeftoverEnd == nil {
			return fail(409, "This host does not track leftover processes")
		}
		if err := e.LeftoverEnd(id); err != nil {
			return fail(404, err.Error())
		}
		reply.Result = map[string]any{"id": id}
	case core.EffectLeftoverKeep:
		id, _ := ef.Request.Args["id"].(string)
		h, _ := ef.Request.Args["hours"].(float64)
		if e.LeftoverKeep == nil {
			return fail(409, "This host does not track leftover processes")
		}
		if err := e.LeftoverKeep(id, time.Duration(h*float64(time.Hour))); err != nil {
			return fail(404, err.Error())
		}
		reply.Result = map[string]any{"id": id, "hours": h}
	case core.EffectTempRemove:
		p, _ := ef.Request.Args["path"].(string)
		if e.TempRemove == nil {
			return fail(409, "This host lists no temporary folders")
		}
		if err := e.TempRemove(p); err != nil {
			return fail(409, err.Error())
		}
		reply.Result = map[string]any{"path": p}
	case core.EffectHeavyRemove:
		p, _ := ef.Request.Args["pattern"].(string)
		if e.HeavyRemove == nil || !e.HeavyRemove(p) {
			return fail(404, "No such heavy command")
		}
		reply.Result = map[string]any{"pattern": p}
	case core.EffectUndeclaredSettings:
		if e.UndeclaredSettings == nil {
			return fail(409, "This host cannot change these settings")
		}
		if err := e.UndeclaredSettings(ef.Request.Args); err != nil {
			return fail(400, err.Error())
		}
		reply.Result = ef.Request.Args
	case core.EffectSlotOrderSet: // V0.137: the owner's order of the slot queue
		if e.Slots == nil || e.Slots.Off() {
			return fail(409, "This host has no slots for heavy jobs")
		}
		var owners []string
		for _, x := range ef.Request.Args["owners"].([]any) {
			owners = append(owners, x.(string))
		}
		reply.Result = e.Slots.Reorder(owners)
	case core.EffectSystemAgentsSet: // V0.115: the owner's setting
		a := ef.Request.Args
		job, _ := a["job"].(string)
		clr, _ := a["clear"].(bool)
		p, _ := a["profile"].(string)
		m, _ := a["model"].(string)
		if err := e.setSystemAgents(job, core.SystemAgentChoice{Profile: p, Model: m}, clr); err != nil {
			return fail(400, err.Error())
		}
		reply.Result = e.SystemAgentsView()
	case core.EffectProvider:
		if e.Providers == nil {
			return fail(404, "no providers on this host")
		}
		r := e.Providers.Run(api.Cmd{Action: ef.Request.Action, Args: core.Args(ef.Request.Args)})
		if r.Status == 0 {
			r.Status = 200
		}
		return r
	}
	return reply
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
			// A kind that makes its thread (T3 adoption) names the new id.
			if k := core.Kinds[in.Kind]; k == nil || !k.Makes || in.Thread == "" {
				return nil, &core.Rejection{Code: core.RejectNotFound, Reason: "no thread " + in.Thread}
			}
			fresh = &core.Thread{ID: in.Thread}
			cur = fresh
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
	if in.Source == core.FromOwner || fresh {
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
		s.newPR = s.cur.MainPR() == nil && t.MainPR() != nil
		e.threads[t.ID], e.views[t.ID] = t, s.v
		e.bump(t.ID)
		d, prev := e.seen(t, s.now)
		s.dto, s.prev = &d, prev
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
		e.Hub.Send(stream.FrameThread, *p.dto)
	}
	e.sendEvents(p.evs)
	if p.dto != nil {
		e.notify(*p.dto, p.prev)
	}
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

// ThreadProject is the name of thread id's project, or "" (decision 0029:
// a job's class starts with it).
func (e *Engine) ThreadProject(id string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if t := e.threads[id]; t != nil {
		return e.projectName(t)
	}
	return ""
}

// notify reports an attention change; call it without e.mu held.
func (e *Engine) notify(d api.Thread, prev core.State) {
	if e.OnAttention != nil && d.Attention.State != prev {
		e.OnAttention(d, prev)
	}
}

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
	}
	quiet := core.PushQuiet(t, att.State, e.autoMergeOn)
	return api.Thread{Thread: t, Rev: e.revs[t.ID], Attention: att, View: vd, Unavailable: un, NewProject: e.draft(t), QuietUntil: e.quietUntil(t.ProjectID, now), PushQuiet: quiet,
		PR: t.MainPR(), Project: e.projectName(t), LiveView: t.ViewOn(),
		SettledAt: t.Settle.At, SettleWhenDone: core.SettlesWhenDone(t), SettleWhenMerged: core.SettlesWhenMerged(t), SettleWhen: core.SettleWhenChange(t), Changes: changes,
		RootPrompt: e.threadRootPrompt(t)}
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

// Continue sends "continue" to a thread whose turn a forced deploy cut
// (V0.83), as the owner's message on the one send path, so the run resumes
// the session. A thread settled or archived since gets nothing. One
// commandID sends once (I7). It reports whether the thread took it.
func (e *Engine) Continue(id, commandID string) bool {
	e.mu.Lock()
	t := e.threads[id]
	skip := t == nil || t.ArchivedAt != nil || t.Settle.At != nil
	e.mu.Unlock()
	if skip {
		return false
	}
	return e.submit(core.Input{Source: core.FromOwner, Kind: "thread.send", Thread: id, Args: core.Args{"text": "continue"}, CmdID: commandID}).OK
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
	type change struct {
		d    api.Thread
		prev core.State
	}
	var changed []change
	var due []string
	for id, t := range e.threads {
		if at := core.NextCheck(t, e.view(id), now); at != nil && !at.After(now) {
			due = append(due, id)
		}
		if core.ComputeWith(t, e.view(id), now, e.autoMergeOn).State != e.last[id] || quiet[t.ProjectID] {
			e.bump(id)
			d, prev := e.seen(core.CloneThread(t), now)
			changed = append(changed, change{d, prev})
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
				e.Hub.Send(stream.FrameProject, p)
			}
		}
	}
	for _, c := range changed {
		e.Hub.Send(stream.FrameThread, c.d)
		e.notify(c.d, c.prev)
	}
	e.sweepSlots()      // V0.86
	e.sweepQuietWaits() // V0.195
}

// EventsBefore pages back: the newest limit events older than before.
func (e *Engine) EventsBefore(id string, before int64, limit int) ([]store.Event, error) {
	return e.st.EventsBefore(id, before, limit)
}

// Events reads the timeline of one thread; after == 0 gives the newest limit.
func (e *Engine) Events(id string, after int64, limit int) ([]store.Event, error) {
	return e.st.EventsFor(id, after, limit, after == 0)
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
