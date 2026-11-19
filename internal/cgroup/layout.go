package cgroup

// V0.agents-unit (decision 0036 (B), #221): the agents leave fyrd's unit.
//
//	system.slice/fyr.service      fyrd and the launcher; root-owned, not
//	                              delegated, ManagedOOMPreference=omit
//	fyr.slice/fyr-agents.slice    the agents' budget and the oomd rule
//	  fyr-agent-<thread>.scope    one thread: Delegate=yes (fyrd makes job/
//	    job/ undeclared/          and undeclared/ in it), OOMPolicy=kill,
//	                              so memory.oom.group=1
//	  fyr-deploy-<n>.scope        one command of a self-deploy
//
// A process in fyr.service cannot start in, or move into, a cgroup under
// another slice: the kernel wants write access to the common ancestor, which
// is root's. So each thread's CLI starts through `systemd-run --scope`, which
// asks the system manager (polkit, see nix/module.nix) to make the scope with
// its own pid and then execs the CLI there. Inside the delegated scope fyrd
// moves processes as before.
//
// Without systemd-run, without permission or without the module's slice the
// tree keeps the V0.80 layout ("unit"), or is off.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Layout is where the agents' cgroups are.
type Layout string

const (
	// LayoutUnit is V0.80: fyr.service/agents/<thread>.
	LayoutUnit Layout = "unit"
	// LayoutScope is V0.agents-unit: <agents slice>/fyr-agent-<thread>.scope.
	LayoutScope Layout = "scope"
)

const (
	scopePrefix  = "fyr-agent-"
	deployPrefix = "fyr-deploy-"
	scopeSuffix  = ".scope"
	// probePrefix follows scopePrefix in a scope that is no thread: the
	// layout's probe at setup and the slowdown probe (V0.probe-scope).
	probePrefix = "probe-"
)

// scopes starts processes in transient scopes of the system manager.
type scopes struct {
	run, ctl string // systemd-run and systemctl
	slice    string // the agents' slice, fyr-agents.slice
	user     string // the delegated scope's owner: fyrd's user
	partOf   string // fyrd's unit: stopping it stops the agents, as before
	userBus  bool   // tests: the user manager instead of the system's
	// resetOOM: fyrd runs with a negative oom_score_adj, which its children
	// inherit; the agents go back to 0.
	resetOOM bool
	exec     func(name string, args ...string) error // tests; nil runs it
}

// scopeUnit is the unit of a thread's process: gen 0 is the plain name, a
// second process while the first still runs (a handover) gets .1, and so on.
func scopeUnit(name string, gen int) string {
	if gen == 0 {
		return scopePrefix + name + scopeSuffix
	}
	return scopePrefix + name + "." + strconv.Itoa(gen) + scopeSuffix
}

// scopeThread is the thread (cgroup name) and generation of a child of the
// agents' slice; a deploy's scope is DeployDir.
func scopeThread(base string) (string, int, bool) {
	if !strings.HasSuffix(base, scopeSuffix) {
		return "", 0, false
	}
	s := strings.TrimSuffix(base, scopeSuffix)
	if strings.HasPrefix(s, deployPrefix) {
		return DeployDir, 0, true
	}
	s, ok := strings.CutPrefix(s, scopePrefix)
	if !ok || s == "" || strings.HasPrefix(s, probePrefix) {
		return "", 0, false
	}
	gen := 0
	if i := strings.IndexByte(s, '.'); i >= 0 {
		n, err := strconv.Atoi(s[i+1:])
		if err != nil {
			return "", 0, false
		}
		s, gen = s[:i], n
	}
	return s, gen, s != ""
}

// prefix is the command line that starts the rest in scope unit, with
// cpu.weight w (empty for the default).
func (s *scopes) prefix(unit, w string) []string {
	p := []string{s.run}
	if s.userBus {
		p = append(p, "--user")
	}
	p = append(p, "--scope", "--quiet", "--no-ask-password", "--collect", "--unit="+unit, "--slice="+s.slice,
		"--property=Delegate=yes", "--property=OOMPolicy=kill")
	if s.user != "" && !s.userBus {
		p = append(p, "--property=User="+s.user) // the delegated cgroup's owner
	}
	if s.partOf != "" {
		p = append(p, "--property=PartOf="+s.partOf)
	}
	if w != "" {
		p = append(p, "--property=CPUWeight="+w)
	}
	p = append(p, "--")
	if s.resetOOM {
		p = append(p, "sh", "-c", `echo 0 >/proc/self/oom_score_adj 2>/dev/null; exec "$0" "$@"`)
	}
	return p
}

