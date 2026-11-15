package github

// Merged to live (SPEC 0.5 V0.66): GitHub Deployments of a merge commit, and
// the annotations of a failing PR's check runs.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
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
}

type noteCache struct {
	sha    string
	failed int
	notes  []core.Annotation
}

// annotations are the PR's, read again only when its head or its number of
// failed checks changed.
func (p *Poller) annotations(ctx context.Context, url string, pr core.PR, failed int) []core.Annotation {
	if c, ok := p.notes[url]; ok && c.sha == pr.Head && c.failed == failed {
		return c.notes
	}
	notes, err := FetchAnnotations(ctx, p.Gh, pr.Repo, pr.Head)
	if err != nil {
		log.Printf("annotations %s: %v", url, err)
		return nil
	}
	if p.notes == nil {
		p.notes = map[string]noteCache{}
	}
	p.notes[url] = noteCache{sha: pr.Head, failed: failed, notes: notes}
	return notes
}

func (p *Poller) pollDeploy(ctx context.Context, src DeploySource, d DeployThread) {
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	obs, err := FetchDeployments(c, p.Gh, d.Repo, d.SHA, d.First)
	if err != nil {
		log.Printf("deployments %s@%s: %v", d.Repo, short(d.SHA), err)
		return
	}
	src.ObserveDeploy(d.ID, d.Change, obs)
}

// FetchAnnotations reads the annotations of the failed check runs of a
// commit: at most 10 runs and 50 annotations.
func FetchAnnotations(ctx context.Context, gh Runner, repo, sha string) ([]core.Annotation, error) {
	out, err := gh(ctx, "api", fmt.Sprintf("repos/%s/commits/%s/check-runs?per_page=100", repo, sha))
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

// FetchDeployments reads the deployments of a merge commit: the newest per
// environment and its newest status. First reads the repo's newest
// deployments instead, so a repo without any ends the watch in one call.
func FetchDeployments(ctx context.Context, gh Runner, repo, sha string, first bool) (core.DeployObs, error) {
	q := fmt.Sprintf("repos/%s/deployments?sha=%s&per_page=30", repo, sha)
	if first {
		q = fmt.Sprintf("repos/%s/deployments?per_page=30", repo)
	}
	out, err := gh(ctx, "api", q)
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
	if first && len(ds) == 0 {
		return core.DeployObs{None: true}, nil
	}
	var obs core.DeployObs
	seen := map[string]bool{}
	for _, d := range ds { // newest first
		if d.SHA != sha || seen[strings.ToLower(d.Env)] {
			continue
		}
		seen[strings.ToLower(d.Env)] = true
		out, err := gh(ctx, "api", fmt.Sprintf("repos/%s/deployments/%d/statuses?per_page=1", repo, d.ID))
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
		x := core.Deployment{Env: d.Env, Production: core.IsProduction(d.Env, d.Production), State: "pending"}
		if len(st) > 0 {
			x.State, x.URL, x.LogURL = st[0].State, st[0].EnvURL, st[0].LogURL
			if x.LogURL == "" {
				x.LogURL = st[0].Target
			}
		}
		obs.Deployments = append(obs.Deployments, x)
	}
	return obs, nil
}
