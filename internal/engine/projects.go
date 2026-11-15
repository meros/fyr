package engine

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
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
	// Rev is when this copy was built (#42): a client keeps the higher of a
	// snapshot's copy and a stream frame's.
	Rev      int64     `json:"rev"`
	Threads  int       `json:"threads"`
	LastUsed time.Time `json:"lastUsed"`
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

// ProjectFolders are the folders of the projects (V0.42, V0.55).
func (e *Engine) ProjectFolders() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, p := range e.projects {
		if p.Folder != "" {
			out = append(out, p.Folder)
		}
	}
	return out
}

// projectList is the projects for matching. Call with e.mu held.
func (e *Engine) projectList() []*core.Project {
	out := make([]*core.Project, 0, len(e.projects))
	for _, p := range e.projects {
		out = append(out, p)
	}
	return out
}

// assignProject drops a project that is gone and, for a thread not filed by
// hand and without a project, finds the project whose folder covers the
// thread's folder (SPEC 0.5 V0.55). It never makes a project. Call with
// e.mu held.
func (e *Engine) assignProject(t *core.Thread) {
	if t.ProjectID != "" && e.projects[t.ProjectID] == nil {
		t.ProjectID, t.ProjectManual = "", false
	}
	if t.ProjectID == "" && !t.ProjectManual {
		t.ProjectID = core.MatchFolder(e.projectList(), t.Cwd)
	}
	t.Project = ""
	if p := e.projects[t.ProjectID]; p != nil {
		t.Project = p.Name
	}
}

// NewProject is what "New project from this thread" starts with (SPEC 0.5
// V0.30, V0.55): the thread's folder and its name.
type NewProject struct {
	Name   string `json:"name"`
	Folder string `json:"folder"`
}

// draft is the thread's NewProject, or nil for a scratch folder, the home
// folder, / and a folder that a project already has. Call with e.mu held.
func (e *Engine) draft(t *core.Thread) *NewProject {
	if t.Cwd == "" {
		return nil
	}
	dir := filepath.Clean(t.Cwd)
	if home, _ := os.UserHomeDir(); dir == "/" || (home != "" && dir == filepath.Clean(home)) {
		return nil
	}
	for _, r := range e.scratchRoots() {
		if core.Covers(r, dir) {
			return nil
		}
	}
	for _, p := range e.projects {
		if p.Folder != "" && filepath.Clean(p.Folder) == dir {
			return nil
		}
	}
	return &NewProject{Name: filepath.Base(dir), Folder: dir}
}

// migrateFolders gives every project stored with sources (before V0.55) one
// folder: the one marked main on this host, else its first folder on this
// host, else its first folder on any host. The other sources are dropped and
// logged (decision 0017).
func (e *Engine) migrateFolders(old map[string][]store.OldSource) error {
	var changed []*core.Project
	for id, srcs := range old {
		p := e.projects[id]
		if p == nil {
			continue
		}
		keep := -1
		for pass := 0; pass < 3 && keep < 0; pass++ {
			for i, s := range srcs {
				here := s.Host == "" || s.Host == e.HostID
				if s.Kind == "folder" && s.Path != "" && ((pass == 0 && here && s.Main) || (pass == 1 && here) || pass == 2) {
					keep = i
					break
				}
			}
		}
		if p.Folder == "" && keep >= 0 {
			p.Folder = filepath.Clean(srcs[keep].Path)
		}
		for i, s := range srcs {
			switch {
			case i == keep:
			case s.Kind == "folder":
				log.Printf("project %q: dropped folder %s:%s (V0.55: a project has one folder)", p.Name, s.Host, s.Path)
			case s.Kind == "repo":
				log.Printf("project %q: dropped repo %s (V0.55: a project has one folder)", p.Name, s.URL)
			default:
				log.Printf("project %q: dropped %s source (V0.55)", p.Name, s.Kind)
			}
		}
		changed = append(changed, p)
	}
	if len(changed) == 0 {
		return nil
	}
	_, err := e.st.Write(func(tx *store.Tx) error {
		for _, p := range changed {
			if err := tx.PutProject(p); err != nil {
				return err
			}
		}
		return nil
	})
	return err
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

// Projects lists projects with their thread counts, all stamped with one new
// rev (built under e.mu, so a later copy is never older).
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
	rev := e.nextRev()
	for _, p := range e.projects {
		out = append(out, ProjectDTO{Project: *p, Rev: rev, Threads: counts[p.ID], LastUsed: last[p.ID], QuietUntil: e.quietUntil(p.ID, now)})
	}
	return out
}

