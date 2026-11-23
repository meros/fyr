package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/gitdiff"
	"github.com/meros/fyr/internal/provider"
)

// Trans-host handover helpers (decision 0039, V0.trans-host-handover).

// MaxSessionArchiveBytes bounds the CLI session archive sent across hosts for
// rung 1; larger sessions cleanly fall back to rung 2 (adopt).
const MaxSessionArchiveBytes = 20 << 20

func cliFamily(c string) string {
	if c == "" {
		return "claude"
	}
	return c
}

// readHandoverGit reads the checkout's git summary for cross-host handover, or
// nil when dir is not inside a git repo.
func readHandoverGit(dir string) *HandoverGit {
	if dir == "" {
		return nil
	}
	g := gitFacts(dir)
	if g.Branch == "" {
		return nil
	}
	hg := &HandoverGit{
		Repo:     g.Repo,
		Branch:   g.Branch,
		Default:  g.Default,
		Head:     g.Head,
		Ahead:    g.Ahead,
		Unpushed: g.Unpushed,
		Dirty:    g.Dirty,
	}
	if g.Path != "" {
		if fi, err := os.Lstat(filepath.Join(g.Path, ".git")); err == nil && fi.Mode().IsRegular() {
			hg.Worktree = true
		}
	}
	return hg
}

// ExportAgentData is the agent-owned half of a cross-host handover bundle.
type ExportAgentData struct {
	CLI            string       `json:"cli,omitempty"`
	Git            *HandoverGit `json:"git,omitempty"`
	Conversation   string       `json:"conversation,omitempty"`
	SessionArchive []byte       `json:"sessionArchive,omitempty"`
	SessionNote    string       `json:"sessionNote,omitempty"`
	OpenChanges    string       `json:"openChanges,omitempty"`
	Brief          string       `json:"brief,omitempty"`
}

// HandoverExport gathers the source thread's session archive, conversation
// Markdown and git facts, or runs a rung-3 Brief when wantBrief is true.
func (m *Manager) HandoverExport(threadID string, wantBrief bool, targetLabel string) (ExportAgentData, error) {
	t, ok := m.host.Thread(threadID)
	if !ok {
		return ExportAgentData{}, fmt.Errorf("no thread %s", threadID)
	}
	if wantBrief {
		if targetLabel == "" {
			targetLabel = "another host"
		}
		brief, err := m.brief(threadID, provider.Profile{Name: targetLabel})
		if err != nil {
			return ExportAgentData{}, fmt.Errorf("the current agent could not write a handover: %w", err)
		}
		return ExportAgentData{Brief: brief}, nil
	}
	from := m.cfg.Profile(t.Profile)
	out := ExportAgentData{
		CLI:         cliID(from.CLI),
		Git:         readHandoverGit(t.Cwd),
		OpenChanges: core.OpenChangesText(&t),
	}
	if src, err := m.resumable(t, from); err != nil {
		out.SessionNote = err.Error()
	} else if ad, aerr := m.adapter(from); aerr != nil {
		out.SessionNote = aerr.Error()
	} else if sa, ok := ad.(cli.SessionArchiver); !ok {
		out.SessionNote = "Session transfer is not supported for " + cliID(from.CLI) + " sessions"
	} else if arc, err := sa.ExportSession(src, m.sessionsDir(from, ad), t.SessionID, MaxSessionArchiveBytes); err != nil {
		if errors.Is(err, cli.ErrSessionTooLarge) {
			out.SessionNote = "Session transcript is too large to transfer"
		} else {
			out.SessionNote = "Could not export the session: " + err.Error()
		}
	} else {
		out.SessionArchive = arc
	}
	if c, ok := m.host.(Conversations); ok {
		if conv, err := c.Conversation(threadID); err == nil {
			out.Conversation = conv
		}
	}
	return out, nil
}

