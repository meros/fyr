package engine

// V0.load-invariants: invariants of the load guard, checked on its tick. A
// violation writes one `invariant: <id> ...` log line and, at most once a day
// per invariant and never while its card is open, puts a card in the owner's
// Needs you with the evidence: the signals and the guard's log lines of the
// ten minutes before. The card's Fix it starts a thread in the host's Fyr
// checkout with that evidence; fyrd never starts one by itself (the owner,
// 19 Nov: "a Needs-you card only").

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/slots"
)

// The invariants' ids, stable: the log, the card and the fix thread name them.
const (
	InvThawIntoCritical  = "thaw-into-critical"
	InvFreezeHeldLong    = "freeze-held-long"
	InvFreezeCycling     = "freeze-cycling"
	InvProbeSilent       = "probe-silent"
	InvLimitUnderPrior   = "limit-under-prior"
	InvPSITriggerMissing = "psi-trigger-missing"
	InvShortNothingFroze = "short-nothing-frozen"
)

// The thresholds.
const (
	// InvHeldLong: a freeze held longer than this (FreezeCeiling thaws it).
	InvHeldLong = 30 * time.Minute
	// InvCycles thaws of one thread within InvCycleWindow, and more, cycle.
	InvCycles      = 3
	InvCycleWindow = 60 * time.Minute
	// InvProbeSilentAfter: the cgroups provider without a slowdown this long.
	InvProbeSilentAfter = 5 * time.Minute
	// InvPSIWithin: the scope layout has its PSI trigger this long after the start.
	InvPSIWithin = time.Hour
	// InvShortAfter: memory short this long with flagged work and nothing frozen.
	InvShortAfter = 10 * time.Minute
	// InvCardEvery: at most one card per invariant in this time, and none
	// for this long after a Dismiss.
	InvCardEvery = 24 * time.Hour
	// InvLogWindow and InvLogKeep: the guard's log lines a card shows.
	InvLogWindow = 10 * time.Minute
	InvLogKeep   = 200
	invShowLines = 60
)

// invTexts is each invariant in one sentence, as the card says it.
var invTexts = map[string]string{
	InvThawIntoCritical:  "The guard thawed work while the agents' memory stalled critically (not at the 30 min ceiling, and not because its processes were gone).",
	InvFreezeHeldLong:    "Work stayed frozen for more than 30 minutes.",
	InvFreezeCycling:     "One thread was frozen and thawed more than 3 times within an hour.",
	InvProbeSilent:       "The cgroups provider runs, but the slowdown probe has given no reading for more than 5 minutes.",
	InvLimitUnderPrior:   "A job's memory.high was written below the prior (the agents' memory budget divided by the slots setting).",
	InvPSITriggerMissing: "An hour after fyrd started, the scope layout still has no PSI trigger on the agents' memory stall.",
	InvShortNothingFroze: "Memory has been short for more than 10 minutes with flagged work running, and nothing is frozen or held.",
}

// invOrder is the order the Settings list shows.
var invOrder = []string{InvThawIntoCritical, InvFreezeHeldLong, InvFreezeCycling, InvShortNothingFroze, InvLimitUnderPrior, InvProbeSilent, InvPSITriggerMissing}

const invKey = "loadInvariants"

// invState is one invariant's record, kept in the host's kv.
type invState struct {
	Count     int                  `json:"count"`
	Last      time.Time            `json:"last"`
	CardAt    time.Time            `json:"cardAt"`
	Dismissed time.Time            `json:"dismissed"`
	Open      *slots.InvariantCard `json:"open,omitempty"`
	Thread    string               `json:"thread,omitempty"` // the last Fix it's thread
}

type violation struct{ id, evidence string }

// invariants is the checker's state, under its own mu (never e.mu).
type invariants struct {
	mu      sync.Mutex
	loaded  bool
	state   map[string]*invState
	start   time.Time
	pending []violation
	once    map[string]time.Time   // a condition already reported, and when
	thaws   map[string][]time.Time // thread → its thaws in the last InvCycleWindow
	cycled  map[string]time.Time   // thread → its last freeze-cycling
	silent  time.Time              // since when the probe is silent
	scope   bool                   // PSITrigger: the scope layout
	armed   bool                   // PSITrigger: the trigger is set
	psiSeen bool                   // PSITrigger was called
	ring    lineRing
}

// lineRing keeps the guard's last log lines with when they were written.
type lineRing struct {
	mu    sync.Mutex
	now   func() time.Time
	lines []ringLine
}

