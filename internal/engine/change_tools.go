package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/github"
	"github.com/meros/fyr/internal/mcp"
)

// The change tools of SPEC 0.5 V0.59 (5), decision 0018: one family for every
// change an agent makes or asks for. Git and GitHub are observed without
// them; the tools add intent, grouping and asks.

// mergeAskReadWait is how long a merge ask waits for its fresh read of the PR
// (V0.199); after it, the ask goes by the last read.
const mergeAskReadWait = 10 * time.Second

// readPRForMergeAsk reads the thread's PRs before an agent's merge ask is
// judged (V0.199). An agent asks right after it pushes, and the poller's last
// read can be from before the push: a conflict the push resolved, and the old
// head the ask would be bound to.
func (e *Engine) readPRForMergeAsk(ctx context.Context, th string) {
	if e.RefreshPR == nil {
		return
	}
	c, cancel := context.WithTimeout(ctx, mergeAskReadWait)
	defer cancel()
	e.RefreshPR(c, th)
}

const changeRef = "The change: its id (c_…), its key, its PR URL, owner/repo#N, or #N when one change of this thread has that PR."

// The tool descriptions of decision 0024 section 7 (SPEC V0.101), code only
// since V0.changes-code-only.
const (
	ChangeOpenDesc    = "Start a code change in its own git worktree (workspace: true, branch, repo?, after?): Fyr makes the worktree, returns its path and watches it. Or give Fyr a card for a code change it cannot see by itself: a branch you pushed some other way, or a PR you did not open with gh pr create. Fyr makes cards for your pushes and PRs itself. The same key, PR or branch returns the same change. Only code: a write the owner must approve (an apply, a deploy, a migration) is run_request; a document or a design to approve is ask."
	ChangeUpdateDesc  = "Tell Fyr what only you know about a code change. Send only what changed; send nothing when nothing moved. Fyr reads PR state, checks, reviews, conflicts and the pushed head itself; if you send one of these, Fyr replies with the value it reads. abandoned takes the card off the board."
	ChangeRequestDesc = "Ask the owner for an effect Fyr runs: the one way to ask for a merge or a Fyr deploy. It returns at once; the answer comes later as a [Fyr] line. The same open ask made again returns the one already open. ask \"merge\" {change: the change, or the PR URL, args?: {method: squash|merge|rebase, head?: <commit SHA, default the head Fyr reads now>, sensitive?: <why, in a few words>}}: ask right after you push; Fyr shows the ask to the owner when CI is green on that head, closes it and tells you when CI fails or GitHub cannot merge, and merges the head the owner saw; never run gh pr merge. Give sensitive (for example \"promotes to production\") when the merge needs the owner's deliberate press, such as a production promotion, a migration or a release: Fyr then never merges it by the repo's auto-merge setting and shows the reason in red; a PR with the GitHub label \"sensitive\" counts the same. ask \"fyr.deploy\" {args: {} or {dir: <Fyr checkout>, tests?: true}}, only from a thread on Fyr's own repo, no change needed: Fyr updates itself when CI publishes a green commit on main, so do not ask after a merge; args {} installs the newest CI release now; dir builds that checkout and switches to it to try it, which waits for the owner's press."
	ChangeListDesc    = "Fyr's facts, one line per change, open changes only by default, at most 4,000 characters. id gives one change of this thread in full."
	PRCreateDesc      = "Open a GitHub pull request for a pushed branch in this thread's repo or one of its change worktrees, and link it to the change card immediately. Preferred over shelling out to gh pr create (never blocked by CLI permission classifiers and needs no temp files). Call change_request {change, ask: \"merge\"} right after when the PR is ready to merge."
)