// PrepareTargetWorktree creates or reuses a worktree for git.Branch inside
// targetDir when targetDir is a checkout of the same repo and the branch
// exists locally or on origin. It never fails the handover: if the branch is
// unpushed or missing, it stays in targetDir and returns an explanatory note.
func PrepareTargetWorktree(ctx context.Context, targetDir string, g *HandoverGit) (actualDir, note string) {
	if g == nil || g.Branch == "" || (g.Default != "" && g.Branch == g.Default) {
		return targetDir, ""
	}
	tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	git := func(args ...string) (string, bool) {
		out, err := gitdiff.Output(tctx, targetDir, args...)
		return strings.TrimSpace(out), err == nil
	}
	top, ok := git("rev-parse", "--show-toplevel")
	if !ok || top == "" {
		return targetDir, ""
	}
	if g.Repo != "" {
		if u, ok := git("remote", "get-url", "origin"); ok && u != "" {
			if NormalizeRemote(u) != g.Repo {
				return targetDir, ""
			}
		}
	}
	from := ""
	if _, ok := git("rev-parse", "--verify", "--quiet", "refs/heads/"+g.Branch); !ok {
		// Best-effort fetch of the branch from origin.
		_, _ = runGit(tctx, targetDir, "fetch", "--no-tags", "origin", g.Branch)
		if _, ok := git("rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+g.Branch); ok {
			from = "origin/" + g.Branch
		} else {
			return targetDir, fmt.Sprintf("Branch %s was not found on origin, so the thread stayed in %s", g.Branch, targetDir)
		}
	}
	ws, err := MakeWorktree(tctx, WorktreeReq{Dir: targetDir, Branch: g.Branch, From: from})
	if err != nil {
		return targetDir, fmt.Sprintf("Could not prepare worktree for %s (%v), so the thread stayed in %s", g.Branch, err, targetDir)
	}
	return ws.Path, fmt.Sprintf("Checked out branch %s in worktree %s", g.Branch, ws.Path)
}

// HostMoveInfo holds the facts injected into the target thread after a
// trans-host handover (decision 0039, V0.trans-host-handover).
type HostMoveInfo struct {
	FromHost     string
	FromCwd      string
	ToHost       string
	ToCwd        string
	Git          *HandoverGit
	WorktreeNote string
	OpenChanges  string
}

// FormatHostMoveNotice renders the host-move and git-limitation notice
// injected into the target thread (without the leading "[Fyr] " prefix so it
// fits cleanly in core.NoticeText as well as handover messages).
func FormatHostMoveNotice(in HostMoveInfo) string {
	fromHost := in.FromHost
	if fromHost == "" {
		fromHost = "another host"
	}
	toHost := in.ToHost
	if toHost == "" {
		toHost = "this host"
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("Host handover: the owner moved this thread from host %s (%s) to %s (%s).", fromHost, in.FromCwd, toHost, in.ToCwd))
	if in.Git != nil && in.Git.Branch != "" {
		branch := in.Git.Branch
		if in.Git.Head != "" {
			branch += " (" + in.Git.Head + ")"
		}
		var stayed []string
		if in.Git.Dirty > 0 {
			stayed = append(stayed, pluralCount(in.Git.Dirty, "uncommitted file", "uncommitted files"))
		}
		if in.Git.Unpushed > 0 {
			stayed = append(stayed, pluralCount(in.Git.Unpushed, "unpushed commit", "unpushed commits"))
		}
		if len(stayed) > 0 {
			parts = append(parts, fmt.Sprintf("Source git state at handover: branch %s with %s that stayed on %s.", branch, strings.Join(stayed, " and "), fromHost))
		} else {
			parts = append(parts, fmt.Sprintf("Source git state at handover: branch %s (clean and pushed).", branch))
		}
	}
	if in.WorktreeNote != "" {
		parts = append(parts, in.WorktreeNote+".")
	}
	parts = append(parts, fmt.Sprintf("Local files outside git, uncommitted edits, unpushed commits, and running processes on %s did not move with this thread. Verify your working directory and git status before making changes.", fromHost))
	if strings.TrimSpace(in.OpenChanges) != "" {
		parts = append(parts, strings.TrimSpace(in.OpenChanges))
	}
	return strings.Join(parts, "\n")
}

