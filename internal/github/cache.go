package github

// V0.persist-runtime-state: durable backing for GitHub caches and rate-limit
// state that outlive a fyrd restart, each with an explicit expiry.

import (
	"encoding/json"
	"github.com/meros/fyr/internal/kvstore"
	"sort"
	"sync"
	"time"
)

const (
	// CIKeep is how long Fyr remembers that a repo has CI across restarts.
	CIKeep = 7 * 24 * time.Hour
	// DefaultBranchRecheck is how long a repo's default branch is kept.
	DefaultBranchRecheck = 24 * time.Hour
	// RepoDeploysKeep is how long Fyr remembers that a repo uses deployments.
	RepoDeploysKeep = 7 * 24 * time.Hour
	// FinalStatusKeep is how long a terminal deployment status is kept.
	FinalStatusKeep = 7 * 24 * time.Hour
	finalStatusMax  = 500
	// DeployAtKeep bounds persisted deploy-watch poll timestamps (the longest
	// DeployEvery interval).
	DeployAtKeep = 30 * time.Minute
	// NotesKeep is how long cached failing-check annotations survive restarts.
	NotesKeep = 24 * time.Hour

	ciReposKey         = "github.ciRepos"
	baseRulesKey       = "github.baseRules"
	defaultBranchesKey = "github.defaultBranches"
	repoDeploysKey     = "github.repoDeploys"
	finalStatusKey     = "github.finalStatus"
	budgetKey          = "github.budget"
	deployAtKey        = "github.deployAt"
	notesKey           = "github.notes"
)

var (
	cacheMu  sync.Mutex
	cacheKV  kvstore.Table
	cacheNow = time.Now
)

func nowCache() time.Time {
	cacheMu.Lock()
	fn := cacheNow
	cacheMu.Unlock()
	if fn != nil {
		return fn()
	}
	return time.Now()
}

// setCacheNow replaces the package cache clock (tests). Passing nil restores
// time.Now.
func setCacheNow(fn func() time.Time) {
	cacheMu.Lock()
	if fn == nil {
		cacheNow = time.Now
	} else {
		cacheNow = fn
	}
	cacheMu.Unlock()
}

// UseCache backs the package-level GitHub caches with kv and loads their
// non-expired entries, replacing any in-memory entries (called at fyrd start
// and in restart tests). Passing nil clears the caches and disables backing.
func UseCache(kv kvstore.Table) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cacheKV = kv
	ciRepos.Clear()
	baseRules.Clear()
	defaultBranches.Clear()
	repoDeploys.Clear()
	finalStatus.Clear()
	if kv == nil {
		return
	}
	now := time.Now()
	if cacheNow != nil {
		now = cacheNow()
	}
	loadCIReposLocked(kv, now)
	loadBaseRulesLocked(kv, now)
	loadDefaultBranchesLocked(kv, now)
	loadRepoDeploysLocked(kv, now)
	loadFinalStatusLocked(kv, now)
}

type savedCIFact struct {
	Has bool      `json:"has"`
	At  time.Time `json:"at"`
}

func (f ciFact) valid(now time.Time) bool {
	if f.has {
		return f.at.IsZero() || now.Sub(f.at) < CIKeep
	}
	return !f.at.IsZero() && now.Sub(f.at) < CIRecheck
}

func loadCIReposLocked(kv kvstore.Table, now time.Time) {
	raw, _ := kv.GetKV(ciReposKey)
	if raw == "" {
		return
	}
	var m map[string]savedCIFact
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	for k, v := range m {
		f := ciFact{has: v.Has, at: v.At}
		if f.valid(now) {
			ciRepos.Store(k, f)
		}
	}
}

func saveCIRepos() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cacheKV == nil {
		return
	}
	now := time.Now()
	if cacheNow != nil {
		now = cacheNow()
	}
	m := map[string]savedCIFact{}
	ciRepos.Range(func(k, v any) bool {
		f := v.(ciFact)
		if f.valid(now) {
			m[k.(string)] = savedCIFact{Has: f.has, At: f.at}
		} else {
			ciRepos.Delete(k)
		}
		return true
	})
	if len(m) == 0 {
		cacheKV.SetKV(ciReposKey, "")
		return
	}
	if b, err := json.Marshal(m); err == nil {
		cacheKV.SetKV(ciReposKey, string(b))
	}
}

type savedRuleSet struct {
	Types     []string  `json:"types,omitempty"`
	Required  []string  `json:"required,omitempty"`
	Protected bool      `json:"protected,omitempty"`
	Approvals int       `json:"approvals,omitempty"`
	Threads   bool      `json:"threads,omitempty"`
	At        time.Time `json:"at"`
}

func loadBaseRulesLocked(kv kvstore.Table, now time.Time) {
	raw, _ := kv.GetKV(baseRulesKey)
	if raw == "" {
		return
	}
	var m map[string]savedRuleSet
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	for k, v := range m {
		if !v.At.IsZero() && now.Sub(v.At) < RulesRecheck {
			baseRules.Store(k, ruleSet{
				types: v.Types, required: v.Required, protected: v.Protected,
				approvals: v.Approvals, threads: v.Threads, at: v.At,
			})
		}
	}
}