type ringLine struct {
	at   time.Time
	text string
}

func (r *lineRing) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	at := time.Now()
	if r.now != nil {
		at = r.now()
	}
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		i := strings.Index(l, "guard: ")
		if i < 0 || strings.Contains(l[:i], "invariant: ") {
			continue
		}
		r.lines = append(r.lines, ringLine{at, at.Format("15:04:05") + " " + l[i:]})
	}
	if n := len(r.lines); n > InvLogKeep {
		r.lines = append([]ringLine(nil), r.lines[n-InvLogKeep:]...)
	}
	return len(b), nil
}

// since is the lines written at from or later, the newest invShowLines.
func (r *lineRing) since(from time.Time) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, l := range r.lines {
		if !l.at.Before(from) {
			out = append(out, l.text)
		}
	}
	if len(out) > invShowLines {
		out = out[len(out)-invShowLines:]
	}
	return out
}

// LogTap is the writer fyrd tees its log into, so a card shows the guard's
// last lines without reading the log file.
func (e *Engine) LogTap() io.Writer {
	e.inv.ring.mu.Lock()
	e.inv.ring.now = e.Now
	e.inv.ring.mu.Unlock()
	return &e.inv.ring
}

// PSITrigger tells the checker whether fyrd runs the scope layout and has
// its PSI trigger on the agents' memory stall (psi-trigger-missing).
func (e *Engine) PSITrigger(scope, armed bool) {
	c := &e.inv
	c.mu.Lock()
	c.scope, c.armed, c.psiSeen = scope, c.armed || armed, true
	c.mu.Unlock()
}

// JobLimitSet is the `guard: job limit` decision as JobGroups wrote it
// (limit-under-prior): a memory.high below the prior, budget/N, is a
// violation unless the class has learned its peak from slots.LearnRuns runs.
func (e *Engine) JobLimitSet(thread, class, why string, high, prior uint64, settled bool) {
	if high == 0 || prior == 0 || high >= prior || settled {
		return
	}
	c := &e.inv
	c.mu.Lock()
	c.pending = append(c.pending, violation{InvLimitUnderPrior, fmt.Sprintf("thread=%s class=%q high=%s prior=%s why=%s", thread, class, slots.GB(float64(high)), slots.GB(float64(prior)), why)})
	c.mu.Unlock()
}

// invEvent sees each guard decision (guardRecord): thaw-into-critical and
// freeze-cycling.
func (e *Engine) invEvent(ev slots.GuardEvent, sig slots.Signals) {
	if ev.Kind != "thaw" || ev.Thread == "" {
		return
	}
	c := &e.inv
	c.mu.Lock()
	defer c.mu.Unlock()
	if sig.AgentsStall == slots.StallCritical && ev.Reason != "ceiling" && ev.Reason != "gone" {
		c.pending = append(c.pending, violation{InvThawIntoCritical, fmt.Sprintf("thread=%s work=%s reason=%s %s", ev.Thread, ev.Work, ev.Reason, guardSignals(sig))})
	}
	if c.thaws == nil {
		c.thaws, c.cycled = map[string][]time.Time{}, map[string]time.Time{}
	}
	ts := slices.DeleteFunc(append(c.thaws[ev.Thread], ev.At), func(t time.Time) bool { return ev.At.Sub(t) >= InvCycleWindow })
	c.thaws[ev.Thread] = ts
	if last, ok := c.cycled[ev.Thread]; len(ts) > InvCycles && (!ok || ev.At.Sub(last) >= InvCycleWindow) {
		c.cycled[ev.Thread] = ev.At
		c.pending = append(c.pending, violation{InvFreezeCycling, fmt.Sprintf("thread=%s thaws=%d in %s %s", ev.Thread, len(ts), InvCycleWindow, guardSignals(sig))})
	}
}