func pluralCount(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// HandoverInReq asks the target host's agent manager to bring a newly imported
// thread live via the 3-rung ladder (decision 0039, V0.trans-host-handover).
type HandoverInReq struct {
	FromHostID     string
	FromHostName   string
	FromThreadID   string
	FromProfile    string
	FromCwd        string
	FromCLI        string
	ToHostID       string
	ToHostName     string
	Profile        string
	Model          string
	Effort         *string
	SessionID      string
	SessionArchive []byte
	SessionNote    string
	Conversation   string
	Brief          string
	Git            *HandoverGit
	WorktreeNote   string
	OpenChanges    string
}

// HandoverInResult reports which rung succeeded (or that a rung-3 Brief is
// needed from the source host).
type HandoverInResult struct {
	Rung      string `json:"rung,omitempty"`
	Path      string `json:"path,omitempty"`
	Note      string `json:"note,omitempty"`
	NeedBrief bool   `json:"needBrief,omitempty"`
}

// HandoverIn runs the 3-rung ladder (resume → adopt → brief) for a thread
// handed over from another host, injecting the host-move context into the
// target thread on every rung.
func (m *Manager) HandoverIn(threadID string, req HandoverInReq) (HandoverInResult, error) {
	t, ok := m.host.Thread(threadID)
	if !ok {
		return HandoverInResult{}, fmt.Errorf("no thread %s", threadID)
	}
	to := m.cfg.Profile(req.Profile)
	model := req.Model
	if model == "" {
		model = to.DefaultModel
	}
	asked := t.Effort
	if req.Effort != nil {
		asked = *req.Effort
	}
	effort := cli.EffortFor(to.ModelCaps, model, asked, to.DefaultEffort)
	moveNotice := FormatHostMoveNotice(HostMoveInfo{
		FromHost:     req.FromHostName,
		FromCwd:      req.FromCwd,
		ToHost:       req.ToHostName,
		ToCwd:        t.Cwd,
		Git:          req.Git,
		WorktreeNote: req.WorktreeNote,
		OpenChanges:  req.OpenChanges,
	})
	var notes []string
	if req.SessionNote != "" {
		notes = append(notes, req.SessionNote)
	}
	if req.WorktreeNote != "" {
		notes = append(notes, req.WorktreeNote)
	}

	finish := func(rung, sid, path, first, hostNotice string) HandoverInResult {
		note := strings.Join(notes, "; ")
		data := map[string]any{
			"from":       req.FromProfile,
			"to":         req.Profile,
			"fromHost":   req.FromHostName,
			"fromHostId": req.FromHostID,
			"fromThread": req.FromThreadID,
			"fromCwd":    req.FromCwd,
			"toHost":     req.ToHostName,
			"toHostId":   req.ToHostID,
			"toCwd":      t.Cwd,
			"model":      model,
			"rung":       rung,
		}
		if effort != "" {
			data["effort"] = effort
		}
		if path != "" {
			data["path"] = path
		}
		if note != "" {
			data["note"] = note
		}
		if req.Git != nil {
			data["git"] = req.Git
		}
		m.observe(threadID, core.KindHandoverDone, core.HandoverDoneArgs{
			Profile:    req.Profile,
			Model:      model,
			SessionID:  sid,
			Effort:     req.Effort,
			Data:       data,
			HostNotice: hostNotice,
			FirstMessage: first != "",
		})
		m.mu.Lock()
		s := m.sessions[threadID]
		m.mu.Unlock()
		if first == "" {
			if s != nil {
				m.armIdle(s)
			}
			return HandoverInResult{Rung: rung, Path: path, Note: note}
		}
		if err := m.send(threadID, core.Effect{Kind: core.EffectSend, Text: first, From: FromHandover}, nil); err != nil {
			log.Printf("thread %s: cross-host handover first message: %v", threadID, err)
		}
		return HandoverInResult{Rung: rung, Path: path, Note: note}
	}

	// Rung 1: import the session archive under the target cwd and resume.
	fromCLI, toCLI := cliID(req.FromCLI), cliID(to.CLI)
	if fromCLI != toCLI {
		notes = append(notes, "The target runs another CLI")
	} else if req.SessionID != "" && len(req.SessionArchive) > 0 {
		if ad, aerr := m.adapter(to); aerr != nil {
			notes = append(notes, aerr.Error())
		} else if sa, ok := ad.(cli.SessionArchiver); !ok {
			notes = append(notes, "The target cannot import a "+toCLI+" session")
		} else if err := sa.ImportSession(req.SessionArchive, m.sessionsDir(to, ad), t.Cwd, req.SessionID, MaxSessionArchiveBytes); err != nil {
			notes = append(notes, "Could not import the session: "+err.Error())
		} else if _, err := m.ensureAs(threadID, &as{profile: to, model: model, session: req.SessionID, resume: true, effort: effort}); err != nil {
			notes = append(notes, "Resume failed: "+core.TruncHead(firstLine(err.Error()), 300))
		} else {
			return finish(RungResume, req.SessionID, "", "", moveNotice), nil
		}
	}

	fromLabel := req.FromProfile
	if req.FromHostName != "" {
		fromLabel = fmt.Sprintf("%s on %s", req.FromProfile, req.FromHostName)
	}

	// Rung 2: target reads the exported conversation Markdown.
	if strings.TrimSpace(req.Conversation) != "" {
		path, err := m.writeHandoverFile(threadID, "conversation", req.Conversation)
		if err == nil {
			sid := newUUID()
			if _, err := m.ensureAs(threadID, &as{profile: to, model: model, session: sid, effort: effort, addDirs: []string{filepath.Dir(path)}}); err != nil {
				return HandoverInResult{}, fmt.Errorf("%s could not start: %w", name(to), err)
			}
			first := fmt.Sprintf("[Fyr] %s\n\nHandover: the owner handed this thread over to you from %s. Your session is new: the conversation so far is in %s (Fyr wrote it from the thread's log). Read it, then continue the work from where it stopped. Start your reply with a short summary of where things stand.", moveNotice, fromLabel, path)
			return finish(RungAdopt, sid, path, first, ""), nil
		}
		notes = append(notes, "Could not write the conversation: "+err.Error())
	} else {
		notes = append(notes, "the conversation is empty")
	}

	// Rung 3: Brief from source agent. If not supplied yet, ask the front to
	// request a Brief from the source host before retrying.
	if strings.TrimSpace(req.Brief) == "" {
		return HandoverInResult{NeedBrief: true, Note: strings.Join(notes, "; ")}, nil
	}
	path, perr := m.writeHandoverFile(threadID, "brief", req.Brief)
	sid := newUUID()
	o := &as{profile: to, model: model, session: sid, effort: effort}
	saved := ""
	if perr == nil {
		o.addDirs = []string{filepath.Dir(path)}
		saved = " (also saved at " + path + ")"
	}
	if _, err := m.ensureAs(threadID, o); err != nil {
		return HandoverInResult{}, fmt.Errorf("%s could not start: %w", name(to), err)
	}
	first := fmt.Sprintf("[Fyr] %s\n\nHandover: the owner handed this thread over to you from %s. Your session is new; the previous agent wrote this handover%s:\n\n%s\n\nContinue the work from there. Start your reply with a short summary of where things stand.", moveNotice, fromLabel, saved, req.Brief)
	return finish(RungBrief, sid, path, first, ""), nil
}
