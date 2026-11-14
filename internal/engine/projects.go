package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

// ProjectDTO is a project as clients see it (SPEC 0.5 V0.23).
type ProjectDTO struct {
	core.Project
	Threads  int       `json:"threads"`
	LastUsed time.Time `json:"lastUsed"`
	// Repos are the normalised origin remotes of the project's folders on
	// this host and its repo sources, deduplicated (V0.42); read-only.
	Repos []string `json:"repos"`
	// QuietUntil is the end of the quiet hours the project is in (V0.46).
	QuietUntil *time.Time `json:"quietUntil,omitempty"`
}

// quietUntil is the end of the quiet hours project id is in at now, in the
// host's time zone, or nil (SPEC 0.5 V0.46). Call with e.mu held.
func (e *Engine) quietUntil(id string, now time.Time) *time.Time {
	p := e.projects[id]
	if p == nil || len(p.Quiet) == 0 {
		return nil
	}
	if u, ok := core.QuietUntil(p.Quiet, now.In(time.Local)); ok {
		return &u
	}
	return nil
}

// runGit runs git in dir with a short timeout, for the clone provider.
func runGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// scratchDir is where Fyr makes scratch folders (V0.42).
func (e *Engine) scratchDir() string {
	if e.ScratchDir != "" {
		return e.ScratchDir
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share", "fyr", "scratch")
}

// scratchRoots are the folders whose children are throwaway places (SPEC 0.5
// V0.28): ScratchRoots when set, else T3's scratch root; Fyr's own scratch
// folder is always one (V0.42).
func (e *Engine) scratchRoots() []string {
	roots := e.ScratchRoots
	if roots == nil {
		if home, _ := os.UserHomeDir(); home != "" {
			roots = []string{filepath.Join(home, ".t3", "scratch")}
		}
	}
	if d := e.scratchDir(); d != "" {
		for _, r := range roots {
			if filepath.Clean(r) == d {
				return roots
			}
		}
		roots = append(append([]string{}, roots...), d)
	}
	return roots
}

// scratchSlug is a folder-name slug from the first words of text (V0.42).
func scratchSlug(text string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(text) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if !ok {
			if b.Len() > 0 {
				dash = true
			}
			continue
		}
		if dash {
			if b.Len() >= 34 {
				break
			}
			b.WriteByte('-')
			dash = false
		}
		b.WriteRune(r)
		if b.Len() >= 40 {
			break
		}
	}
	if b.Len() == 0 {
		return "thread"
	}
	return strings.TrimSuffix(b.String(), "-")
}

// makeScratch makes a fresh scratch folder for a thread (V0.42).
func (e *Engine) makeScratch(text string, now time.Time) (string, error) {
	root := e.scratchDir()
	if root == "" {
		return "", fmt.Errorf("no home folder for a scratch folder")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	base := filepath.Join(root, now.Local().Format("20060102-150405")+"-"+scratchSlug(text))
	dir := base
	for i := 2; ; i++ {
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			return dir, nil
		}
		if !os.IsExist(err) || i > 99 {
			return "", err
		}
		dir = fmt.Sprintf("%s-%d", base, i)
	}
}

// RecentFolders are the thread folders on this host, newest first (V0.42).
func (e *Engine) RecentFolders() []string {
	e.mu.Lock()
	ts := make([]*core.Thread, 0, len(e.threads))
	for _, t := range e.threads {
		if t.Cwd != "" {
			ts = append(ts, t)
		}
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].UpdatedAt.After(ts[j].UpdatedAt) })
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Cwd)
	}
	e.mu.Unlock()
	return out
}

// ProjectFolders are the folders of every project on this host (V0.42).
func (e *Engine) ProjectFolders() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, p := range e.projects {
		for _, s := range p.Sources {
			if s.Kind == "folder" && s.Host == e.HostID {
				out = append(out, s.Path)
			}
		}
	}
	return out
}