// checkFolder cleans a project folder and refuses one that cannot match: it
// must be a full path to a folder that exists on this host (SPEC 0.5 V0.23,
// V0.55). "" stays "" (no folder).
func checkFolder(dir string) (string, error) {
	p := strings.TrimSpace(dir)
	if p == "" {
		return "", nil
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = home + p[1:]
		}
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("the folder must be a full path, like /home/you/src/app")
	}
	p = filepath.Clean(p)
	if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("there is no folder %s on this host", p)
	}
	return p, nil
}

// projectCommand handles project.create and project.update (SPEC 0.5
// V0.23, V0.55).
func (e *Engine) projectCommand(c Cmd) Reply {
	if prev, _ := e.st.Receipt(c.CommandID); prev != nil {
		var r Reply
		json.Unmarshal(prev, &r)
		r.Status = 200
		return r
	}
	if c.Action == "project.delete" {
		return e.projectDelete(c)
	}
	bad := func(msg string) Reply { return Reply{Error: msg, Status: 400} }
	_, folderSet := c.Args["folder"]
	folder, err := checkFolder(c.Args.Str("folder"))
	if err != nil {
		return bad(err.Error())
	}
	e.mu.Lock()
	var p *core.Project
	switch c.Action {
	case "project.create":
		name := strings.TrimSpace(c.Args.Str("name"))
		if name == "" && folder != "" {
			name = filepath.Base(folder)
		}
		if name == "" {
			e.mu.Unlock()
			return bad("a project needs a name or a folder")
		}
		p = &core.Project{ID: NewID("p_"), Name: name, Folder: folder, Color: core.DefaultColor(name), Icon: core.Icon{Kind: "monogram"}, CreatedAt: e.Now(), Edited: true}
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
	case "project.update":
		cur := e.projects[c.Args.Str("id")]
		if cur == nil {
			e.mu.Unlock()
			return Reply{Error: "no project " + c.Args.Str("id"), Status: 404}
		}
		cp := *cur
		p = &cp
		if folderSet {
			p.Folder = folder
		}
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
	_, err = e.st.Write(func(tx *store.Tx) error {
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
	for _, d := range e.Projects() {
		if d.ID == p.ID {
			e.Hub.Project(d)
		}
	}
	for _, d := range touched {
		e.Hub.Thread(d)
	}
	if c.Action == "project.create" || folderSet {
		e.Rematch() // V0.55: the folders changed; threads may land here now
	}
	return reply
}

// projectDelete removes a project (SPEC 0.5 V0.58). Its threads lose it and
// the filed-by-hand mark, then match again, so one inside another project's
// folder lands there. The stream gets {id, deleted: true}. Called after the
// receipt check.
func (e *Engine) projectDelete(c Cmd) Reply {
	id := c.Args.Str("id")
	e.mu.Lock()
	if e.projects[id] == nil {
		e.mu.Unlock()
		return Reply{Error: "no project " + id, Status: 404}
	}
	delete(e.projects, id)
	delete(e.quietSent, id)
	now := e.Now()
	var moved []*core.Thread
	gained := map[string]bool{}
	for _, t := range e.threads {
		if t.ProjectID != id {
			continue
		}
		t.ProjectID, t.ProjectManual = "", false
		e.assignProject(t)
		if t.ProjectID != "" {
			gained[t.ProjectID] = true
		}
		e.bump(t.ID)
		moved = append(moved, t)
	}
	dtos := make([]DTO, 0, len(moved))
	for _, t := range moved {
		dtos = append(dtos, e.dto(t, now))
	}
	reply := Reply{OK: true, Status: 200, Result: map[string]any{"id": id}}
	_, err := e.st.Write(func(tx *store.Tx) error {
		if err := tx.DeleteProject(id); err != nil {
			return err
		}
		for _, t := range moved {
			if err := tx.PutThread(t); err != nil {
				return err
			}
		}
		return tx.PutReceipt(c.CommandID, reply, now)
	})
	e.mu.Unlock()
	if err != nil {
		return Reply{Error: err.Error(), Status: 500}
	}
	e.Hub.ProjectDeleted(id)
	for _, d := range dtos {
		e.Hub.Thread(d)
	}
	// The projects that took threads have new counts.
	for _, d := range e.Projects() {
		if gained[d.ID] {
			e.Hub.Project(d)
		}
	}
	return reply
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

// Rematch runs matching for every thread not filed by hand after a change
// to a project's folder (SPEC 0.5 V0.55): a thread moves to the project
// whose folder covers it, the longest first; a thread that nothing covers
// keeps its project.
func (e *Engine) Rematch() {
	e.mu.Lock()
	now := e.Now()
	list := e.projectList()
	var moved []*core.Thread
	for _, t := range e.threads {
		old := t.ProjectID
		e.assignProject(t)
		if !t.ProjectManual {
			if id := core.MatchFolder(list, t.Cwd); id != "" {
				t.ProjectID, t.Project = id, e.projects[id].Name
			}
		}
		if t.ProjectID != old {
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