func saveBaseRules() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cacheKV == nil {
		return
	}
	now := time.Now()
	if cacheNow != nil {
		now = cacheNow()
	}
	m := map[string]savedRuleSet{}
	baseRules.Range(func(k, v any) bool {
		rs := v.(ruleSet)
		if !rs.at.IsZero() && now.Sub(rs.at) < RulesRecheck {
			m[k.(string)] = savedRuleSet{
				Types: rs.types, Required: rs.required, Protected: rs.protected,
				Approvals: rs.approvals, Threads: rs.threads, At: rs.at,
			}
		} else {
			baseRules.Delete(k)
		}
		return true
	})
	if len(m) == 0 {
		cacheKV.SetKV(baseRulesKey, "")
		return
	}
	if b, err := json.Marshal(m); err == nil {
		cacheKV.SetKV(baseRulesKey, string(b))
	}
}

type defaultBranchFact struct {
	branch string
	at     time.Time
}

func (f defaultBranchFact) valid(now time.Time) bool {
	return f.branch != "" && (f.at.IsZero() || now.Sub(f.at) < DefaultBranchRecheck)
}

type savedDefaultBranch struct {
	Branch string    `json:"branch"`
	At     time.Time `json:"at"`
}

func loadDefaultBranchesLocked(kv kvstore.Table, now time.Time) {
	raw, _ := kv.GetKV(defaultBranchesKey)
	if raw == "" {
		return
	}
	var m map[string]savedDefaultBranch
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	for k, v := range m {
		f := defaultBranchFact{branch: v.Branch, at: v.At}
		if f.valid(now) {
			defaultBranches.Store(k, f)
		}
	}
}

func saveDefaultBranches() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cacheKV == nil {
		return
	}
	now := time.Now()
	if cacheNow != nil {
		now = cacheNow()
	}
	m := map[string]savedDefaultBranch{}
	defaultBranches.Range(func(k, v any) bool {
		f := v.(defaultBranchFact)
		if f.valid(now) {
			m[k.(string)] = savedDefaultBranch{Branch: f.branch, At: f.at}
		} else {
			defaultBranches.Delete(k)
		}
		return true
	})
	if len(m) == 0 {
		cacheKV.SetKV(defaultBranchesKey, "")
		return
	}
	if b, err := json.Marshal(m); err == nil {
		cacheKV.SetKV(defaultBranchesKey, string(b))
	}
}

type savedDeploysFact struct {
	Has bool      `json:"has"`
	At  time.Time `json:"at"`
}

func (f deploysFact) valid(now time.Time) bool {
	if f.has {
		return f.at.IsZero() || now.Sub(f.at) < RepoDeploysKeep
	}
	return !f.at.IsZero() && now.Sub(f.at) < RepoDeploysRecheck
}

func loadRepoDeploysLocked(kv kvstore.Table, now time.Time) {
	raw, _ := kv.GetKV(repoDeploysKey)
	if raw == "" {
		return
	}
	var m map[string]savedDeploysFact
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	for k, v := range m {
		f := deploysFact{has: v.Has, at: v.At}
		if f.valid(now) {
			repoDeploys.Store(k, f)
		}
	}
}

func saveRepoDeploys() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cacheKV == nil {
		return
	}
	now := time.Now()
	if cacheNow != nil {
		now = cacheNow()
	}
	m := map[string]savedDeploysFact{}
	repoDeploys.Range(func(k, v any) bool {
		f := v.(deploysFact)
		if f.valid(now) {
			m[k.(string)] = savedDeploysFact{Has: f.has, At: f.at}
		} else {
			repoDeploys.Delete(k)
		}
		return true
	})
	if len(m) == 0 {
		cacheKV.SetKV(repoDeploysKey, "")
		return
	}
	if b, err := json.Marshal(m); err == nil {
		cacheKV.SetKV(repoDeploysKey, string(b))
	}
}

type savedStatusRead struct {
	State  string    `json:"state"`
	EnvURL string    `json:"envURL,omitempty"`
	LogURL string    `json:"logURL,omitempty"`
	At     time.Time `json:"at"`
}

func (s statusRead) valid(now time.Time) bool {
	return s.State != "" && (s.at.IsZero() || now.Sub(s.at) < FinalStatusKeep)
}

func loadFinalStatusLocked(kv kvstore.Table, now time.Time) {
	raw, _ := kv.GetKV(finalStatusKey)
	if raw == "" {
		return
	}
	var m map[string]savedStatusRead
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	for k, v := range m {
		s := statusRead{State: v.State, EnvURL: v.EnvURL, LogURL: v.LogURL, at: v.At}
		if s.valid(now) {
			finalStatus.Store(k, s)
		}
	}
}

func saveFinalStatus() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cacheKV == nil {
		return
	}
	now := time.Now()
	if cacheNow != nil {
		now = cacheNow()
	}
	type item struct {
		k string
		v savedStatusRead
	}
	var items []item
	finalStatus.Range(func(k, v any) bool {
		s := v.(statusRead)
		if s.valid(now) {
			items = append(items, item{k: k.(string), v: savedStatusRead{State: s.State, EnvURL: s.EnvURL, LogURL: s.LogURL, At: s.at}})
		} else {
			finalStatus.Delete(k)
		}
		return true
	})
	if len(items) > finalStatusMax {
		sort.Slice(items, func(i, j int) bool { return items[i].v.At.After(items[j].v.At) })
		for _, drop := range items[finalStatusMax:] {
			finalStatus.Delete(drop.k)
		}
		items = items[:finalStatusMax]
	}
	if len(items) == 0 {
		cacheKV.SetKV(finalStatusKey, "")
		return
	}
	m := make(map[string]savedStatusRead, len(items))
	for _, it := range items {
		m[it.k] = it.v
	}
	if b, err := json.Marshal(m); err == nil {
		cacheKV.SetKV(finalStatusKey, string(b))
	}
}