func (e *Engine) changeTools(s *mcp.Server) {
	e.offerTool(s)      // V0.60
	e.runRequestTool(s) // V0.149
	s.Add(mcp.Tool{Name: "change_update",
		Description: ChangeUpdateDesc,
		Schema: obj(map[string]any{
			"change": str(changeRef),
			"phase":  map[string]any{"type": "string", "enum": core.Phases()},
			"label":  str("A word for where it is that Fyr cannot read, for example \"waits for the API PR\"."),
			"conditions": map[string]any{"type": "array", "items": obj(map[string]any{
				"type":   str("What the fact is about: tests, lint, a local check, …"),
				"status": map[string]any{"type": "string", "enum": core.CondStatuses()},
				"reason": str("One line, mainly for fail and pending."),
			}, "type", "status")},
			"revision": revisionSchema(),
			"evidence": evidenceSchema(),
		}, "change"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			out, err := e.changeInput(th, core.KindChangeUpdate, core.ChangeUpdateArgs{Change: sArg(a, "change"), Update: updateArg(a), Revision: revisionArg(a["revision"]), Evidence: evidenceArg(a)})
			if err != nil {
				return "", err
			}
			return updateReply(out), nil
		}})
	s.Add(mcp.Tool{Name: "change_open",
		Description: ChangeOpenDesc,
		Schema: obj(map[string]any{
			"key":       str("A stable name, unique in this thread, for example \"api:retry-backoff\". The same key returns the same change. With pr, the PR names it."),
			"kind":      map[string]any{"type": "string", "enum": core.OpenKinds(), "description": "code, the default: Fyr tracks no other changes."},
			"title":     str("One line for the owner: what changes."),
			"targets":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Where it lands, as kind:name, for example \"repo:github.com/o/r\"."},
			"branch":    str("The git branch. A branch or a pr is required."),
			"pr":        str("A GitHub PR URL; Fyr watches its checks and reviews."),
			"group":     str("A name shared by changes that belong together, for example one feature across several repos."),
			"after":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Changes this one builds on (a stack), by id or key. With workspace, the new branch starts from the first one's branch."},
			"workspace": map[string]any{"type": "boolean", "description": "true: Fyr makes a git worktree of repo on branch for this change and returns its path. Work there, and give that path to a subagent that works on the change."},
			"repo":      str("With workspace: the folder of the repo, absolute or relative to the thread's folder. Default: the thread's folder."),
			"revision":  revisionSchema(),
		}, "title"),
		Call: func(ctx context.Context, th string, a map[string]any) (string, error) {
			o := core.ChangeOpen{Key: sArg(a, "key"), Kind: core.ChangeKind(sArg(a, "kind")), Title: sArg(a, "title"), Branch: sArg(a, "branch"), Group: sArg(a, "group"),
				Targets: listArg(a, "targets"), After: listArg(a, "after"), Revision: revisionArg(a["revision"])}
			pr := sArg(a, "pr")
			if pr != "" {
				var ok bool
				if pr, ok = core.CanonicalPR(pr); !ok {
					return "", errors.New("pr must be a GitHub pull request URL")
				}
				prRepo, prN, _ := core.ParsePR(pr)
				o.Evidence = append(o.Evidence, core.Evidence{Kind: "pr", URL: pr, Title: fmt.Sprintf("#%d", prN)})
				if prRepo != "" && strings.Count(prRepo, "/") == 1 {
					o.Targets = append(o.Targets, "repo:github.com/"+prRepo)
				}
				if o.Key == "" { // V0.101: the PR names the change
					o.Key = core.PRKey(prRepo, prN)
				}
			}
			if a["workspace"] == true {
				if err := e.openWorkspace(ctx, th, sArg(a, "repo"), &o); err != nil {
					return "", err
				}
			}
			r, err := e.changeInput(th, core.KindChangeOpen, core.ChangeOpenArgs{Open: o, PR: pr})
			if err != nil {
				return "", err
			}
			if pr != "" && e.OnPR != nil {
				e.OnPR(th)
			}
			c, made := r.Change, r.New
			verb := "Opened"
			if !made {
				verb = "Found"
			}
			out := fmt.Sprintf("%s change %s %q · %s. Fyr shows it in the thread's Changes tab. Report what only you know with change_update, and ask the owner with change_request.", verb, c.ID, c.Title, phaseText(c))
			if a["workspace"] == true && c.Workspace != nil {
				out += " " + workspaceText(c.Workspace)
			}
			return out, nil
		}})

	s.Add(mcp.Tool{Name: "change_request",
		Description: ChangeRequestDesc,
		Schema: obj(map[string]any{
			"change": str(changeRef + " Not needed for fyr.deploy."),
			"ask":    map[string]any{"type": "string", "enum": core.Names(core.ChangeAsks())},
			"args":   map[string]any{"type": "object"},
			"reason": str("One sentence for the owner: why now."),
		}, "ask"),
		Call: func(ctx context.Context, th string, a map[string]any) (string, error) {
			ask := core.ChangeAsk(sArg(a, "ask"))
			args, _ := a["args"].(map[string]any)
			if args == nil {
				args = map[string]any{}
			}
			if err := canonicalArgs(args); err != nil {
				return "", err
			}
			if ask == core.AskMerge {
				e.readPRForMergeAsk(ctx, th)
			}
			out, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindChangeRequest, Thread: th,
				Args: core.ChangeRequest{ID: NewID("d_"), Change: sArg(a, "change"), Ask: ask, Args: args, Reason: sArg(a, "reason"),
					FyrRepo: ask == core.AskFyrDeploy && e.onFyrRepo(th)}})
			if err != nil {
				return "", err
			}
			r := out.(core.ChangeRequestReply)
			if s := e.heldReply(th, r.ID); s != "" {
				return s, nil
			}
			what := "deploy Fyr"
			if r.Change != nil {
				what = fmt.Sprintf("%s %q", ask, r.Change.Title)
			}
			if r.Again {
				return fmt.Sprintf("Already asked: the owner has your ask to %s. The answer arrives in a later message starting with [Fyr]; do not ask again or do it another way.", what), nil
			}
			return fmt.Sprintf("Asked the owner to %s. The answer arrives in a later message starting with [Fyr]. Continue with other work or end your turn; do not do it another way meanwhile.", what), nil
		}})

	s.Add(mcp.Tool{Name: "pr_create",
		Description: PRCreateDesc,
		Schema: obj(map[string]any{
			"title":     str("Pull request title."),
			"body":      str("Pull request description (Markdown)."),
			"change":    str("Optional: the change id or key to link this PR to; defaults dir to its worktree and head to its branch."),
			"dir":       str("Optional: repo or worktree directory (absolute or relative to the thread's folder). Defaults to the change's worktree or the thread's folder."),
			"head":      str("Optional: pushed branch name. Defaults to the change's branch or the checkout's current branch."),
			"base":      str("Optional: target base branch. Defaults to the repo's default branch."),
			"draft":     map[string]any{"type": "boolean", "description": "Open as a draft pull request."},
			"labels":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional labels to apply."},
			"reviewers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional GitHub logins to request review from."},
		}, "title", "body"),
		Call: func(ctx context.Context, th string, a map[string]any) (string, error) {
			if e.CreatePR == nil {
				return "", errors.New("this host cannot open GitHub pull requests")
			}
			t, ok := e.Thread(th)
			if !ok {
				return "", errors.New("no such thread")
			}
			var ch *core.Change
			if ref := sArg(a, "change"); ref != "" {
				ch = core.FindChange(&t, ref)
				if ch == nil {
					return "", noChange(map[string]any{"change": ref})
				}
			}
			rawDir := sArg(a, "dir")
			if rawDir == "" && ch != nil && ch.Workspace != nil {
				rawDir = ch.Workspace.Path
			}
			dir, err := allowedPRDir(t, rawDir)
			if err != nil {
				return "", err
			}
			head := sArg(a, "head")
			if head == "" && ch != nil {
				head = ch.Branch
			}
			// If no change was named explicitly, match a change whose worktree is dir.
			if ch == nil {
				for i := range t.Changes {
					if ws := t.Changes[i].Workspace; ws != nil && filepath.Clean(ws.Path) == dir {
						ch = &t.Changes[i]
						if head == "" {
							head = ch.Branch
						}
						break
					}
				}
			}
			draft, _ := a["draft"].(bool)
			req := github.CreatePRReq{
				Dir:       dir,
				Title:     sArg(a, "title"),
				Body:      sArg(a, "body"),
				Head:      head,
				Base:      sArg(a, "base"),
				Draft:     draft,
				Labels:    listArg(a, "labels"),
				Reviewers: listArg(a, "reviewers"),
			}
			url, err := e.CreatePR(ctx, req)
			if err != nil {
				return "", err
			}
			prRepo, prN, _ := core.ParsePR(url)
			o := core.ChangeOpen{
				Kind:   core.KindCode,
				Title:  req.Title,
				Branch: head,
				Evidence: []core.Evidence{
					{Kind: "pr", URL: url, Title: fmt.Sprintf("#%d", prN)},
				},
			}
			if prRepo != "" && strings.Count(prRepo, "/") == 1 {
				o.Targets = append(o.Targets, "repo:github.com/"+prRepo)
			}
			if ch != nil {
				o.Key = ch.Key
			} else {
				o.Key = core.PRKey(prRepo, prN)
			}
			r, err := e.changeInput(th, core.KindChangeOpen, core.ChangeOpenArgs{Open: o, PR: url})
			if err != nil {
				return "", fmt.Errorf("PR opened at %s, but linking failed: %w", url, err)
			}
			if e.OnPR != nil {
				e.OnPR(th)
			}
			return fmt.Sprintf("Opened pull request %s (change %s %q). Fyr is watching its checks; call change_request {change: %q, ask: \"merge\"} now if it is ready to merge.", url, r.Change.ID, r.Change.Title, r.Change.ID), nil
		}})

	s.Add(mcp.Tool{Name: "change_list",
		Description: ChangeListDesc,
		Schema: obj(map[string]any{
			"scope": map[string]any{"type": "string", "enum": []string{"thread", "project"}},
			"id":    str("One change, by id or key (this thread only), in full."),
			"done":  map[string]any{"type": "boolean", "description": "Also list done changes: landed, live, abandoned or reverted."},
		}),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			t, ok := e.Shown(th)
			if !ok {
				return "", errors.New("no such thread")
			}
			type row struct {
				Thread string `json:"thread,omitempty"`
				core.Change
				// V0.84: the owner parked it (Not now).
				Note string `json:"note,omitempty"`
			}
			if id := sArg(a, "id"); id != "" {
				c := core.FindChange(&t, id)
				if c == nil {
					return "", noChange(map[string]any{"change": id})
				}
				rows := []row{{Change: *c, Note: core.ParkedText(c)}}
				b, _ := json.MarshalIndent(rows, "", " ")
				return string(b), nil
			}
			var rows []row
			if sArg(a, "scope") == "project" && t.ProjectID != "" {
				seen := map[string]bool{}
				for _, d := range e.Snapshot() {
					if d.ProjectID != t.ProjectID || d.ArchivedAt != nil {
						continue
					}
					for _, c := range d.Changes {
						if c.Threads != nil { // a host record: once (#35)
							if seen[c.ID] {
								continue
							}
							seen[c.ID] = true
						}
						r := row{Change: c}
						if d.ID != th {
							r.Thread = d.ID + " " + d.Title
						}
						rows = append(rows, r)
					}
				}
			} else {
				for _, c := range t.Changes {
					rows = append(rows, row{Change: c})
				}
			}
			if len(rows) == 0 {
				return "No changes yet.", nil
			}
			// SPEC V0.96 (2): open changes, one line each, within 4,000
			// characters.
			all, _ := a["done"].(bool)
			var lines []string
			hidden := 0
			for i := range rows {
				c := &rows[i].Change
				if !all && !core.ListedOpen(c) {
					hidden++
					continue
				}
				l := core.ChangeLine(c)
				if rows[i].Thread != "" {
					l += " · thread " + core.TruncHead(rows[i].Thread, 60)
				}
				lines = append(lines, l)
			}
			head := fmt.Sprintf("%d open", len(lines))
			if all {
				head = fmt.Sprintf("%d changes", len(lines))
			} else if hidden > 0 {
				head += fmt.Sprintf("; %d done not shown (done: true lists them)", hidden)
			}
			return core.CapLines(head+".", lines), nil
		}})
}

