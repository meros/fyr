// Package usage is the token meter (decision 0041, V0.usage-ledger): it
// turns each usage report of a CLI into rows of the host's usage ledger.
//
// The ledger is a measurement kept outside the one path (SPEC 3): no reducer
// reads it and it decides nothing a thread does. Exact totals come from the
// session-cumulative counts a CLI reports (claude's result.modelUsage),
// diffed per session id, so a resumed session, a restart of fyrd and a
// replayed result all count once.
package usage

import (
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/store"
)

// Sources of a row: a thread's turn, or a call Fyr makes itself.
const (
	SourceTurn        = "turn"
	SourceCalibration = "calibration"
)

// SourceJob is the source of a system job's call (core.JobTitle, …).
func SourceJob(job string) string { return "job:" + job }

// Triggers: what started a turn.
const (
	TriggerOwner      = "owner"      // the owner's message
	TriggerThread     = "thread"     // another thread's agent wrote it
	TriggerView       = "view"       // a press in the live view
	TriggerHandover   = "handover"   // Fyr's handover brief
	TriggerNotice     = "notice"     // the feed's notices (CI, a review, a change)
	TriggerWake       = "wake"       // a watch or a Rule woke the agent
	TriggerBackground = "background" // the CLI went on by itself (a background task ended)
)

// Parts of a turn.
const (
	PartMain  = "main"  // the main agent's requests
	PartOther = "other" // subagents and the CLI's own small-model calls
)

// Dims are the dimensions of a report, copied into each row as they are
// when it is written.
type Dims struct {
	At                                           time.Time
	Source, Trigger, Thread, Project, Provider   string
	Account, Billing, Adapter, MainModel, Effort string
}

// Fold diffs a report against the session's baseline. A cumulative report
// gives now − baseline per model. A per-turn report is its own delta and
// leaves the baseline as it was.
//
// The CLI restores a session's counts only when it resumes it from the same
// login on the same host; a session copied to another account or host
// starts again from zero under the same id. Fold reads that as a reset
// (delta = now) when a count went down, or when the main model's count
// grew by less than the main agent's own share of this turn, which a
// session that kept counting cannot do.
func Fold(base map[string]cli.TokenCount, r cli.UsageReport, mainModel string) (delta, next map[string]cli.TokenCount) {
	delta = map[string]cli.TokenCount{}
	if r.Scope != cli.Cumulative {
		for m, c := range r.Models {
			if !c.Zero() {
				delta[m] = c
			}
		}
		return delta, base
	}
	reset := false
	for m, now := range r.Models {
		if now.Less(base[m]) {
			reset = true
		}
	}
	if now, ok := r.Models[mainModel]; ok && r.Main != nil {
		need := base[mainModel].Add(*r.Main)
		if now.Input < need.Input || now.Output < need.Output || now.CacheRead < need.CacheRead {
			reset = true
		}
	}
	next = make(map[string]cli.TokenCount, len(base)+len(r.Models))
	if !reset {
		for m, c := range base {
			next[m] = c
		}
	}
	for m, now := range r.Models {
		d := now
		if !reset {
			d = now.Sub(base[m])
		}
		if !d.Zero() {
			delta[m] = d
		}
		next[m] = now
	}
	return delta, next
}

// Split gives each model's delta its parts. main is the main agent's share
// (nil when the CLI does not say it); it belongs to mainModel, or, when
// that model has no delta, to the model with the most tokens. Without a
// main share every count is the main part.
func Split(delta map[string]cli.TokenCount, main *cli.TokenCount, mainModel string) map[string]map[string]cli.TokenCount {
	out := map[string]map[string]cli.TokenCount{}
	put := func(model, part string, c cli.TokenCount) {
		if c.Zero() {
			return
		}
		if out[model] == nil {
			out[model] = map[string]cli.TokenCount{}
		}
		out[model][part] = c
	}
	if main == nil {
		for m, c := range delta {
			put(m, PartMain, c)
		}
		return out
	}
	if _, ok := delta[mainModel]; !ok {
		mainModel = biggest(delta)
	}
	for m, c := range delta {
		if m == mainModel {
			mp := c.Min(*main)
			put(m, PartMain, mp)
			put(m, PartOther, c.Sub(mp))
			continue
		}
		put(m, PartOther, c)
	}
	return out
}

func biggest(delta map[string]cli.TokenCount) string {
	keys := make([]string, 0, len(delta))
	for m := range delta {
		keys = append(keys, m)
	}
	sort.Strings(keys) // ties go to the first name: the same answer every time
	best, n := "", int64(-1)
	for _, m := range keys {
		c := delta[m]
		if t := Total(c); t > n {
			best, n = m, t
		}
	}
	return best
}