// RefreshRepos reads the origin remote of every project folder on this host
// (V0.42) and reports whether a project's repos changed. Git runs outside
// the lock.
func (e *Engine) RefreshRepos() bool {
	dirs := e.ProjectFolders()
	got := map[string]string{}
	for _, d := range dirs {
		if _, ok := got[d]; ok {
			continue
		}
		remote, err := runGit(d, "remote", "get-url", "origin")
		if err != nil {
			remote = ""
		}
		got[d] = core.NormalizeRemote(remote)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	changed := len(got) != len(e.repoOf)
	for d, r := range got {
		if old, ok := e.repoOf[d]; !ok || old != r {
			changed = true
		}
	}
	e.repoOf = got
	return changed
}

// repos is a project's derived and hand-made repos, deduplicated. Call with
// e.mu held.
func (e *Engine) repos(p *core.Project) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(r string) {
		if r != "" && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	for _, s := range p.Sources {
		switch {
		case s.Kind == "folder" && s.Host == e.HostID:
			add(e.repoOf[s.Path])
		case s.Kind == "repo":
			add(core.NormalizeRemote(s.URL))
		}
	}
	return out
}

// providers identify a folder, in order (SPEC 0.5 V0.28).
func (e *Engine) providers() []core.Provider {
	return []core.Provider{core.ScratchProvider{Roots: e.scratchRoots()}, core.CloneProvider{Git: runGit}, core.FolderProvider{}}
}

// projectList is the projects for matching: a folder's derived repo counts
// as a repo source (V0.42).
func (e *Engine) projectList() []core.Project {
	out := make([]core.Project, 0, len(e.projects))
	for _, p := range e.projects {
		cp := *p
		for _, s := range p.Sources {
			if s.Kind == "folder" && s.Host == e.HostID && e.repoOf[s.Path] != "" {
				cp.Sources = append(append([]core.Source{}, cp.Sources...), core.Source{Kind: "repo", URL: e.repoOf[s.Path]})
			}
		}
		out = append(out, cp)
	}
	return out
}

// workspaceOf finds or identifies the workspace of a folder on this host.
// Call with e.mu held; it returns the workspace and whether it is new.
func (e *Engine) workspaceOf(cwd string) (*core.Workspace, bool) {
	dir := filepath.Clean(cwd)
	for _, w := range e.workspaces {
		if w.Host == e.HostID && w.Path == dir {
			return w, false
		}
	}
	w := core.Identify(e.providers(), e.HostID, dir)
	w.ID = NewID("w_")
	e.workspaces[w.ID] = &w
	return &w, true
}

// assignProject gives t its workspace and, when the workspace is new to the
// thread and it has no project, the project that matching finds; it never
// makes a project (SPEC 0.5 V0.28). Call with e.mu held; it returns a new
// workspace to persist, or nil.
func (e *Engine) assignProject(t *core.Thread, now time.Time) *core.Workspace {
	if t.Cwd == "" {
		return nil
	}
	var made *core.Workspace
	ws := e.workspaces[t.WorkspaceID]
	fresh := ws == nil
	if fresh {
		w, isNew := e.workspaceOf(t.Cwd)
		ws, t.WorkspaceID = w, w.ID
		if isNew {
			made = w
		}
	}
	if t.ProjectID != "" && e.projects[t.ProjectID] == nil {
		t.ProjectID, t.ProjectManual = "", false
	}
	if t.ProjectID == "" && fresh {
		t.ProjectID = core.MatchProject(e.projectList(), ws.Refs)
	}
	t.Project = ""
	if p := e.projects[t.ProjectID]; p != nil {
		t.Project = p.Name
	}
	return made
}

// suggestion is the project a thread without one could get (V0.28).
func (e *Engine) suggestion(t *core.Thread) *core.Suggestion {
	if t.ProjectID != "" && e.projects[t.ProjectID] != nil {
		return nil
	}
	if w := e.workspaces[t.WorkspaceID]; w != nil {
		return core.Suggest(*w)
	}
	return nil
}

// draft is what "New project from this thread" starts with (V0.30), without
// the sources an existing project already has. Call with e.mu held.
func (e *Engine) draft(t *core.Thread) *core.Suggestion {
	w := e.workspaces[t.WorkspaceID]
	if w == nil {
		return nil
	}
	home, _ := os.UserHomeDir()
	return core.Draft(*w, home, func(s core.Source) bool {
		ref := s.Ref()
		for _, p := range e.projects {
			for _, ps := range p.Sources {
				if ps.Ref() == ref {
					return true
				}
			}
		}
		return false
	})
}

// migrateAuto ends auto projects (SPEC 0.5 V0.28): one that holds threads
// becomes a normal project; an empty one that was never edited goes.
func (e *Engine) migrateAuto() error {
	used := map[string]bool{}
	for _, t := range e.threads {
		used[t.ProjectID] = true
	}
	var keep []*core.Project
	var gone []string
	for id, p := range e.projects {
		if !p.Auto {
			continue
		}
		if !used[id] && !p.Edited {
			gone = append(gone, id)
			delete(e.projects, id)
			continue
		}
		p.Auto = false
		keep = append(keep, p)
	}
	if len(keep)+len(gone) == 0 {
		return nil
	}
	_, err := e.st.Write(func(tx *store.Tx) error {
		for _, p := range keep {
			if err := tx.PutProject(p); err != nil {
				return err
			}
		}
		for _, id := range gone {
			if err := tx.DeleteProject(id); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// Projects lists projects with their thread counts.
func (e *Engine) Projects() []ProjectDTO {
	e.mu.Lock()
	defer e.mu.Unlock()
	counts := map[string]int{}
	last := map[string]time.Time{}
	for _, t := range e.threads {
		if t.ArchivedAt == nil {
			counts[t.ProjectID]++
		}
		if t.UpdatedAt.After(last[t.ProjectID]) {
			last[t.ProjectID] = t.UpdatedAt
		}
	}
	out := make([]ProjectDTO, 0, len(e.projects))
	now := e.Now()
	for _, p := range e.projects {
		out = append(out, ProjectDTO{Project: *p, Threads: counts[p.ID], LastUsed: last[p.ID], Repos: e.repos(p), QuietUntil: e.quietUntil(p.ID, now)})
	}
	return out
}

// SeedProjects adds projects from another source (T3) by their folder on
// this host; it never overwrites a project the user edited.
func (e *Engine) SeedProjects(seeds []core.Project) {
	e.mu.Lock()
	now := e.Now()
	var changed []*core.Project
	for _, sd := range seeds {
		folder := sd.Folder(e.HostID)
		if folder == "" {
			continue
		}
		var hit *core.Project
		for _, p := range e.projects {
			if p.Folder(e.HostID) == filepath.Clean(folder) {
				hit = p
			}
		}
		if hit != nil {
			if hit.Edited {
				continue
			}
			if sd.Name != "" {
				hit.Name = sd.Name
			}
			if sd.Icon.Kind != "" {
				hit.Icon = sd.Icon
			}
			hit.Seeded, hit.Auto = true, false
			changed = append(changed, hit)
			continue
		}
		p := sd
		p.ID = NewID("p_")
		p.Seeded, p.Auto = true, false
		p.CreatedAt = now
		for i := range p.Sources {
			if p.Sources[i].Kind == "folder" {
				p.Sources[i].Path = filepath.Clean(p.Sources[i].Path)
			}
		}
		if p.Name == "" {
			p.Name = filepath.Base(folder)
		}
		if !core.ValidColor(p.Color) {
			p.Color = core.DefaultColor(p.Name)
		}
		if p.Icon.Kind == "" {
			p.Icon = core.Icon{Kind: "monogram"}
		}
		e.projects[p.ID] = &p
		changed = append(changed, &p)
	}
	e.mu.Unlock()
	for _, p := range changed {
		e.saveProject(p)
	}
}

func (e *Engine) saveProject(p *core.Project) {
	if _, err := e.st.Write(func(tx *store.Tx) error { return tx.PutProject(p) }); err != nil {
		return
	}
	for _, d := range e.Projects() {
		if d.ID == p.ID {
			e.Hub.Project(d)
		}
	}
}

// checkSource cleans a source and refuses one that cannot match: a folder
// must be an absolute path, and on this host it must exist (SPEC 0.5 V0.23).
func (e *Engine) checkSource(s *core.Source) error {
	switch s.Kind {
	case "folder":
		if s.Host == "" {
			s.Host = e.HostID
		}
		p := strings.TrimSpace(s.Path)
		if p == "~" || strings.HasPrefix(p, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				p = home + p[1:]
			}
		}
		if !filepath.IsAbs(p) {
			return fmt.Errorf("the folder must be a full path, like /home/you/src/app")
		}
		s.Path = filepath.Clean(p)
		if s.Host == e.HostID {
			if fi, err := os.Stat(s.Path); err != nil || !fi.IsDir() {
				return fmt.Errorf("there is no folder %s on %s", s.Path, s.Host)
			}
		}
	case "repo":
		s.URL = core.NormalizeRemote(s.URL)
		if s.URL == "" {
			return fmt.Errorf("the repo needs a URL")
		}
	default:
		return fmt.Errorf("source kind must be folder or repo")
	}
	return nil
}

// projectCommand handles project.update, project.create, project.createFrom,
// project.addSource and project.removeSource.
func (e *Engine) projectCommand(c Cmd) Reply {
	if prev, _ := e.st.Receipt(c.CommandID); prev != nil {
		var r Reply
		json.Unmarshal(prev, &r)
		r.Status = 200
		return r
	}
	bad := func(msg string) Reply { return Reply{Error: msg, Status: 400} }
	e.mu.Lock()
	var p *core.Project
	switch c.Action {
	case "project.create":
		name := c.Args.Str("name")
		p = &core.Project{ID: NewID("p_"), Name: name, Icon: core.Icon{Kind: "monogram"}, CreatedAt: e.Now(), Edited: true}
		raw, _ := json.Marshal(c.Args["sources"])
		json.Unmarshal(raw, &p.Sources)
		for i := range p.Sources {
			if err := e.checkSource(&p.Sources[i]); err != nil {
				e.mu.Unlock()
				return bad(err.Error())
			}
		}
		if p.Name == "" && len(p.Sources) > 0 {
			p.Name = filepath.Base(p.Sources[0].Path + p.Sources[0].URL)
		}
		if strings.TrimSpace(p.Name) == "" {
			e.mu.Unlock()
			return bad("a project needs a name or a source")
		}
		p.Color = core.DefaultColor(p.Name)
		// V0.30: file the thread it was made from, by hand, in the same write.
		if tid := c.Args.Str("threadId"); tid != "" {
			t := e.threads[tid]
			if t == nil {
				e.mu.Unlock()
				return Reply{Error: "no thread " + tid, Status: 404}
			}
			t.ProjectID, t.ProjectManual, t.Project = p.ID, true, ""
		}
		e.projects[p.ID] = p
	case "project.createFrom":
		// SPEC 0.5 V0.28: the thread's suggestion becomes a project, and
		// every thread without a project that it matches is filed.
		tid := c.Args.Str("threadId")
		if tid == "" {
			tid = c.ThreadID
		}
		t := e.threads[tid]
		if t == nil {
			e.mu.Unlock()
			return Reply{Error: "no thread " + tid, Status: 404}
		}
		sug := e.suggestion(t)
		if sug == nil {
			e.mu.Unlock()
			return bad("this thread has no project to make: it has a project, or its folder has no git remote")
		}
		p = &core.Project{ID: NewID("p_"), Name: sug.Name, Color: core.DefaultColor(sug.Name), Icon: core.Icon{Kind: "monogram"},
			Sources: sug.Sources, CreatedAt: e.Now(), Edited: true}
		e.projects[p.ID] = p
		list := e.projectList()
		for _, o := range e.threads {
			if o.ProjectManual || (o.ProjectID != "" && e.projects[o.ProjectID] != nil) {
				continue
			}
			if w := e.workspaces[o.WorkspaceID]; w != nil && core.MatchProject(list, w.Refs) == p.ID {
				o.ProjectID = p.ID
			}
		}
	case "project.update", "project.addSource", "project.removeSource", "project.setMain":
		cur := e.projects[c.Args.Str("id")]
		if cur == nil {
			e.mu.Unlock()
			return Reply{Error: "no project " + c.Args.Str("id"), Status: 404}
		}
		cp := *cur
		p = &cp
		if c.Action == "project.addSource" {
			var s core.Source
			raw, _ := json.Marshal(c.Args["source"])
			json.Unmarshal(raw, &s)
			if err := e.checkSource(&s); err != nil {
				e.mu.Unlock()
				return bad(err.Error())
			}
			p.Sources = append(append([]core.Source{}, p.Sources...), s)
		} else if c.Action == "project.removeSource" {
			i, ok := c.Args["index"].(float64)
			if n, isInt := c.Args["index"].(int); isInt {
				i, ok = float64(n), true
			}
			if !ok || i != float64(int(i)) || int(i) < 0 || int(i) >= len(p.Sources) {
				e.mu.Unlock()
				return bad(fmt.Sprintf("index must be 0 to %d", len(p.Sources)-1))
			}
			p.Sources = append(append([]core.Source{}, p.Sources[:int(i)]...), p.Sources[int(i)+1:]...)
		} else if c.Action == "project.setMain" {
			// V0.42: new threads in the project start in this folder.
			i, ok := c.Args["index"].(float64)
			if n, isInt := c.Args["index"].(int); isInt {
				i, ok = float64(n), true
			}
			if !ok || i != float64(int(i)) || !p.SetMain(int(i)) {
				e.mu.Unlock()
				return bad("index must point at one of the project's folders")
			}
		} else {
			if n := strings.TrimSpace(c.Args.Str("name")); n != "" {
				p.Name = n
			}
			if col := c.Args.Str("color"); col != "" {
				if !core.ValidColor(col) {
					e.mu.Unlock()
					return bad(fmt.Sprintf("color must be one of %v", core.Palette))
				}
				p.Color = col
			}
			if raw, ok := c.Args["icon"]; ok {
				var ic core.Icon
				b, _ := json.Marshal(raw)
				json.Unmarshal(b, &ic)
				if ic.Kind != "monogram" && ic.Kind != "emoji" && ic.Kind != "line" {
					e.mu.Unlock()
					return bad("icon kind must be monogram, emoji or line")
				}
				p.Icon = ic
			}
			if v, ok := c.Args["profile"].(string); ok {
				p.Profile = v
			}
			if v, ok := c.Args["model"].(string); ok {
				p.Model = v
			}
			// V0.46: quiet hours, a list of windows (decision 0014
			// addendum); null or an empty list clears them. The single
			// object of before is read as a list of one.
			if raw, ok := c.Args["quiet"]; ok {
				var qs core.QuietHours
				if raw != nil {
					b, _ := json.Marshal(raw)
					if err := json.Unmarshal(b, &qs); err != nil {
						e.mu.Unlock()
						return bad("quiet must be a list of {from, to, days?}")
					}
					if err := qs.Validate(); err != nil {
						e.mu.Unlock()
						return bad(err.Error())
					}
				}
				if len(qs) == 0 {
					qs = nil
				}
				p.Quiet = qs
			}
		}
		p.Edited = true
		e.projects[p.ID] = p
	default:
		e.mu.Unlock()
		return bad("unknown action " + c.Action)
	}
	// The name and the quiet hours show on threads: refresh them.
	var touched []DTO
	now := e.Now()
	_, quietChanged := c.Args["quiet"]
	quietChanged = quietChanged && c.Action == "project.update"
	if quietChanged {
		e.quietSent[p.ID] = quietKey(e.quietUntil(p.ID, now))
	}
	for _, t := range e.threads {
		if t.ProjectID == p.ID && (t.Project != p.Name || quietChanged) {
			t.Project = p.Name
			e.bump(t.ID)
			touched = append(touched, e.dto(t, now))
		}
	}
	reply := Reply{OK: true, Status: 200, Result: map[string]any{"id": p.ID}}
	_, err := e.st.Write(func(tx *store.Tx) error {
		if err := tx.PutProject(p); err != nil {
			return err
		}
		for _, d := range touched {
			if err := tx.PutThread(d.Thread); err != nil {
				return err
			}
		}
		return tx.PutReceipt(c.CommandID, reply, now)
	})
	e.mu.Unlock()
	if err != nil {
		return Reply{Error: err.Error(), Status: 500}
	}
	sourcesChanged := c.Action != "project.update" && c.Action != "project.createFrom" && c.Action != "project.setMain"
	if sourcesChanged || c.Action == "project.createFrom" {
		e.RefreshRepos() // V0.42: the folders changed; read their remotes again
	}
	for _, d := range e.Projects() {
		if d.ID == p.ID {
			e.Hub.Project(d)
		}
	}
	for _, d := range touched {
		e.Hub.Thread(d)
	}
	if sourcesChanged {
		e.Rematch() // the sources changed: threads may belong elsewhere now
	}
	return reply
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

// Rematch runs matching for every thread not filed by hand after a change
// to the projects' sources (SPEC 0.5 V0.28): a thread moves to the project
// that matching finds; a thread that nothing matches keeps its project.
// Threads without a workspace get one.
func (e *Engine) Rematch() {
	e.mu.Lock()
	now := e.Now()
	list := e.projectList()
	var made []*core.Workspace
	var moved []*core.Thread
	for _, t := range e.threads {
		if t.Cwd == "" {
			continue
		}
		old, oldWS := t.ProjectID, t.WorkspaceID
		if w := e.assignProject(t, now); w != nil {
			made = append(made, w)
		}
		if !t.ProjectManual {
			if id := core.MatchProject(list, e.workspaces[t.WorkspaceID].Refs); id != "" {
				t.ProjectID, t.Project = id, e.projects[id].Name
			}
		}
		if t.ProjectID != old || t.WorkspaceID != oldWS {
			e.bump(t.ID)
			moved = append(moved, t)
		}
	}
	var dtos []DTO
	for _, t := range moved {
		dtos = append(dtos, e.dto(t, now))
	}
	e.mu.Unlock()
	e.st.Write(func(tx *store.Tx) error {
		for _, w := range made {
			if err := tx.PutWorkspace(w); err != nil {
				return err
			}
		}
		for _, t := range moved {
			if err := tx.PutThread(t); err != nil {
				return err
			}
		}
		return nil
	})
	for _, d := range dtos {
		e.Hub.Thread(d)
	}
}