// changeInput sends a change tool's Input and returns the change it
// touched.
func (e *Engine) changeInput(th, kind string, args any) (core.ChangeReply, error) {
	out, err := e.Apply(core.Input{Source: core.FromAgent, Kind: kind, Thread: th, Args: args})
	if err != nil {
		return core.ChangeReply{}, err
	}
	return out.(core.ChangeReply), nil
}

func phaseText(c core.Change) string {
	s := string(c.Phase)
	if c.PhaseBy == core.ByAgent && c.Phase != core.PhaseWorking {
		s += " (reported)"
	}
	if c.Label != "" {
		s += ", " + c.Label
	}
	for _, k := range c.Conditions {
		if k.Status == core.CondFail {
			s += " · " + string(k.Type) + " failed"
			if k.Reason != "" {
				s += ": " + k.Reason
			}
		}
	}
	return s
}

func noChange(a map[string]any) error { return core.NoChange(sArg(a, "change")) }

func sArg(a map[string]any, k string) string {
	v, _ := a[k].(string)
	return strings.TrimSpace(v)
}

func listArg(a map[string]any, k string) []string { return listOf[string](a, k) }

// listOf reads a list of strings into a typed list.
func listOf[T ~string](a map[string]any, k string) []T {
	raw, _ := a[k].([]any)
	var out []T
	for _, x := range raw {
		if s, ok := x.(string); ok {
			out = append(out, T(s))
		}
	}
	return out
}

