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
	"fmt"
	"log"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
	"github.com/meros/fyr/internal/lockwatch"
	"github.com/meros/fyr/internal/projicon"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/quota"
	"github.com/meros/fyr/internal/slots"
	"github.com/meros/fyr/internal/store"
	"github.com/meros/fyr/internal/stream"
)

// Agents runs agent effects. *agent.Manager implements it.
type Agents interface {
	Do(threadID string, e core.Effect) error
}

// Providers runs a provider command. *provider.Registry implements it.
type Providers = provider.Runner

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
	stored   map[string]*storedJSON   // V0.structure-cost (4): by thread id
	last     map[string]core.State    // last broadcast state, for time-driven changes
	lastWhy  map[string]string        // the words sent with it (attentionWhy)
	revs     map[string]int64         // per-thread rev (SPEC 0.5 V0.2)
	usage    map[string]*api.Usage    // V0.usage-ledger: each thread's token total, as the meter last said
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
	RerunDeps
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
	// RunProbe runs a stage probe's script in dir and returns what it printed
	// and its exit code (V0.project-playbook); nil runs it with sh in the
	// folder's environment. Probes run several at a time.
	RunProbe func(ctx context.Context, dir, script string) (string, int, error)
	probes   probes
	// RootPrompt is why a root run request cannot ask for a password here,
	// or ""; nil reads this host's processes (V0.149 (4)).
	RootPrompt func() string
	// LongRunAfter is when a pressed run request that still runs tells the
	// agent it started; 0 is core.LongRunAfter (V0.run-long-notice).
	LongRunAfter time.Duration
	pressMu      sync.Mutex
	presses      map[string]*pressRun
	rootWhy      string
	rootAt       time.Time
	Hub          *stream.Hub
	Now          func() time.Time
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
	heavyClass map[string]string          // under heavyMu: thread → its newest heavy class at the gate (V0.rerun-classes)
	// heavyGranted: under heavyMu, thread → the class its hold (since) was
	// admitted for at the gate (V0.admission-tidy: a heavier one is admitted again).
	heavyGranted map[string]grantedClass
	// heavyOpen: the threads with a heavy foreground call open, published for
	// admission, which reads it under the pool's lock (V0.admission-tidy).
	heavyOpen atomic.Pointer[map[string]bool]
	// gs is the memory guard's state between ticks (V0.guard-tick: internal/guard
	// decides; the engine keeps it and applies the decisions), under mu.
	gs           guard.State
	jobThawed    map[string]time.Time  // under mu: owner → when the guard last thawed its job (V0.sim-fixes: its idle clock starts there)
	queuedAt     map[string]time.Time  // under mu: when each waiting thread joined the queue (V0.load-tuning)
	slotOverTold map[string]time.Time  // under mu: owner → the hold already told
	guardLast    map[string]guardLine  // under mu: the guard's log lines, to say a repeat once a minute
	guardTrig    time.Time             // under mu: the PSI trigger's last firing (V0.freeze-fast-class-name)
	guardEvents  []resource.GuardEvent // under mu: the guard's last decisions, for the front
	guardPub     atomic.Pointer[resource.Guard]
	ranks        atomic.Pointer[map[string]int] // V0.project-priority: thread id → its project's rank, the non-zero ones
	// V0.guard-wake (decision 0036 E1): the threads whose new heavy command
	// the gate held while memory was short.
	gateHeld map[string]bool
	// V0.subagent-admission: heldBy is the subagent whose heavy command the
	// gate refused last on a thread (none: the thread's own agent); subWake
	// is the admission or recovery that waits for that subagent's next tool
	// call while it still runs. Both under mu.
	heldBy  map[string]subAgent
	subWake map[string]subWake
	// SubagentLive says whether a subagent of the thread's process still runs
	// (the agent manager knows; V0.subagent-admission). Optional: without it
	// every subagent counts as ended.
	SubagentLive func(thread, agent string) bool
	// V0.rerun-classes: the threads whose killed job waits for its rerun
	// (thread → entry); the kills are gs.RerunKills.
	rerunWait    map[string]string // under mu
	inv          invariants        // V0.load-invariants: under its own lock
	titleMu      sync.Mutex
	titling      map[string]bool
	textAskSlots chan struct{} // V0.210: the question job's calls at once
	searchSlots  chan struct{} // V0.ai-search: the searches at once
	// V0.quota-reset-wake: exhausted quota windows observed (profile\x00window
	// -> resetsAt) and reset frames already sent (thread\x00profile\x00window -> resetsAt), under mu.
	quotaWindows   map[string]time.Time
	quotaResetSent map[string]time.Time
}