// setProperty sets a property of a running scope (--runtime: it goes with
// the scope).
func (s *scopes) setProperty(unit, prop, v string) error {
	args := []string{}
	if s.userBus {
		args = append(args, "--user")
	}
	args = append(args, "--no-ask-password", "set-property", "--runtime", unit, prop+"="+v)
	if s.exec != nil {
		return s.exec(s.ctl, args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, s.ctl, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v %s", prop, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// unitProperty is the systemd property for a cgroup file fyrd writes on a
// thread cgroup, and its value: the file of a scope is root's.
func unitProperty(file, v string) (string, string, bool) {
	switch file {
	case "cpu.weight":
		return "CPUWeight", v, true
	case "memory.high":
		if v == "max" {
			v = "infinity"
		}
		return "MemoryHigh", v, true
	}
	return "", "", false
}

// Layout is where the agents' cgroups are ("" when off).
func (t *Tree) Layout() Layout {
	if t == nil {
		return ""
	}
	return t.layout
}

// AgentsDir is the cgroup that holds every thread's: the agents' budget.
func (t *Tree) AgentsDir() string {
	if t == nil {
		return ""
	}
	return t.agentsDir()
}

// agentsDir is t.agents, or agents/ under the root for a Tree a test made by
// hand.
func (t *Tree) agentsDir() string {
	if t.agents == "" {
		return filepath.Join(t.root, "agents")
	}
	return t.agents
}

func pathExists(p string) bool { _, err := os.Stat(p); return err == nil }

// threadDir is the cgroup of thread name (sanitized): in the scope layout the
// scope of its newest process, else the legacy agents/<name> of a process
// from before the switch, else where its next scope will be.
func (t *Tree) threadDir(name string) string {
	if t.layout != LayoutScope {
		return filepath.Join(t.agentsDir(), name)
	}
	t.curMu.Lock()
	d := t.cur[name]
	t.curMu.Unlock()
	if d != "" && pathExists(d) {
		return d
	}
	best, bestGen := "", -1
	es, _ := os.ReadDir(t.agentsDir())
	for _, e := range es {
		if n, g, ok := scopeThread(e.Name()); ok && e.IsDir() && n == name && g > bestGen {
			best, bestGen = filepath.Join(t.agentsDir(), e.Name()), g
		}
	}
	if best != "" {
		return best
	}
	if t.legacy != "" {
		if l := filepath.Join(t.legacy, name); pathExists(l) {
			return l
		}
	}
	return filepath.Join(t.agentsDir(), scopeUnit(name, 0))
}

// threadCg is one cgroup of a thread (or of the deploy, DeployDir).
type threadCg struct{ Name, Dir string }

// threadCgroups are the cgroups of every thread and the deploy, in name
// order; in the scope layout also the legacy ones that still hold work from
// before the switch. A thread may have two (a handover's overlap).
func (t *Tree) threadCgroups() []threadCg {
	var out []threadCg
	plain := func(dir string) {
		es, _ := os.ReadDir(dir)
		for _, e := range es {
			if e.IsDir() && e.Name() != "probe" {
				out = append(out, threadCg{e.Name(), filepath.Join(dir, e.Name())})
			}
		}
	}
	if t.layout != LayoutScope {
		plain(t.agentsDir())
		return out
	}
	es, _ := os.ReadDir(t.agentsDir())
	for _, e := range es {
		if n, _, ok := scopeThread(e.Name()); ok && e.IsDir() {
			out = append(out, threadCg{n, filepath.Join(t.agentsDir(), e.Name())})
		}
	}
	if t.legacy != "" {
		plain(t.legacy)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// setAttr writes a control file of a thread cgroup: directly, or through the
// system manager for a scope, whose own files are root's.
func (t *Tree) setAttr(dir, file, v string) error {
	if t.sc != nil && filepath.Dir(dir) == t.agentsDir() && strings.HasSuffix(dir, scopeSuffix) {
		if prop, pv, ok := unitProperty(file, v); ok {
			return t.sc.setProperty(filepath.Base(dir), prop, pv)
		}
	}
	return put(dir, file, v)
}

// pendingFor is how long a scope name handed out stays taken before its
// scope exists: systemd-run makes it within a second.
const pendingFor = time.Minute

// freeUnit is the first scope name of thread name that is not in use. Holds t.mu.
func (t *Tree) freeUnit(name string) string {
	for gen := 0; ; gen++ {
		u := scopeUnit(name, gen)
		if at, ok := t.pending[u]; ok && time.Since(at) < pendingFor {
			continue // given to a process that has not started yet
		}
		if !pathExists(filepath.Join(t.agentsDir(), u)) {
			return u
		}
	}
}

// DeployWrap is the command line that starts one command of a self-deploy in
// a scope of its own under the agents' slice, or nil in the other layouts
// (Deploy is the directory there).
func (t *Tree) DeployWrap() func() []string {
	if t == nil || t.layout != LayoutScope {
		return nil
	}
	return func() []string {
		n := t.deploySeq.Add(1)
		return t.sc.prefix(fmt.Sprintf("%s%d-%d%s", deployPrefix, os.Getpid(), n, scopeSuffix), "")
	}
}

// ScopeProbe starts one process through the scopes and reports the cgroup it
// ran in, relative to the cgroup2 mount, and its memory.oom.group.
type ScopeProbe func(prefix []string) (path, oomGroup string, err error)

// unitOf is fyrd's own .service unit from its cgroup path (server/ under it
// after a V0.80 split), "" when it runs elsewhere (a dev run in a terminal's
// scope): the scopes are PartOf it.
func unitOf(path string) string {
	base := filepath.Base(strings.TrimSuffix(path, "/server"))
	if strings.HasSuffix(base, ".service") {
		return base
	}
	return ""
}

// setupScopes tries the scope layout: the module named the agents' slice and
// a probe process lands in a delegated scope under it. legacy is the V0.80
// tree when the unit could still be split (the switch: its threads run on
// there), else nil; self is fyrd's cgroup path.
func setupScopes(o Options, legacy *Tree, self string) (*Tree, error) {
	if !o.Enable {
		return nil, errors.New("not configured")
	}
	sc := &scopes{run: o.SystemdRun, ctl: o.Systemctl, slice: o.AgentsSlice, partOf: unitOf(self), userBus: o.userBus, exec: o.scopeExec}
	if sc.run == "" {
		p, err := exec.LookPath("systemd-run")
		if err != nil {
			return nil, errors.New("no systemd-run")
		}
		sc.run = p
	}
	if sc.ctl == "" {
		sc.ctl = filepath.Join(filepath.Dir(sc.run), "systemctl")
	}
	if u, err := user.Current(); err == nil {
		sc.user = u.Username
	}
	if b, err := os.ReadFile(o.oomAdj); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && n < 0 {
			sc.resetOOM = true
		}
	}
	unit := fmt.Sprintf("%s%s%d%s", scopePrefix, probePrefix, o.PID, scopeSuffix)
	path, oomGroup, err := o.ScopeProbe(sc.prefix(unit, ""))
	if err != nil {
		return nil, err
	}
	slice := filepath.Dir(filepath.Join(o.FS, path))
	if filepath.Base(path) != unit || filepath.Base(slice) != o.AgentsSlice {
		return nil, fmt.Errorf("the probe ran in %s, not in %s/%s", path, o.AgentsSlice, unit)
	}
	if oomGroup != "1" {
		o.Logf("cgroups: the probe scope has memory.oom.group=%q, not 1: oomd may take part of a thread", oomGroup)
	}
	t := legacy
	if t == nil {
		t = newTree(filepath.Join(o.FS, self), o)
	} else {
		t.legacy = t.agentsDir()
	}
	t.layout, t.agents, t.sc = LayoutScope, slice, sc
	return t, nil
}

// addThreadLoad is two cgroups of one thread as one reading.
func addThreadLoad(a, b ThreadLoad) ThreadLoad {
	a.Memory += b.Memory
	a.Working += b.Working
	a.Peak = max(a.Peak, b.Peak)
	a.MemoryHigh = max(a.MemoryHigh, b.MemoryHigh)
	if a.CPU == nil {
		a.CPU = b.CPU
	} else if b.CPU != nil {
		c := *a.CPU + *b.CPU
		a.CPU = &c
	}
	if a.CPUPressure == nil || (b.CPUPressure != nil && *b.CPUPressure > *a.CPUPressure) {
		a.CPUPressure = b.CPUPressure
	}
	return a
}