func revisionSchema() map[string]any {
	return obj(map[string]any{
		"id":      str("A commit SHA."),
		"summary": str("One line: what this version changes."),
		"url":     str("A link to this version."),
	}, "id")
}

func evidenceSchema() map[string]any {
	return map[string]any{"type": "array", "items": obj(map[string]any{
		"kind":       str("log, preview, pr, other"),
		"url":        str("A link."),
		"attachment": str("A Fyr attachment id (from show)."),
		"title":      str("A short label."),
	}, "kind")}
}

func evidenceArg(a map[string]any) []core.Evidence {
	var evs []core.Evidence
	raw, _ := a["evidence"].([]any)
	for _, x := range raw {
		m, _ := x.(map[string]any)
		evs = append(evs, core.Evidence{Kind: sArg(m, "kind"), URL: sArg(m, "url"), Attachment: sArg(m, "attachment"), Title: sArg(m, "title")})
	}
	return evs
}

// updateArg reads the report of change_update.
func updateArg(a map[string]any) core.ChangeUpdate {
	u := core.ChangeUpdate{Phase: core.Phase(sArg(a, "phase"))}
	if v, ok := a["label"].(string); ok {
		u.Label = &v
	}
	if raw, ok := a["conditions"].([]any); ok {
		for _, x := range raw {
			m, _ := x.(map[string]any)
			u.Conditions = append(u.Conditions, core.Condition{Type: core.CondType(sArg(m, "type")), Status: core.CondStatus(sArg(m, "status")), Reason: sArg(m, "reason")})
		}
	}
	return u
}