// Total is a plain sum of the token kinds (thinking is part of output, and
// web requests are not tokens). It is a count, not a weight: a cache-read
// token and an output token weigh very differently against a quota.
func Total(c cli.TokenCount) int64 {
	return c.Input + c.Output + c.CacheRead + c.CacheWrite5m + c.CacheWrite1h
}

// Meter writes reports to the ledger and keeps each thread's total.
type Meter struct {
	st      *store.Store
	version string
	loc     *time.Location
	// OnRecord hears that a thread's total moved; optional.
	OnRecord func(thread string)
	Logf     func(format string, args ...any)

	mu     sync.Mutex
	totals map[string]cli.TokenCount
}

// New makes a meter over st; version is Fyr's build, loc the zone of the
// ledger's days (nil: the host's).
func New(st *store.Store, version string, loc *time.Location) *Meter {
	if loc == nil {
		loc = time.Local
	}
	return &Meter{st: st, version: version, loc: loc, totals: map[string]cli.TokenCount{}}
}

// Load reads every thread's total from the ledger.
func (m *Meter) Load() error {
	gs, err := m.st.UsageRollup([]string{"thread"}, store.UsageFilter{})
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, g := range gs {
		if t := g.Key["thread"]; t != "" {
			m.totals[t] = g.TokenCount
		}
	}
	return nil
}

// Threads lists the threads that have a total.
func (m *Meter) Threads() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.totals))
	for t := range m.totals {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// ThreadTotal is a thread's total so far; ok is false when it has none.
func (m *Meter) ThreadTotal(thread string) (cli.TokenCount, bool) {
	if m == nil {
		return cli.TokenCount{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.totals[thread]
	return c, ok
}

// Record writes one report. id names it once: a report with an id the
// ledger has seen writes nothing (a result the runner delivered again).
// It returns the rows it wrote.
func (m *Meter) Record(id string, d Dims, r cli.UsageReport) ([]store.UsageRow, error) {
	if id == "" {
		return nil, errors.New("usage: a report needs an id")
	}
	if d.At.IsZero() {
		d.At = time.Now()
	}
	if _, ok := r.Models[d.MainModel]; !ok {
		// The main model by another name (an alias, or a process adopted
		// after a restart that never saw the CLI's init): the model with
		// the most tokens in the session is the main agent's.
		d.MainModel = biggest(r.Models)
	}
	var rows []store.UsageRow
	session := r.Session
	if r.Scope != cli.Cumulative {
		session = "" // nothing to diff against
	}
	err := m.st.RecordUsage(id, session, d.At, func(raw []byte) ([]store.UsageRow, []byte, error) {
		base := map[string]cli.TokenCount{}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &base); err != nil {
				return nil, nil, err
			}
		}
		delta, next := Fold(base, r, d.MainModel)
		parts := Split(delta, r.Main, d.MainModel)
		models := make([]string, 0, len(parts))
		for model := range parts {
			models = append(models, model)
		}
		sort.Strings(models)
		for _, model := range models {
			for _, part := range []string{PartMain, PartOther} {
				c, ok := parts[model][part]
				if !ok {
					continue
				}
				rows = append(rows, store.UsageRow{ID: id + "/" + model + "/" + part, Report: id, At: d.At,
					Day: d.At.In(m.loc).Format("2006-01-02"), Source: d.Source, Trigger: d.Trigger, Thread: d.Thread,
					Project: d.Project, Provider: d.Provider, Account: d.Account, Billing: d.Billing, Adapter: d.Adapter,
					Session: r.Session, Model: model, Effort: d.Effort, Part: part, FyrVersion: m.version, TokenCount: c})
			}
		}
		var nb []byte
		if session != "" {
			b, err := json.Marshal(next)
			if err != nil {
				return nil, nil, err
			}
			nb = b
		}
		return rows, nb, nil
	})
	if errors.Is(err, store.ErrUsageSeen) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if d.Thread != "" && len(rows) > 0 {
		m.mu.Lock()
		t := m.totals[d.Thread]
		for _, r := range rows {
			t = t.Add(r.TokenCount)
		}
		m.totals[d.Thread] = t
		m.mu.Unlock()
		if m.OnRecord != nil {
			m.OnRecord(d.Thread)
		}
	}
	return rows, nil
}

// Seed gives a session that starts from another (a fork) the parent's
// baseline, so its first cumulative report does not count the parent's
// tokens again.
func (m *Meter) Seed(session, parent string) error {
	if session == "" || parent == "" || session == parent {
		return nil
	}
	b, err := m.st.UsageBaseline(parent)
	if err != nil || b == nil {
		return err
	}
	return m.st.SeedUsageBaseline(session, b, time.Now())
}

// Rollup sums the ledger by the given keys.
func (m *Meter) Rollup(group []string, f store.UsageFilter) ([]store.UsageGroup, error) {
	return m.st.UsageRollup(group, f)
}
