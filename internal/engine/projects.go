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
	if r, err := exec.CommandContext(ctx, "git", "-C", cwd, "remote", "get-url", "origin").Output(); err == nil {
		remote = core.NormalizeRemote(string(r))
	}
	return root, remote
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
	if id := core.ProjectFor(e.projectList(), e.HostID, t.Cwd, ""); id != "" {
		t.ProjectID, t.Project = id, e.projects[id].Name
		return nil
	}
	root, remote := gitInfo(t.Cwd)
	if id := core.ProjectFor(e.projectList(), e.HostID, root, remote); id != "" {
		t.ProjectID, t.Project = id, e.projects[id].Name
		return nil
	}
	name := filepath.Base(root)
	p := &core.Project{ID: NewID("p_"), Name: name, Color: core.DefaultColor(name), Icon: core.Icon{Kind: "monogram"},
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
			changed = append(changed, hit)
			continue
		}
		p := sd
		p.ID = NewID("p_")
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

// projectCommand handles project.update, project.create and project.addSource.
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
		if p.Name == "" && len(p.Sources) > 0 {
			p.Name = filepath.Base(p.Sources[0].Path + p.Sources[0].URL)
		}
		if strings.TrimSpace(p.Name) == "" {
			e.mu.Unlock()
			return bad("a project needs a name or a source")
		}
		p.Color = core.DefaultColor(p.Name)
		e.projects[p.ID] = p
	default:
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
			if s.Kind != "folder" && s.Kind != "repo" {
				e.mu.Unlock()
				return bad("source kind must be folder or repo")
			}
			if s.Kind == "repo" {
				s.URL = core.NormalizeRemote(s.URL)
			}
			p.Sources = append(append([]core.Source{}, p.Sources...), s)
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
	return reply
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}