// updateReply is the change and what Fyr reads instead of an observed
// value the agent sent (V0.101).
func updateReply(r core.ChangeReply) string {
	out := fmt.Sprintf("%q · %s.", r.Change.Title, phaseText(r.Change))
	if len(r.Notes) > 0 {
		out += " " + strings.Join(r.Notes, " ")
	}
	return out
}

func revisionArg(v any) *core.Revision {
	m, _ := v.(map[string]any)
	if m == nil || sArg(m, "id") == "" {
		return nil
	}
	return &core.Revision{ID: sArg(m, "id"), Summary: sArg(m, "summary"), URL: sArg(m, "url"), By: core.ByAgent}
}

// onFyrRepo reports that the thread works on Fyr's own repo
// (V0.one-tool-vocabulary): its folder, or its project's, is a Fyr checkout.
// Only such a thread may ask to deploy Fyr.
func (e *Engine) onFyrRepo(th string) bool {
	t, ok := e.Thread(th)
	if !ok {
		return false
	}
	if t.Cwd != "" && isFyrCheckout(t.Cwd) {
		return true
	}
	e.mu.Lock()
	p := e.projects[t.ProjectID]
	e.mu.Unlock()
	return p != nil && p.Folder != "" && isFyrCheckout(p.Folder)
}

