package github

// Merged to live (SPEC 0.5 V0.66): GitHub Deployments of a merge commit, and
// the annotations of a failing PR's check runs.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/core"
)

const (
	maxNoteRuns = 10
	maxNotes    = 50
)

// DeploySource is what the poller needs to watch merge commits; a Source may
// also be one.
type DeploySource interface {
	Deploys() []DeployThread
	ObserveDeploy(threadID, changeID string, d core.DeployObs)
}

// DeployThread is one watched merge commit of a thread's change.
type DeployThread struct {
	ID, Change, Repo, SHA string
	First                 bool
	Since                 time.Time // the merge (V0.173)
}

type noteCache struct {
	sha    string
	failed int
	notes  []core.Annotation
	at     time.Time
}

type savedNoteCache struct {
	SHA    string            `json:"sha"`
	Failed int               `json:"failed"`
	Notes  []core.Annotation `json:"notes,omitempty"`
	At     time.Time         `json:"at"`
}

func (p *Poller) loadState() {
	if p.loaded {
		return
	}
	p.loaded = true
	if p.KV == nil {
		return
	}
	now := p.now()
	if raw, _ := p.KV.GetKV(deployAtKey); raw != "" {
		var m map[string]time.Time
		if json.Unmarshal([]byte(raw), &m) == nil {
			for k, t := range m {
				if !t.IsZero() && now.Sub(t) < DeployAtKeep {
					if p.deployAt == nil {
						p.deployAt = map[string]time.Time{}
					}
					p.deployAt[k] = t
				}
			}
		}
	}
	if raw, _ := p.KV.GetKV(notesKey); raw != "" {
		var m map[string]savedNoteCache
		if json.Unmarshal([]byte(raw), &m) == nil {
			for k, v := range m {
				if !v.At.IsZero() && now.Sub(v.At) < NotesKeep {
					if p.notes == nil {
						p.notes = map[string]noteCache{}
					}
					p.notes[k] = noteCache{sha: v.SHA, failed: v.Failed, notes: v.Notes, at: v.At}
				}
			}
		}
	}
}

func (p *Poller) saveDeployAt() {
	if p.KV == nil {
		return
	}
	now := p.now()
	m := map[string]time.Time{}
	for k, t := range p.deployAt {
		if !t.IsZero() && now.Sub(t) < DeployAtKeep {
			m[k] = t
		}
	}
	if len(m) == 0 {
		p.KV.SetKV(deployAtKey, "")
		return
	}
	if b, err := json.Marshal(m); err == nil {
		p.KV.SetKV(deployAtKey, string(b))
	}
}

func (p *Poller) saveNotes() {
	if p.KV == nil {
		return
	}
	now := p.now()
	m := map[string]savedNoteCache{}
	for k, c := range p.notes {
		if c.at.IsZero() || now.Sub(c.at) < NotesKeep {
			m[k] = savedNoteCache{SHA: c.sha, Failed: c.failed, Notes: c.notes, At: c.at}
		}
	}
	if len(m) == 0 {
		p.KV.SetKV(notesKey, "")
		return
	}
	if b, err := json.Marshal(m); err == nil {
		p.KV.SetKV(notesKey, string(b))
	}
}

// annotations are the PR's, read again only when its head or its number of
// failed checks changed.
func (p *Poller) annotations(ctx context.Context, url string, pr core.PR, failed int) []core.Annotation {
	now := p.now()
	if c, ok := p.notes[url]; ok && c.sha == pr.Head && c.failed == failed && (c.at.IsZero() || now.Sub(c.at) < NotesKeep) {
		return c.notes
	}
	notes, err := FetchAnnotations(ctx, p.Gh, pr.Repo, pr.Head)
	if err != nil {
		if !errors.Is(err, ErrBudget) {
			log.Printf("annotations %s: %v", url, err)
		}
		return nil
	}
	if p.notes == nil {
		p.notes = map[string]noteCache{}
	}
	p.notes[url] = noteCache{sha: pr.Head, failed: failed, notes: notes, at: now}
	p.saveNotes()
	return notes
}

