package engine

import (
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/hostfs"
	"github.com/meros/fyr/internal/store"
	"github.com/meros/fyr/internal/stream"
)

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
}

// projectName is the name of the thread's project, or "" (#33: derived for
// the DTO, never stored). Call with e.mu held.
func (e *Engine) projectName(t *core.Thread) string {
	if p := e.projects[t.ProjectID]; p != nil {
		return p.Name
	}
	return ""
}

// draft is the thread's NewProject, or nil for a scratch folder, the home
// folder, / and a folder that a project already has. Call with e.mu held.
func (e *Engine) draft(t *core.Thread) *api.NewProject {
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
	return &api.NewProject{Name: filepath.Base(dir), Folder: dir}
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
func (e *Engine) Projects() []api.Project {
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
	out := make([]api.Project, 0, len(e.projects))
	now := e.Now()
	rev := e.nextRev()
	for _, p := range e.projects {
		out = append(out, api.Project{Project: *p, Rev: rev, Threads: counts[p.ID], LastUsed: last[p.ID], QuietUntil: e.quietUntil(p.ID, now), Repo: e.projectRepo(p.Folder, now)})
	}
	return out
}

// folderRepo is a folder's git origin as last read.
type folderRepo struct {
	repo string
	at   time.Time
}

// projectRepo is the normalised origin of a project's folder, read at most
// every five minutes. Call with e.mu held: it reads one small file and runs
// no git.
func (e *Engine) projectRepo(dir string, now time.Time) string {
	if dir == "" {
		return ""
	}
	if c, ok := e.repos[dir]; ok && now.Sub(c.at) < 5*time.Minute {
		return c.repo
	}
	if e.repos == nil {
		e.repos = map[string]folderRepo{}
	}
	c := folderRepo{repo: originOf(filepath.Join(dir, ".git", "config")), at: now}
	e.repos[dir] = c
	return c.repo
}

// originOf reads the origin URL from a git config file; "" without one.
func originOf(config string) string {
	b, err := os.ReadFile(config)
	if err != nil {
		return ""
	}
	in := false
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "[") {
			in = l == `[remote "origin"]`
			continue
		}
		if k, v, ok := strings.Cut(l, "="); in && ok && strings.TrimSpace(k) == "url" {
			return agent.NormalizeRemote(strings.TrimSpace(v))
		}
	}
	return ""
}

// checkFolder cleans a project folder and refuses one that cannot match: it
// must be a full path to a folder that exists on this host (SPEC 0.5 V0.23,
// V0.55). "" stays "" (no folder).
func checkFolder(dir string) (string, error) {
	p := strings.TrimSpace(dir)
	if p == "" {
		return "", nil
	}
	p = hostfs.ExpandHome(p, "")
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("the folder must be a full path, like /home/you/src/app")
	}
	p = filepath.Clean(p)
	if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("there is no folder %s on this host", p)
	}
	return p, nil
}

