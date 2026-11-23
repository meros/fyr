package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/hostfs"
	"github.com/meros/fyr/internal/store"
)

// Trans-host handover support in the engine (decision 0039, V0.trans-host-handover).

// MaxHandoverEvents caps how many newest lean events are copied to the target
// host so huge threads stay bounded on the wire.
const MaxHandoverEvents = 2000

// ExportedEvent is one timeline event sent in a cross-host handover bundle.
type ExportedEvent struct {
	At   time.Time       `json:"at"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// ExportedPR is the open main PR carried to the target thread so the PR tab
// and badge continue to work immediately.
type ExportedPR struct {
	URL    string `json:"url"`
	Repo   string `json:"repo,omitempty"`
	Number int    `json:"number,omitempty"`
	Title  string `json:"title,omitempty"`
	Branch string `json:"branch,omitempty"`
}

// ThreadExport is the engine-owned half of a cross-host handover bundle.
type ThreadExport struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	TitleBy   string          `json:"titleBy,omitempty"`
	Cwd       string          `json:"cwd"`
	Profile   string          `json:"profile"`
	Model     string          `json:"model,omitempty"`
	Effort    string          `json:"effort,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Branch    string          `json:"branch,omitempty"`
	Summary   string          `json:"summary,omitempty"`
	LiveView  bool            `json:"liveView,omitempty"`
	PR        *ExportedPR     `json:"pr,omitempty"`
	Events    []ExportedEvent `json:"events,omitempty"`
}

// CanHandover checks the thread.handover availability guard on id and returns
// the thread when available, or an error status (404/409) and reason.
func (e *Engine) CanHandover(id string) (core.Thread, int, string) {
	e.mu.Lock()
	t := e.threads[id]
	v := e.views[id]
	now := e.Now()
	var cp core.Thread
	var vcp core.View
	if t != nil {
		cp = *core.CloneThread(t)
	}
	if v != nil {
		vcp = *v
	}
	e.mu.Unlock()
	if t == nil {
		return core.Thread{}, 404, "no such thread"
	}
	if why := core.Registry["thread.handover"].Available(&cp, &vcp, now); why != "" {
		return core.Thread{}, 409, why
	}
	return cp, 200, ""
}

// ExportThread collects the thread's metadata, open main PR and newest lean
// events for a cross-host handover bundle.
func (e *Engine) ExportThread(id string) (ThreadExport, int, string) {
	t, code, why := e.CanHandover(id)
	if code != 200 {
		return ThreadExport{}, code, why
	}
	evs, err := e.Events(id, 0, MaxHandoverEvents)
	if err != nil {
		return ThreadExport{}, 500, err.Error()
	}
	lean := LeanAll(evs)
	outEvs := make([]ExportedEvent, 0, len(lean))
	for _, ev := range lean {
		// Drop per-host internal command/handover-lock noise only if empty; keep conversation events intact.
		if ev.Type == "command" {
			continue
		}
		outEvs = append(outEvs, ExportedEvent{At: ev.At, Type: ev.Type, Data: ev.Data})
	}
	out := ThreadExport{
		ID:        t.ID,
		Title:     t.Title,
		TitleBy:   t.TitleBy,
		Cwd:       t.Cwd,
		Profile:   t.Profile,
		Model:     t.Model,
		Effort:    t.Effort,
		SessionID: t.SessionID,
		Branch:    t.Branch,
		Summary:   t.Summary,
		LiveView:  t.ViewOn(),
		Events:    outEvs,
	}
	if p := t.MainPR(); p != nil && p.State == "OPEN" && p.URL != "" {
		out.PR = &ExportedPR{URL: p.URL, Repo: p.Repo, Number: p.Number, Title: p.Title, Branch: p.Branch}
	}
	return out, 200, ""
}

// HandInSpec describes the target thread to create on the destination host.
type HandInSpec struct {
	FromHostID   string
	FromHostName string
	FromThreadID string
	Title        string
	TitleBy      string
	Cwd          string
	ProjectID    string
	UseWorktree  bool
	Profile      string
	Model        string
	Effort       string
	Summary      string
	LiveView     bool
	PR           *ExportedPR
	Git          *agent.HandoverGit
	Events       []ExportedEvent
}