// DeployEvery is how often the poller reads a deploy watch, by the watch's
// age (V0.173): each poll in the first hour, every 10 minutes in its first
// day, then every 30 minutes.
func DeployEvery(age time.Duration) time.Duration {
	switch {
	case age < time.Hour:
		return 0
	case age < 24*time.Hour:
		return 10 * time.Minute
	}
	return 30 * time.Minute
}

func (p *Poller) pollDeploy(ctx context.Context, src DeploySource, d DeployThread) {
	p.loadState()
	now := p.now()
	key := d.Repo + "@" + d.SHA
	if last, ok := p.deployAt[key]; ok && !d.Since.IsZero() && now.Sub(last) < DeployEvery(now.Sub(d.Since)) {
		return
	}
	if p.deployAt == nil {
		p.deployAt = map[string]time.Time{}
	}
	p.deployAt[key] = now
	p.saveDeployAt()
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	obs, err := FetchDeployments(c, p.Gh, d.Repo, d.SHA, d.First)
	if errors.Is(err, ErrBudget) {
		delete(p.deployAt, key) // not read: due again at the next poll
		p.saveDeployAt()
		return
	}
	if err != nil {
		log.Printf("deployments %s@%s: %v", d.Repo, short(d.SHA), err)
		return
	}
	src.ObserveDeploy(d.ID, d.Change, obs)
}

