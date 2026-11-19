package cgroup

import (
	"os"
	"path/filepath"
	"syscall"
)

// KillJob kills thread's job (or its undeclared work) to free its memory
// (V0.rerun-classes, decision 0036 (F)): cgroup.kill on its cgroup, which
// takes every process under it, frozen or not, then process_mrelease on each
// so the memory comes back at once (Linux 5.15+). Without cgroups it sends
// SIGKILL to each process the guard stopped, and to the whole group of one
// that leads its own process group. The freeze is cleared. It returns how
// many processes it killed; 0 when nothing was there.
func (g *JobGroups) KillJob(thread string, undeclared bool) int {
	if g == nil {
		return 0
	}
	if g.canStop() {
		return g.killStopped(thread, undeclared)
	}
	if g.Tree == nil {
		return 0
	}
	d := g.dir(thread)
	if undeclared {
		d = g.udir(thread)
	}
	ps := treePIDs(d)
	if len(ps) == 0 {
		return 0
	}
	release := releaseMemory(ps)
	if err := put(d, "cgroup.kill", "1"); err != nil {
		release(false)
		return 0
	}
	release(true)
	if undeclared {
		g.ThawUndeclared(thread)
	} else {
		g.Thaw(thread)
	}
	return len(ps)
}

// treePIDs are the processes of dir and every cgroup under it.
func treePIDs(dir string) []int {
	var out []int
	filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err == nil && e.IsDir() {
			out = append(out, pids(filepath.Join(p, "cgroup.procs"))...)
		}
		return nil
	})
	return out
}

// killStopped sends SIGKILL to what the guard stopped for thread.
func (g *JobGroups) killStopped(thread string, undeclared bool) int {
	key := stopJob + thread
	if undeclared {
		key = stopUndeclared + thread
	}
	g.stopMu.Lock()
	g.loadStopped()
	ps := g.stopped[key]
	delete(g.stopped, key)
	g.saveStopped()
	g.stopMu.Unlock()
	n := 0
	for _, p := range ps {
		if pg, err := syscall.Getpgid(p); err == nil && pg == p {
			g.signal(-p, syscall.SIGKILL) // it leads its group: the whole group
		}
		if g.signal(p, syscall.SIGKILL) == nil {
			n++
		}
	}
	g.mu.Lock()
	if undeclared {
		delete(g.ufrozen, thread)
	} else {
		delete(g.frozen, thread)
	}
	g.mu.Unlock()
	return n
}