// PrepareHandIn resolves the target folder (explicit cwd, project folder, or
// fresh scratch folder), optionally creates/reuses a branch worktree, and
// creates the linked target thread with its copied conversation history via
// core.KindThreadHandedIn.
func (e *Engine) PrepareHandIn(ctx context.Context, spec HandInSpec) (threadID, actualCwd, projectID, worktreeNote string, status int, errMsg string) {
	if spec.Profile == "" {
		return "", "", "", "", 400, "profile is required"
	}
	if e.ProfileExists != nil && !e.ProfileExists(spec.Profile) {
		return "", "", "", "", 400, "no profile " + spec.Profile
	}
	now := e.Now()
	cwd := strings.TrimSpace(spec.Cwd)
	pid := strings.TrimSpace(spec.ProjectID)
	if pid != "" {
		e.mu.Lock()
		pr := e.projects[pid]
		e.mu.Unlock()
		if pr == nil {
			return "", "", "", "", 400, "no project " + pid
		}
		if cwd == "" && pr.Folder != "" {
			cwd = pr.Folder
		}
	}
	scratch := ""
	if cwd == "" {
		dir, err := e.makeScratch(spec.Title, now)
		if err != nil {
			return "", "", "", "", 500, "could not make a scratch folder: " + err.Error()
		}
		cwd, scratch = dir, dir
	} else {
		cwd = hostfs.ExpandHome(cwd, "")
		fi, err := os.Stat(cwd)
		if err != nil || !fi.IsDir() {
			return "", "", "", "", 400, fmt.Sprintf("folder %s does not exist on this host", cwd)
		}
	}
	actualCwd = cwd
	if spec.UseWorktree && scratch == "" && spec.Git != nil {
		actualCwd, worktreeNote = agent.PrepareTargetWorktree(ctx, cwd, spec.Git)
	}
	title := strings.TrimSpace(spec.Title)
	if title == "" {
		title = "Handed over thread"
	}
	tid := NewID("t_")
	t := &core.Thread{
		ID:           tid,
		Title:        title,
		TitleBy:      spec.TitleBy,
		Cwd:          actualCwd,
		Owner:        core.Owner,
		Visibility:   "private",
		Profile:      spec.Profile,
		Model:        spec.Model,
		Effort:       spec.Effort,
		Conversation: core.Conversation{Summary: spec.Summary},
		CreatedAt:    now,
		UpdatedAt:    now,
		ActivityAt:   now,
		HandedOverFrom: &core.HandoverLink{
			HostID:   spec.FromHostID,
			HostName: spec.FromHostName,
			ThreadID: spec.FromThreadID,
		},
	}
	if spec.LiveView {
		core.SetView(t, true)
	}
	if spec.PR != nil && spec.PR.URL != "" {
		repo, num := spec.PR.Repo, spec.PR.Number
		if repo == "" || num == 0 {
			if r, n, ok := core.ParsePR(spec.PR.URL); ok {
				repo, num = r, n
			}
		}
		if num > 0 {
			core.PRObserved(t, core.PR{
				URL:    spec.PR.URL,
				Repo:   repo,
				Number: num,
				Title:  spec.PR.Title,
				Branch: spec.PR.Branch,
				State:  "OPEN",
				Checks: core.ChecksPending,
			}, now)
		}
	}
	v := &core.View{User: core.Owner, Entity: tid, SeenAt: now}
	hist := make([]core.Event, 0, len(spec.Events))
	ats := make([]time.Time, 0, len(spec.Events))
	for _, ev := range spec.Events {
		if ev.Type == "" {
			continue
		}
		var data any
		if len(ev.Data) > 0 {
			_ = json.Unmarshal(ev.Data, &data)
		}
		hist = append(hist, core.Event{Type: ev.Type, Data: data})
		at := ev.At
		if at.IsZero() {
			at = now
		}
		ats = append(ats, at)
	}
	if _, err := e.Apply(core.Input{
		Source: core.Observer(core.ObsHost),
		Kind:   core.KindThreadHandedIn,
		Thread: tid,
		Args:   core.Adopted{Thread: t, View: v, History: hist, At: ats, Project: pid},
	}); err != nil {
		if scratch != "" {
			os.Remove(scratch)
		}
		return "", "", "", "", 500, err.Error()
	}
	e.mu.Lock()
	if got := e.threads[tid]; got != nil {
		projectID = got.ProjectID
	}
	e.mu.Unlock()
	return tid, actualCwd, projectID, worktreeNote, 200, ""
}

// RollbackHandIn removes a freshly imported thread whose target session could
// not start (or needs a rung-3 Brief first), leaving no orphan thread behind.
func (e *Engine) RollbackHandIn(id string) {
	if id == "" {
		return
	}
	e.mu.Lock()
	delete(e.threads, id)
	delete(e.views, id)
	delete(e.last, id)
	delete(e.revs, id)
	e.mu.Unlock()
	_, _ = e.st.Write(func(tx *store.Tx) error { return tx.DeleteThread(id) })
}