// FetchAnnotations reads the annotations of the failed check runs of a
// commit: at most 10 runs and 50 annotations.
func FetchAnnotations(ctx context.Context, gh Runner, repo, sha string) ([]core.Annotation, error) {
	out, err := RestGet(ctx, gh, fmt.Sprintf("repos/%s/commits/%s/check-runs?per_page=100", repo, sha))
	if err != nil {
		return nil, err
	}
	var v struct {
		Runs []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			Conclusion string `json:"conclusion"`
			Output     struct {
				Count int `json:"annotations_count"`
			} `json:"output"`
		} `json:"check_runs"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("check runs: %w", err)
	}
	var notes []core.Annotation
	runs := 0
	for _, r := range v.Runs {
		if len(notes) >= maxNotes || runs >= maxNoteRuns {
			break
		}
		if r.Output.Count == 0 || (r.Conclusion != "failure" && r.Conclusion != "timed_out") {
			continue
		}
		runs++
		out, err := gh(ctx, "api", fmt.Sprintf("repos/%s/check-runs/%d/annotations?per_page=%d", repo, r.ID, maxNotes))
		if err != nil {
			return nil, err
		}
		var as []struct {
			Path    string `json:"path"`
			Start   int    `json:"start_line"`
			End     int    `json:"end_line"`
			Level   string `json:"annotation_level"`
			Message string `json:"message"`
			Title   string `json:"title"`
		}
		if err := json.Unmarshal(out, &as); err != nil {
			return nil, fmt.Errorf("annotations: %w", err)
		}
		for _, a := range as {
			if len(notes) >= maxNotes {
				break
			}
			n := core.Annotation{Path: a.Path, Line: a.Start, Level: a.Level, Message: a.Message, Title: a.Title, Check: r.Name}
			if a.End > a.Start {
				n.EndLine = a.End
			}
			notes = append(notes, n)
		}
	}
	return notes, nil
}

// RepoDeploysRecheck is how long Fyr trusts that a repo has no GitHub
// deployments (V0.173): a merge in it then arms no read at all.
const RepoDeploysRecheck = 24 * time.Hour

// repoDeploys is what Fyr read of each repo's deployments, by RepoKey.
var repoDeploys sync.Map

type deploysFact struct {
	has bool
	at  time.Time
}

// RepoHasDeploys reports what Fyr knows of the repo's deployments: known
// is false when it has not read them, or read that there are none more
// than RepoDeploysRecheck ago.
func RepoHasDeploys(repo string) (has, known bool) {
	v, ok := repoDeploys.Load(core.RepoKey(repo))
	if !ok {
		return false, false
	}
	f := v.(deploysFact)
	if !f.valid(nowCache()) {
		return false, false
	}
	return f.has, true
}

func markDeploys(repo string, has bool) {
	if k := core.RepoKey(repo); k != "" {
		now := nowCache()
		if v, ok := repoDeploys.Load(k); ok {
			if f := v.(deploysFact); f.has == has && !f.at.IsZero() && now.Sub(f.at) < time.Hour {
				return
			}
		}
		repoDeploys.Store(k, deploysFact{has: has, at: now})
		saveRepoDeploys()
	}
}

// finalStates are the deployment states that end a deployment's story for
// Fyr; their statuses are not read again (V0.173).
var finalStates = map[string]bool{"success": true, "inactive": true, "failure": true, "error": true}

// finalStatus caches the final status of a deployment, by repo and id.
var finalStatus sync.Map

type statusRead struct {
	State, EnvURL, LogURL string
	at                    time.Time
}

// FetchDeployments reads the deployments of a merge commit: the newest per
// environment and its newest status. First reads the repo's newest
// deployments instead, so a repo without any ends the watch in one call;
// V0.173: and in none once Fyr knows it has none. The reads send ETags, and
// a deployment whose status is final is not read again.
func FetchDeployments(ctx context.Context, gh Runner, repo, sha string, first bool) (core.DeployObs, error) {
	if first {
		if has, known := RepoHasDeploys(repo); known {
			if !has {
				return core.DeployObs{None: true}, nil
			}
			first = false
		}
	}
	q := fmt.Sprintf("repos/%s/deployments?sha=%s&per_page=30", repo, sha)
	if first {
		q = fmt.Sprintf("repos/%s/deployments?per_page=30", repo)
	}
	out, err := RestGet(ctx, gh, q)
	if err != nil {
		return core.DeployObs{}, err
	}
	var ds []struct {
		ID         int64  `json:"id"`
		SHA        string `json:"sha"`
		Env        string `json:"environment"`
		Production bool   `json:"production_environment"`
	}
	if err := json.Unmarshal(out, &ds); err != nil {
		return core.DeployObs{}, fmt.Errorf("deployments: %w", err)
	}
	if first {
		markDeploys(repo, len(ds) > 0)
		if len(ds) == 0 {
			return core.DeployObs{None: true}, nil
		}
	}
	var obs core.DeployObs
	seen := map[string]bool{}
	for _, d := range ds { // newest first
		if d.SHA != sha || seen[strings.ToLower(d.Env)] {
			continue
		}
		seen[strings.ToLower(d.Env)] = true
		x := core.Deployment{Env: d.Env, Production: core.IsProduction(d.Env, d.Production), State: "pending"}
		key := fmt.Sprintf("%s#%d", core.RepoKey(repo), d.ID)
		if v, ok := finalStatus.Load(key); ok {
			if st := v.(statusRead); st.valid(nowCache()) {
				x.State, x.URL, x.LogURL = st.State, st.EnvURL, st.LogURL
				obs.Deployments = append(obs.Deployments, x)
				continue
			}
		}
		out, err := RestGet(ctx, gh, fmt.Sprintf("repos/%s/deployments/%d/statuses?per_page=1", repo, d.ID))
		if err != nil {
			return core.DeployObs{}, err
		}
		var st []struct {
			State  string `json:"state"`
			EnvURL string `json:"environment_url"`
			LogURL string `json:"log_url"`
			Target string `json:"target_url"`
		}
		if err := json.Unmarshal(out, &st); err != nil {
			return core.DeployObs{}, fmt.Errorf("deployment statuses: %w", err)
		}
		if len(st) > 0 {
			x.State, x.URL, x.LogURL = st[0].State, st[0].EnvURL, st[0].LogURL
			if x.LogURL == "" {
				x.LogURL = st[0].Target
			}
			if finalStates[x.State] {
				finalStatus.Store(key, statusRead{State: x.State, EnvURL: x.URL, LogURL: x.LogURL, at: nowCache()})
				saveFinalStatus()
			}
		}
		obs.Deployments = append(obs.Deployments, x)
	}
	return obs, nil
}