// checkInvariants runs on the guard's tick, after freezeGuard and
// memoryGuard: the state invariants, then every violation since the last.
func (e *Engine) checkInvariants() {
	if e.SlotSignals == nil {
		return
	}
	sig := e.SlotSignals()
	now := e.Now()
	flags := e.flags()
	e.mu.Lock()
	frozen := append([]frozenJob(nil), e.frozen...)
	short, since, held := e.guardShort, e.guardSince, len(e.gateHeld)
	e.mu.Unlock()
	c := &e.inv
	c.mu.Lock()
	if c.start.IsZero() {
		c.start = now
	}
	if c.once == nil {
		c.once = map[string]time.Time{}
	}
	for k, at := range c.once {
		if now.Sub(at) > InvCardEvery {
			delete(c.once, k)
		}
	}
	first := func(key string) bool {
		if _, ok := c.once[key]; ok {
			return false
		}
		c.once[key] = now
		return true
	}
	for _, f := range frozen {
		if d := now.Sub(f.at); d > InvHeldLong && first("held:"+f.owner+"@"+f.at.Format(time.RFC3339Nano)) {
			c.pending = append(c.pending, violation{InvFreezeHeldLong, fmt.Sprintf("thread=%s undeclared=%t frozen=%s class=%q %s", f.owner, f.undeclared, d.Round(time.Second), f.class+f.cmd, guardSignals(sig))})
		}
	}
	if sig.Provider == "cgroups" && sig.Slowdown == nil {
		if c.silent.IsZero() {
			c.silent = now
		}
		if d := now.Sub(c.silent); d > InvProbeSilentAfter && first("silent@"+c.silent.Format(time.RFC3339Nano)) {
			c.pending = append(c.pending, violation{InvProbeSilent, fmt.Sprintf("provider=cgroups no slowdown for %s %s", d.Round(time.Second), guardSignals(sig))})
		}
	} else {
		c.silent = time.Time{}
	}
	if c.psiSeen && c.scope && !c.armed && now.Sub(c.start) >= InvPSIWithin && first("psi") {
		c.pending = append(c.pending, violation{InvPSITriggerMissing, fmt.Sprintf("layout=scope no PSI trigger %s after the start", now.Sub(c.start).Round(time.Minute))})
	}
	if short && !since.IsZero() && now.Sub(since) > InvShortAfter && len(flags) > 0 && len(frozen) == 0 && held == 0 && first("short@"+since.Format(time.RFC3339Nano)) {
		names := make([]string, 0, len(flags))
		for _, f := range flags {
			names = append(names, fmt.Sprintf("%s(%s %s)", f.Thread, f.Command, slots.GB(float64(f.Mem))))
		}
		c.pending = append(c.pending, violation{InvShortNothingFroze, fmt.Sprintf("short for %s flagged=%s frozen=0 held=0 running=%d %s", now.Sub(since).Round(time.Second), strings.Join(names, ","), len(e.Running()), guardSignals(sig))})
	}
	vs := c.pending
	c.pending = nil
	c.mu.Unlock()
	for _, v := range vs {
		e.violate(v, now)
	}
}

// violate logs v and opens its card when the day's rule lets it.
func (e *Engine) violate(v violation, now time.Time) {
	log.Printf("invariant: %s %s", v.id, v.evidence)
	lines := e.inv.ring.since(now.Add(-InvLogWindow))
	c := &e.inv
	c.mu.Lock()
	defer c.mu.Unlock()
	s := e.invLoad(v.id)
	s.Count++
	s.Last = now
	quiet := s.CardAt
	if s.Dismissed.After(quiet) {
		quiet = s.Dismissed
	}
	if s.Open == nil && (quiet.IsZero() || now.Sub(quiet) >= InvCardEvery) {
		s.Open = &slots.InvariantCard{ID: v.id, Text: invTexts[v.id], At: now, Evidence: v.evidence, Lines: lines}
		s.CardAt = now
	}
	e.invSave()
}

// invLoad is id's record, the kv read once; under c.mu.
func (e *Engine) invLoad(id string) *invState {
	c := &e.inv
	if !c.loaded {
		c.loaded = true
		c.state = map[string]*invState{}
		if e.st != nil {
			if raw, _ := e.st.GetKV(invKey); raw != "" {
				if err := json.Unmarshal([]byte(raw), &c.state); err != nil {
					log.Printf("load invariants: %v", err)
				}
			}
		}
	}
	if id == "" {
		return nil
	}
	s := c.state[id]
	if s == nil {
		s = &invState{}
		c.state[id] = s
	}
	return s
}

// invSave writes the records; under c.mu.
func (e *Engine) invSave() {
	if e.st == nil {
		return
	}
	b, _ := json.Marshal(e.inv.state)
	if err := e.st.SetKV(invKey, string(b)); err != nil {
		log.Printf("load invariants: %v", err)
	}
}