func New(st *store.Store) *Engine {
	return &Engine{mu: lockwatch.Mutex{Critical: true, Name: "engine.mu"}, st: st, textAskSlots: make(chan struct{}, textAskCalls), searchSlots: make(chan struct{}, searchCalls), threads: map[string]*core.Thread{}, views: map[string]*core.View{},
		last: map[string]core.State{}, lastWhy: map[string]string{}, revs: map[string]int64{}, usage: map[string]*api.Usage{}, projects: map[string]*core.Project{}, stored: map[string]*storedJSON{}, records: map[string]*core.Change{}, deploySeen: map[string]string{}, cmds: map[string][]core.Command{}, quietSent: map[string]string{},
		quotaWindows: map[string]time.Time{}, quotaResetSent: map[string]time.Time{}, HostID: hostname(), Hub: stream.NewHub(), Now: time.Now,
		shotLimit: window{max: 10, span: time.Minute},
		newLimit:  window{max: 12, span: time.Hour, st: st, key: "engine.newLimit"},
		sendLimit: window{max: 30, span: time.Hour, st: st, key: "engine.sendLimit"}}
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
	e.loadAutoMerge()
	e.loadAutoDeploy()   // V0.111
	e.loadHostOrder()    // V0.132
	e.loadHostMarks()    // V0.171
	e.loadAgentMark()    // V0.208
	e.loadSystemAgents() // V0.115
	now := e.Now()
	e.newLimit.load(now)
	e.sendLimit.load(now)
	e.loadQuotaResetsLocked(now)
	e.loadRerunKillsLocked(now)
	// V0.55: threads land in the project whose folder covers theirs; a
	// thread whose project is gone loses it. The host.restarted reduce below
	// files each thread (core.Filed in core.ReduceIn, V0.handover-filed).
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
	now = e.Now()
	for id, t := range e.threads {
		att := core.ComputeWith(t, e.view(id), now, e.autoMergeOn)
		e.last[id], e.lastWhy[id] = att.State, attentionWhy(att)
	}
	e.publishRanksLocked()
	e.mu.Unlock()
	e.checkThreadLoad() // V0.thread-invariants: verify post-restart state
	e.mu.Lock()
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

// nextRev is a new rev: the clock's nanoseconds (e.Now: a sim's fake clock
// drives it), strictly increasing, so on the wall clock it also increases
// across restarts. Call with e.mu held.
func (e *Engine) nextRev() int64 {
	r := e.Now().UnixNano()
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
	skip := t == nil || t.Asleep()
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
	var snap []quota.Status
	if e.QuotaSnapshot != nil {
		snap = e.QuotaSnapshot()
	}
	e.mu.Lock()
	now := e.Now()
	e.recordExhaustedQuotaLocked(snap, now)
	var due []string
	for id, t := range e.threads {
		if at := core.NextCheck(t, e.view(id), now); at != nil && !at.After(now) {
			due = append(due, id)
		}
	}
	e.mu.Unlock()
	// The one timer of the Rules and quota auto-continue (#46, V0.quota-auto-continue):
	// apply KindClock before computing time-driven attention diffs so due transitions
	// update state and e.last without broadcasting a transient pre-clock state.
	for _, id := range due {
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsClock), Kind: core.KindClock, Thread: id}); err != nil {
			log.Printf("thread %s: clock: %v", id, err)
		}
	}
	e.mu.Lock()
	now = e.Now()
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
	for id, t := range e.threads {
		att := core.ComputeWith(t, e.view(id), now, e.autoMergeOn)
		if att.State != e.last[id] || attentionWhy(att) != e.lastWhy[id] || quiet[t.ProjectID] {
			e.bump(id)
			d, prev := e.seen(core.CloneThread(t), now)
			changed = append(changed, change{d, prev})
		}
	}
	qresets := e.dueQuotaResetsLocked(now)
	e.mu.Unlock()
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
	for _, q := range qresets {
		e.Hub.Send(stream.FrameQuotaReset, q)
	}
	e.sweepSlots()         // V0.86
	e.sweepQuietWaits()    // V0.195
	e.checkThreadTick(now) // V0.thread-invariants
	if e.Slots != nil {
		e.publishGuard(e.Now())
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
