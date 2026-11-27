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
	"strconv"
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
	PS func() (string, error)
	// Stats lists container memory as "<full id>\t<mem usage>" lines when
	// container cgroups are not on this host (macOS, rootless Docker); default
	// runs `docker stats --no-stream`.
	Stats func() (string, error)
	FS    string // the cgroup2 mount, default /sys/fs/cgroup
	Now   func() time.Time

	mu       sync.Mutex
	at       time.Time
	projects map[string][]string // project → container ids
	statsAt  time.Time
	stats    map[string]uint64 // container id → memory bytes
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

func dockerStats() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--no-trunc", "--format", "{{.ID}}\t{{.MemUsage}}").Output()
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

// memStats reads container memory from docker stats, cached for dockerEvery.
// Only called on hosts where container cgroups are not directly readable (macOS).
func (d *Docker) memStats() map[string]uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stats != nil && d.now().Sub(d.statsAt) < dockerEvery {
		return d.stats
	}
	fn := d.Stats
	if fn == nil {
		fn = dockerStats
	}
	d.statsAt = d.now()
	out, err := fn()
	d.stats = map[string]uint64{}
	if err != nil {
		return d.stats
	}
	for _, line := range strings.Split(out, "\n") {
		id, usage, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if ok && id != "" && usage != "" {
			if n := parseDockerMem(usage); n > 0 {
				d.stats[id] = n
			}
		}
	}
	return d.stats
}

// parseDockerMem parses the used half of docker stats' MemUsage
// (e.g. "1.23GiB / 7.77GiB", "450MiB / 8GiB", "120MB / 8GB").
func parseDockerMem(usage string) uint64 {
	used, _, _ := strings.Cut(usage, "/")
	s := strings.TrimSpace(used)
	if s == "" {
		return 0
	}
	i := 0
	for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == '.') {
		i++
	}
	if i == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(s[:i], 64)
	if err != nil || v <= 0 {
		return 0
	}
	mult := 1.0
	switch strings.TrimSpace(s[i:]) {
	case "B", "":
		mult = 1
	case "KiB":
		mult = 1 << 10
	case "MiB":
		mult = 1 << 20
	case "GiB":
		mult = 1 << 30
	case "TiB":
		mult = 1 << 40
	case "kB", "KB":
		mult = 1e3
	case "MB":
		mult = 1e6
	case "GB":
		mult = 1e9
	case "TB":
		mult = 1e12
	default:
		return 0
	}
	return uint64(v * mult)
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
	if len(ids) == 0 {
		return 0, 0
	}
	var sum uint64
	foundCgroup := false
	for _, id := range ids {
		// systemd's cgroup driver, then cgroupfs's.
		for _, p := range []string{
			filepath.Join(fs, "system.slice", "docker-"+id+".scope"),
			filepath.Join(fs, "docker", id),
		} {
			if _, err := os.Stat(p); err == nil {
				sum += readBytes(filepath.Join(p, "memory.current"))
				foundCgroup = true
				break
			}
		}
	}
	if !foundCgroup {
		st := d.memStats()
		for _, id := range ids {
			sum += st[id]
		}
	}
	return sum, len(ids)
}
