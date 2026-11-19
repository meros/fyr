package cgroup

// V0.services-not-jobs: the memory of a compose project's containers. They
// live in docker's own cgroups, outside agents/, so the thread that started
// them is told what they hold and admission counts it. The container list is
// asked of docker at most every dockerEvery, and only while some thread runs
// a compose project; each container's memory is its cgroup's memory.current.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// dockerEvery is how often the container list is asked at most.
const dockerEvery = 10 * time.Second

// Docker reads the containers of compose projects.
type Docker struct {
	// PS lists the running containers as "<full id>\t<compose project>" lines;
	// default runs `docker ps`.
	PS  func() (string, error)
	FS  string // the cgroup2 mount, default /sys/fs/cgroup
	Now func() time.Time

	mu       sync.Mutex
	at       time.Time
	projects map[string][]string // project → container ids
}

func (d *Docker) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func dockerPS() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "ps", "--no-trunc", "--format", `{{.ID}}	{{.Label "com.docker.compose.project"}}`).Output()
	return string(out), err
}

// list is the containers per project, asked again when older than dockerEvery.
func (d *Docker) list() map[string][]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.projects != nil && d.now().Sub(d.at) < dockerEvery {
		return d.projects
	}
	ps := d.PS
	if ps == nil {
		ps = dockerPS
	}
	d.at = d.now()
	out, err := ps()
	d.projects = map[string][]string{}
	if err != nil {
		return d.projects // no docker, or it does not answer: nothing is counted
	}
	for _, line := range strings.Split(out, "\n") {
		id, project, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if ok && id != "" && project != "" {
			d.projects[project] = append(d.projects[project], id)
		}
	}
	return d.projects
}

// Memory is what project's running containers hold, and how many there are.
func (d *Docker) Memory(project string) (uint64, int) {
	if d == nil || project == "" {
		return 0, 0
	}
	fs := d.FS
	if fs == "" {
		fs = "/sys/fs/cgroup"
	}
	ids := d.list()[project]
	var sum uint64
	for _, id := range ids {
		// systemd's cgroup driver, then cgroupfs's.
		for _, p := range []string{
			filepath.Join(fs, "system.slice", "docker-"+id+".scope"),
			filepath.Join(fs, "docker", id),
		} {
			if _, err := os.Stat(p); err == nil {
				sum += readBytes(filepath.Join(p, "memory.current"))
				break
			}
		}
	}
	return sum, len(ids)
}