// invariantView is the cards and the counts for the front.
func (e *Engine) invariantView() *slots.Invariants {
	c := &e.inv
	c.mu.Lock()
	e.invLoad("")
	v := &slots.Invariants{Stats: []slots.InvariantStat{}}
	for _, id := range invOrder {
		st := slots.InvariantStat{ID: id, Text: invTexts[id]}
		if s := c.state[id]; s != nil {
			st.Count = s.Count
			if !s.Last.IsZero() {
				t := s.Last
				st.Last = &t
			}
			if s.Open != nil {
				v.Cards = append(v.Cards, *s.Open)
			}
		}
		v.Stats = append(v.Stats, st)
	}
	c.mu.Unlock()
	if len(v.Cards) > 0 {
		v.FixDir, _ = e.fyrCheckout()
	}
	return v
}

// fyrCheckout is the folder and id of the project that holds a Fyr checkout
// (cmd/fyrd and docs/changes), the one named Fyr first; "" when none.
func (e *Engine) fyrCheckout() (dir, id string) {
	type cand struct{ dir, id, name string }
	var cs []cand
	e.mu.Lock()
	for _, p := range e.projects {
		if p.Folder != "" {
			cs = append(cs, cand{p.Folder, p.ID, p.Name})
		}
	}
	e.mu.Unlock()
	slices.SortFunc(cs, func(a, b cand) int {
		fa, fb := strings.EqualFold(a.name, "fyr"), strings.EqualFold(b.name, "fyr")
		if fa != fb {
			if fa {
				return -1
			}
			return 1
		}
		return strings.Compare(a.id, b.id)
	})
	for _, c := range cs {
		if isFyrCheckout(c.dir) {
			return c.dir, c.id
		}
	}
	return "", ""
}

func isFyrCheckout(dir string) bool {
	for _, f := range []string{"cmd/fyrd", "docs/changes/README.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			return false
		}
	}
	return true
}

// invariantDismiss closes id's card; no card of id comes for InvCardEvery.
func (e *Engine) invariantDismiss(id string) error {
	c := &e.inv
	c.mu.Lock()
	defer c.mu.Unlock()
	s := e.invLoad(id)
	if s.Open == nil {
		return fmt.Errorf("no open card for %s", id)
	}
	s.Open, s.Dismissed = nil, e.Now()
	e.invSave()
	return nil
}

// invariantFix starts the fix thread of id's open card in the Fyr checkout,
// as the owner who pressed Fix it, and closes the card.
func (e *Engine) invariantFix(id string) api.Reply {
	c := &e.inv
	c.mu.Lock()
	s := e.invLoad(id)
	var card slots.InvariantCard
	if s.Open != nil {
		card = *s.Open
	}
	c.mu.Unlock()
	if card.ID == "" {
		return api.Reply{Error: "No open card for " + id, Status: 404}
	}
	dir, project := e.fyrCheckout()
	if dir == "" {
		return api.Reply{Error: "This host has no project with a Fyr checkout: add the Fyr repo as a project to fix it from here", Status: 409}
	}
	args := core.Args{"cwd": dir, "projectId": project, "title": "Fix load invariant " + id, "text": invPrompt(card, e.hostName())}
	r := e.submit(core.Input{Source: core.FromOwner, Kind: "thread.create", Args: args, CmdID: NewID("inv_")})
	if !r.OK {
		return r
	}
	c.mu.Lock()
	s.Open, s.Thread = nil, r.ThreadID
	e.invSave()
	c.mu.Unlock()
	return api.Reply{OK: true, Status: 200, ThreadID: r.ThreadID, Result: map[string]any{"thread": r.ThreadID}}
}

// invPrompt is the fix thread's first message.
func invPrompt(card slots.InvariantCard, host string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Fyr's load invariant `%s` was violated on %s at %s.\n\n%s\n\nEvidence: %s\n", card.ID, host, card.At.Format(time.RFC3339), card.Text, card.Evidence)
	if len(card.Lines) > 0 {
		b.WriteString("\nThe guard's log lines of the 10 minutes before:\n\n```\n")
		for _, l := range card.Lines {
			b.WriteString(l + "\n")
		}
		b.WriteString("```\n")
	}
	b.WriteString("\nRead docs/decisions/0029-adaptive-slots.md, docs/decisions/0032-bounded-agents.md, docs/decisions/0036-memory-layers.md and the notes in docs/changes/ (docs/changes/load-invariants.md defines each invariant). Reproduce it in a test first, fix it, run scripts/ci-local.sh, and open a PR.\n")
	return b.String()
}
