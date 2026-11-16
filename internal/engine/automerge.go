package engine

import (
	"encoding/json"
	"log"
	"maps"
	"slices"

	"github.com/meros/fyr/internal/core"
)

// autoMergeKey is the host kv key of the repos the owner opted in to
// auto-merge (V0.102): a JSON list of owner/name in lower case.
const autoMergeKey = "autoMerge.repos"

// AutoMergeRepo is one row of the setting: a repo Fyr knows from a PR, or one
// the owner opted in, and whether its merge asks merge with no press.
type AutoMergeRepo struct {
	Repo string `json:"repo"`
	On   bool   `json:"on"`
}

// loadAutoMerge reads the setting. Call it with e.mu held.
func (e *Engine) loadAutoMerge() {
	e.autoMerge = map[string]bool{}
	raw, _ := e.st.GetKV(autoMergeKey)
	if raw == "" {
		return
	}
	var repos []string
	if err := json.Unmarshal([]byte(raw), &repos); err != nil {
		log.Printf("auto-merge setting: %v", err)
		return
	}
	for _, r := range repos {
		if k := core.RepoKey(r); k != "" {
			e.autoMerge[k] = true
		}
	}
}

// setAutoMerge stores the owner's setting for one repo, then approves the
// asks that already wait in it.
func (e *Engine) setAutoMerge(repo string, on bool) error {
	e.mu.Lock()
	next := maps.Clone(e.autoMerge)
	if next == nil {
		next = map[string]bool{}
	}
	if on {
		next[repo] = true
	} else {
		delete(next, repo)
	}
	list := slices.Sorted(maps.Keys(next))
	b, _ := json.Marshal(list)
	v := string(b)
	if len(list) == 0 {
		v = ""
	}
	if err := e.st.SetKV(autoMergeKey, v); err != nil {
		e.mu.Unlock()
		return err
	}
	e.autoMerge = next
	ids := slices.Collect(maps.Keys(e.threads))
	e.mu.Unlock()
	if on {
		for _, id := range ids {
			e.autoApprove(id)
		}
	}
	return nil
}

// AutoMergeRepos is GET /api/autoMerge: the repos of the threads' PRs and
// the repos in the setting, by name.
func (e *Engine) AutoMergeRepos() []AutoMergeRepo {
	e.mu.Lock()
	defer e.mu.Unlock()
	seen := maps.Clone(e.autoMerge)
	if seen == nil {
		seen = map[string]bool{}
	}
	add := func(c *core.Change) {
		if c.PR == nil {
			return
		}
		if k := core.RepoKey(c.PR.Repo); k != "" && !seen[k] {
			seen[k] = false
		}
	}
	for _, t := range e.threads {
		if t.ArchivedAt != nil {
			continue
		}
		for i := range t.Changes {
			add(&t.Changes[i])
		}
	}
	for _, c := range e.records {
		add(c)
	}
	out := make([]AutoMergeRepo, 0, len(seen))
	for _, k := range slices.Sorted(maps.Keys(seen)) {
		out = append(out, AutoMergeRepo{Repo: k, On: seen[k]})
	}
	return out
}

// autoApprove sends the approval of each of the thread's merge asks that Fyr
// may merge now, in a repo of the setting (V0.102). publish calls it after
// every Input, so an ask approves when its checks turn green, when the run
// ends, or when the owner turns the setting on.
func (e *Engine) autoApprove(id string) {
	e.mu.Lock()
	t := e.threads[id]
	var asks []core.AutoMergeArgs
	if t != nil && len(t.Decisions) > 0 && len(e.autoMerge) > 0 {
		for _, a := range core.AutoMergeAsks(t) {
			if e.autoMerge[core.RepoKey(a.Repo)] {
				asks = append(asks, a)
			}
		}
	}
	e.mu.Unlock()
	for _, a := range asks {
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindAutoMerge, Thread: id, Args: a}); err != nil {
			log.Printf("thread %s: auto-merge %s: %v", id, a.Decision, err)
		}
	}
}