// applyProject is the one path of a project Input (#48): replay the
// receipt, reduce the project, file every thread by the new folders
// (project.matched) and send the reduce's Follow to their threads, store it
// all with the receipt in one transaction, then broadcast the project, the
// threads that moved and the threads that show its name or quiet hours. A
// folder must be a full path to a folder on this host (V0.23, V0.55); its
// clean path is the Input's.
func (e *Engine) applyProject(in core.Input) api.Reply {
	a := maps.Clone(in.Args.(core.Args))
	if _, set := a["folder"]; set {
		folder, err := checkFolder(a.Str("folder"))
		if err != nil {
			return api.Reply{Error: err.Error(), Status: 400}
		}
		a["folder"] = folder
	}
	in.Args = a
	e.mu.Lock()
	if r, done := e.replay(in.CmdID); done {
		e.mu.Unlock()
		return r
	}
	now := e.Now()
	before, after, res, err := core.ReduceProject(e.projects, &core.Project{ID: NewID("p_")}, in, now)
	if err != nil {
		e.mu.Unlock()
		return rejected(err)
	}
	id := before
	if id == nil {
		id = after
	}
	old := e.projects
	e.projects = maps.Clone(old)
	if after != nil {
		e.projects[after.ID] = after
	} else {
		delete(e.projects, id.ID)
	}
	var ss []*staged
	stage := func(in core.Input) error {
		t := e.threads[in.Thread]
		if t == nil {
			return &core.Rejection{Code: core.RejectNotFound, Reason: "no thread " + in.Thread}
		}
		s, err := e.stage(t, in, now, false)
		if err != nil {
			return err
		}
		ss = append(ss, s)
		return nil
	}
	followed := map[string]bool{}
	for _, f := range res.Follow {
		if err = stage(f); err != nil {
			break
		}
		followed[f.Thread] = true
	}
	list := e.projectList()
	for tid, t := range e.threads {
		if err != nil {
			break
		}
		if pid := refiled(t, e.projects, list); pid != t.ProjectID && !followed[tid] {
			err = stage(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindProjectMatched, Thread: tid, Args: core.ProjectMatch{ID: pid}})
		}
	}
	if err != nil {
		e.projects = old
		e.mu.Unlock()
		return rejected(err)
	}
	reply := api.Reply{OK: true, Status: 200, Result: res.Reply}
	if _, err := e.st.Write(func(tx *store.Tx) error {
		if after != nil {
			if err := tx.PutProject(after); err != nil {
				return err
			}
		} else if err := tx.DeleteProject(id.ID); err != nil {
			return err
		}
		for _, ev := range res.Events {
			if err := tx.Append(id.ID, ev.Type, ev.Data, now); err != nil {
				return err
			}
		}
		for _, s := range ss {
			if err := s.persist(tx); err != nil {
				return err
			}
		}
		return tx.PutReceipt(in.CmdID, reply, now)
	}); err != nil {
		e.projects = old
		e.mu.Unlock()
		return api.Reply{Error: err.Error(), Status: 500}
	}
	// The counts of the projects that took or lost threads change; the
	// threads in it show its name and quiet hours.
	frames := map[string]bool{id.ID: true}
	staged := map[string]bool{}
	for _, s := range ss {
		e.commit(s, true)
		staged[s.threadID] = true
		frames[s.cur.ProjectID], frames[s.t.ProjectID] = true, true
	}
	if after != nil {
		e.quietSent[after.ID] = quietKey(e.quietUntil(after.ID, now))
	} else {
		delete(e.quietSent, id.ID)
	}
	var shows []api.Thread
	if after != nil && (before == nil || before.Name != after.Name || !sameJSON(before.Quiet, after.Quiet)) {
		for tid, t := range e.threads {
			if t.ProjectID == after.ID && !staged[tid] {
				e.bump(tid)
				shows = append(shows, e.dto(t, now))
			}
		}
	}
	e.mu.Unlock()
	if after == nil {
		e.Hub.Send(stream.FrameProject, map[string]any{"id": id.ID, "deleted": true}) // SPEC 0.5 V0.58
	}
	for _, d := range e.Projects() {
		if frames[d.ID] {
			e.Hub.Send(stream.FrameProject, d)
		}
	}
	for _, s := range ss {
		e.publish(s.applied)
	}
	for _, d := range shows {
		e.Hub.Send(stream.FrameThread, d)
	}
	return reply
}

// refiled is the project a thread lands in after a change to the projects
// (V0.55, V0.58): a thread whose project is gone loses it and the
// filed-by-hand mark; a thread not filed by hand moves to the project whose
// folder covers it, the longest first, and one that nothing covers keeps
// its project.
func refiled(t *core.Thread, ps map[string]*core.Project, list []*core.Project) string {
	pid, manual := t.ProjectID, t.ProjectManual
	if pid != "" && ps[pid] == nil {
		pid, manual = "", false
	}
	if !manual {
		if m := core.MatchFolder(list, t.Cwd); m != "" {
			pid = m
		}
	}
	return pid
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}