// heldReply is the tool's answer when the merge ask id waits for CI
// (V0.136), or "".
func (e *Engine) heldReply(th, id string) string {
	t, ok := e.Thread(th)
	if !ok {
		return ""
	}
	for _, d := range t.HeldAsks {
		if d.ID != id {
			continue
		}
		head, _ := d.Effect.Args["head"].(string)
		if len(head) > 7 {
			head = head[:7]
		}
		return fmt.Sprintf("Recorded. Fyr shows it to the owner when CI is green on %s, and tells you in a [Fyr] message if CI fails or GitHub cannot merge it. Do not ask again or merge another way; continue with other work or end your turn.", head)
	}
	return ""
}

// openWorkspace makes the worktree of change_open {workspace: true}
// (V0.change-worktree, decision 0019) and puts it on o. The branch is
// required; the key defaults to it. A change of the thread with this key
// that already has a worktree keeps it. With after, a new branch starts from
// the branch of the first change it builds on.
func (e *Engine) openWorkspace(ctx context.Context, th, repo string, o *core.ChangeOpen) error {
	if o.Branch == "" {
		return errors.New("workspace needs the branch to make, for example \"feat/retry\"")
	}
	if o.Kind != "" && o.Kind != core.KindCode {
		return errors.New("workspace is for a code change")
	}
	if o.Key == "" {
		o.Key = o.Branch
	}
	t, ok := e.Thread(th)
	if !ok {
		return core.NoChange(o.Key)
	}
	if c := core.FindChange(&t, o.Key); c != nil && c.Workspace != nil && c.Workspace.Path != "" {
		if _, err := os.Stat(c.Workspace.Path); err == nil {
			return nil
		}
	}
	dir := t.Cwd
	if repo != "" {
		dir = repo
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(t.Cwd, repo)
		}
	}
	from := ""
	if len(o.After) > 0 {
		b := core.FindChange(&t, o.After[0])
		if b == nil {
			return core.NoChange(o.After[0])
		}
		if b.Branch == "" {
			return fmt.Errorf("change %s has no branch to build on", b.ID)
		}
		from = b.Branch
	}
	ws, err := agent.MakeWorktree(ctx, agent.WorktreeReq{Dir: dir, Branch: o.Branch, From: from})
	if err != nil {
		return err
	}
	o.Workspace = &ws
	if ws.Repo != "" {
		o.Targets = append(o.Targets, "repo:"+ws.Repo)
	}
	return nil
}

// workspaceText tells the agent where to work.
func workspaceText(w *core.Workspace) string {
	if !w.MadeByFyr {
		return fmt.Sprintf("The branch %s is already checked out at %s: work there.", w.Branch, w.Path)
	}
	from := ""
	if w.Base != "" {
		from = " from " + w.Base
	}
	return fmt.Sprintf("Fyr made its worktree at %s on branch %s%s: work there (cd into it), and give that path to a subagent that works on this change. Fyr removes it only once nothing in it is uncommitted or unpushed.", w.Path, w.Branch, from)
}

// allowedPRDir resolves rawDir for pr_create and ensures it stays inside the
// thread's working directory or one of its tracked change worktrees
// (V0.pr-create-tool).
func allowedPRDir(t core.Thread, rawDir string) (string, error) {
	cwd := filepath.Clean(t.Cwd)
	if cwd == "" || cwd == "." {
		return "", errors.New("thread has no working directory")
	}
	dir := cwd
	if strings.TrimSpace(rawDir) != "" {
		dir = rawDir
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cwd, dir)
		}
		dir = filepath.Clean(dir)
	}
	within := func(root, target string) bool {
		if root == "" {
			return false
		}
		root = filepath.Clean(root)
		rel, err := filepath.Rel(root, target)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	ok := within(cwd, dir)
	if !ok {
		for _, c := range t.Changes {
			if c.Workspace != nil && within(c.Workspace.Path, dir) {
				ok = true
				break
			}
		}
	}
	if !ok {
		return "", errors.New("dir must be inside the thread's folder or one of its change worktrees")
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("dir %q is not an existing directory", dir)
	}
	return dir, nil
}
