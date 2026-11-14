package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
}

// gitInfo returns the main repository root and the normalised origin remote
// of a folder; outside git it returns the folder and "".
func gitInfo(cwd string) (root, remote string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return filepath.Clean(cwd), ""
	}
	common := strings.TrimSpace(string(out))
	root = common
	if filepath.Base(common) == ".git" {
		root = filepath.Dir(common)
	}
	// A dotfiles repository in the home folder is not a project root.
	if home, _ := os.UserHomeDir(); root == "/" || (home != "" && root == filepath.Clean(home)) {
		return filepath.Clean(cwd), ""
	}
	if r, err := exec.CommandContext(ctx, "git", "-C", cwd, "remote", "get-url", "origin").Output(); err == nil {
		remote = core.NormalizeRemote(string(r))
	}
	return root, remote
}

// homeOrRoot reports whether a folder is the home folder or /: never an
// auto project, because it would hold every folder below it.
func homeOrRoot(dir string) bool {
	dir = filepath.Clean(dir)
	home, _ := os.UserHomeDir()
	return dir == "/" || (home != "" && dir == filepath.Clean(home))
}

// bestProject prefers seeded or user-made projects over auto ones: an auto
// project only matches when no other project does (SPEC 0.5 V0.23).
func (e *Engine) bestProject(cwd, remote string) string {
	var chosen, auto []core.Project
	for _, p := range e.projects {
		if p.Auto && !p.Edited {
			if homeOrRoot(p.Folder(e.HostID)) {
				continue
			}
			auto = append(auto, *p)
		} else {
			chosen = append(chosen, *p)
		}
	}
	if id := core.ProjectFor(chosen, e.HostID, cwd, remote); id != "" {
		return id
	}
	return core.ProjectFor(auto, e.HostID, cwd, remote)
}

func (e *Engine) projectList() []core.Project {
	out := make([]core.Project, 0, len(e.projects))
	for _, p := range e.projects {
		out = append(out, *p)
	}
	return out
}

// assignProject gives t a project, making one when none matches. Call with
// e.mu held; it returns a new project to persist, or nil.
func (e *Engine) assignProject(t *core.Thread, now time.Time) *core.Project {
	if t.Cwd == "" {
		return nil
	}
	if t.ProjectID != "" && e.projects[t.ProjectID] != nil {
		t.Project = e.projects[t.ProjectID].Name
		return nil
	}
	if id := e.bestProject(t.Cwd, ""); id != "" {
		t.ProjectID, t.Project = id, e.projects[id].Name
		return nil
	}
	root, remote := gitInfo(t.Cwd)
	if id := e.bestProject(root, remote); id != "" {
		t.ProjectID, t.Project = id, e.projects[id].Name
		return nil
	}
	if homeOrRoot(root) {
		t.ProjectID, t.Project = "", ""
		return nil
	}
	name := filepath.Base(root)
	p := &core.Project{ID: NewID("p_"), Auto: true, Name: name, Color: core.DefaultColor(name), Icon: core.Icon{Kind: "monogram"},
		Sources: []core.Source{{Kind: "folder", Host: e.HostID, Path: root}}, CreatedAt: now}
	if remote != "" {
		p.Sources = append(p.Sources, core.Source{Kind: "repo", URL: remote})
	}
	e.projects[p.ID] = p
	t.ProjectID, t.Project = p.ID, p.Name
	return p
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
	for _, p := range e.projects {
		out = append(out, ProjectDTO{Project: *p, Threads: counts[p.ID], LastUsed: last[p.ID]})
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

// projectCommand handles project.update, project.create, project.addSource
// and project.removeSource.
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
		e.projects[p.ID] = p
	case "project.update", "project.addSource", "project.removeSource":
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
		}
		p.Edited = true
		e.projects[p.ID] = p
	default:
		e.mu.Unlock()
		return bad("unknown action " + c.Action)
	}
	// The name shows on threads: refresh them.
	var touched []DTO
	now := e.Now()
	for _, t := range e.threads {
		if t.ProjectID == p.ID && t.Project != p.Name {
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
	for _, d := range e.Projects() {
		if d.ID == p.ID {
			e.Hub.Project(d)
		}
	}
	for _, d := range touched {
		e.Hub.Thread(d)
	}
	if c.Action != "project.update" {
		e.Rematch() // the sources changed: threads may belong elsewhere now
	}
	return reply
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

// Rematch moves every thread not filed by hand to the best folder project
// and removes auto projects that are left empty and were never edited
// (SPEC 0.5 V0.23). Call it after SeedProjects at start.
func (e *Engine) Rematch() {
	e.mu.Lock()
	now := e.Now()
	// A project from before the auto flag (not auto, not edited, not
	// seeded) was made by matching: it is an auto project.
	var made []*core.Project
	for _, p := range e.projects {
		if !p.Auto && !p.Edited && !p.Seeded {
			p.Auto = true
			made = append(made, p)
		}
	}
	var moved []*core.Thread
	for _, t := range e.threads {
		if t.ProjectManual || t.Cwd == "" {
			continue
		}
		old := t.ProjectID
		t.ProjectID = ""
		if p := e.assignProject(t, now); p != nil {
			made = append(made, p)
		}
		if t.ProjectID != old {
			e.bump(t.ID)
			moved = append(moved, t)
		}
	}
	used := map[string]bool{}
	for _, t := range e.threads {
		used[t.ProjectID] = true
	}
	var gone []string
	for id, p := range e.projects {
		if p.Auto && !p.Edited && !used[id] {
			gone = append(gone, id)
			delete(e.projects, id)
		}
	}
	var dtos []DTO
	for _, t := range moved {
		dtos = append(dtos, e.dto(t, now))
	}
	var keep []*core.Project
	for _, p := range made {
		if _, ok := e.projects[p.ID]; ok {
			keep = append(keep, p)
		}
	}
	e.mu.Unlock()
	e.st.Write(func(tx *store.Tx) error {
		for _, p := range keep {
			if err := tx.PutProject(p); err != nil {
				return err
			}
		}
		for _, t := range moved {
			if err := tx.PutThread(t); err != nil {
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
	if len(keep) > 0 {
		for _, d := range e.Projects() {
			e.Hub.Project(d)
		}
	}
	for _, d := range dtos {
		e.Hub.Thread(d)
	}
}
