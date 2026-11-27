package cgroup

// V0.services-not-jobs (decision 0036 (D)): services are not jobs. A process
// whose command matches a service pattern (a dev stack, a dev server, `docker
// compose up`) and the containers of a compose project a thread started are
// the thread's services. Their memory is reserved from their learned peak (a
// decaying max with a 48 h half-life) for as long as they run, so admission
// counts it before a batch job starts. The undeclared-work detector does not
// count it, and the guard never freezes a service, never takes one as a root
// or an elder, and never names one as a part in a turn.

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
	"github.com/meros/fyr/internal/kvstore"
)

const (
	servicePeaksKey = "service-peaks"
	// serviceNoticedKey: when each thread was last told about its containers.
	serviceNoticedKey = "service-noticed"
	// serviceHalfLife is how fast a service's learned peak decays (Autopilot's 48 h).
	serviceHalfLife = 48 * time.Hour
	// composeGrace is how long a compose project a thread started counts with
	// no container running (docker may still be starting them).
	composeGrace = 2 * time.Minute
	// serviceNoticeEvery is how often a thread is told about its containers.
	serviceNoticeEvery = time.Hour
	serviceNameMax     = 100
)

// Service is one running service of a thread.
type Service struct {
	Thread string `json:"-"`
	// Name is the command, or "compose <project>" for containers whose client ended (`up -d`).
	Name string `json:"name"`
	// Memory is what its processes hold in the thread's cgroup (their RSS).
	Memory uint64 `json:"memory"`
	// Project and Containers: the compose project and what its containers hold, outside agents/.
	Project    string `json:"project,omitempty"`
	Containers uint64 `json:"containers,omitempty"`
	// Reserve is what admission counts for it: its learned peak, at least what it holds now.
	Reserve uint64 `json:"reserve"`
	PID     int    `json:"-"`
}

type servicePeak struct {
	Peak float64   `json:"peak"`
	At   time.Time `json:"at"`
}

// Services follows the threads' services. Match is the service patterns
// (*heavy.Services, set by the wiring); Cwd a thread's folder, for a compose
// project named by its directory; Trees and CLIs read each thread's process
// tree on hosts without cgroups (macOS, Linux without delegation). Each optional.
type Services struct {
	Match  func(command string) bool
	Docker *Docker
	KV     kvstore.Table
	Cwd    func(thread string) string
	Now    func() time.Time
	Proc   string
	Trees  *TreeReader
	CLIs   func() map[string]int

	mu      sync.Mutex
	loaded  bool
	compose map[string]map[string]time.Time // thread → compose project → when it was started
	peaks   map[string]servicePeak          // class → decaying max
	dirty   bool
	saved   time.Time
	noticed map[string]time.Time
	pub     atomic.Pointer[map[string][]Service]
}

func (s *Services) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Services) proc() string {
	if s.Proc == "" {
		return "/proc"
	}
	return s.Proc
}

// IsService says a process's command line is a service: it matches a service
// pattern and is not the CLI's shell around a Bash command (its child is the
// service). For the guard: a service is never frozen or named in a turn.
func (s *Services) IsService(cmdline string) bool {
	if s == nil || s.Match == nil {
		return false
	}
	return !agentShell(cmdline) && s.Match(cmdline)
}

// Bash hears a thread's Bash command: a compose project it starts (`up`,
// with or without -d) belongs to the thread until a command of the thread
// stops it (`down`, `stop`, `kill`, `rm`) or no container of it runs.
func (s *Services) Bash(thread, command string) {
	if s == nil {
		return
	}
	cwd := ""
	if s.Cwd != nil {
		cwd = s.Cwd(thread)
	}
	project, up, down := ComposeProject(command, cwd)
	if project == "" || (!up && !down) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.compose == nil {
		s.compose = map[string]map[string]time.Time{}
	}
	if down {
		delete(s.compose[thread], project)
		return
	}
	if s.compose[thread] == nil {
		s.compose[thread] = map[string]time.Time{}
	}
	s.compose[thread][project] = s.now()
}

// Of is thread's services at the last tick.
func (s *Services) Of(thread string) []Service {
	if s == nil {
		return nil
	}
	if p := s.pub.Load(); p != nil {
		return (*p)[thread]
	}
	return nil
}

// InCgroup is what thread's services hold in its cgroup: the undeclared-work
// detector takes it off the thread's working set.
func (s *Services) InCgroup(thread string) uint64 {
	var n uint64
	for _, v := range s.Of(thread) {
		n += v.Memory
	}
	return n
}

// Extras are the services' reservations for admission: one per thread, named
// by name (its project or title).
func (s *Services) Extras(name func(thread string) string) []resource.Extra {
	if s == nil {
		return nil
	}
	p := s.pub.Load()
	if p == nil {
		return nil
	}
	var out []resource.Extra
	for th, vs := range *p {
		var r uint64
		for _, v := range vs {
			r += v.Reserve
		}
		if r > 0 {
			out = append(out, resource.Extra{Owner: th, Name: name(th), Need: resource.Need{Mem: r}, Service: true})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Owner < out[j].Owner })
	return out
}

// Notice tells a thread, at most once per serviceNoticeEvery, that the
// containers it started hold memory outside its cgroup: at least the
// undeclared memory threshold's share of the agents' capacity c. "" otherwise.
func (s *Services) Notice(thread string, c Capacity) string {
	if s == nil || c.Memory == 0 {
		return ""
	}
	var big Service
	for _, v := range s.Of(thread) {
		if v.Containers > big.Containers {
			big = v
		}
	}
	if float64(big.Containers) < DefaultUndeclared.Memory*float64(c.Memory) {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	now := s.now()
	if now.Sub(s.noticed[thread]) < serviceNoticeEvery {
		return ""
	}
	s.noticed[thread] = now
	s.saveNoticedLocked(now)
	return fmt.Sprintf("[Fyr] The containers of compose project %s, which this thread started, hold %s outside this thread's own memory. "+
		"Fyr counts them as this thread's service: it reserves that memory before admitting heavy jobs and never pauses them. "+
		"Stop them (`docker compose -p %s down`) when this thread no longer needs them.", big.Project, resource.GB(float64(big.Containers)), big.Project)
}

func (s *Services) loadLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	s.peaks = map[string]servicePeak{}
	s.noticed = map[string]time.Time{}
	if s.KV == nil {
		return
	}
	if raw, _ := s.KV.GetKV(servicePeaksKey); raw != "" {
		json.Unmarshal([]byte(raw), &s.peaks)
	}
	if raw, _ := s.KV.GetKV(serviceNoticedKey); raw != "" {
		json.Unmarshal([]byte(raw), &s.noticed)
	}
}

// saveNoticedLocked keeps when each thread was last told about its
// containers (V0.survive-update), so a restart does not tell it again within
// serviceNoticeEvery; an entry older than that is dropped. Holds s.mu.
func (s *Services) saveNoticedLocked(now time.Time) {
	for th, at := range s.noticed {
		if now.Sub(at) >= serviceNoticeEvery {
			delete(s.noticed, th)
		}
	}
	if s.KV == nil {
		return
	}
	if len(s.noticed) == 0 {
		s.KV.SetKV(serviceNoticedKey, "")
		return
	}
	if b, err := json.Marshal(s.noticed); err == nil {
		s.KV.SetKV(serviceNoticedKey, string(b))
	}
}

// learn folds a service's memory now into its class's decaying max and
// returns the reservation. Holds s.mu.
func (s *Services) learn(class string, cur uint64, now time.Time) uint64 {
	p := s.peaks[class]
	decayed := p.Peak
	if !p.At.IsZero() {
		decayed *= math.Pow(0.5, now.Sub(p.At).Hours()/serviceHalfLife.Hours())
	}
	v := max(float64(cur), decayed)
	if v != p.Peak {
		s.dirty = true
	}
	s.peaks[class] = servicePeak{Peak: v, At: now}
	return uint64(v)
}

// procEntry is one process of a thread's cgroup.
type procEntry struct {
	ppid int
	cmd  string
	rss  uint64
}

// HoldChecker returns a predicate reporting whether ctl holds any of a
// thread's running services (decision 0036 (D)): the guard passes over such a
// thread instead of freezing its service.
func (s *Services) HoldChecker(ctl Control) func(thread string) bool {
	return func(thread string) bool {
		if s == nil || ctl == nil {
			return false
		}
		for _, sv := range s.Of(thread) {
			if ctl.Holds(thread, sv.PID) {
				return true
			}
		}
		return false
	}
}

// Tick reads each thread's services: from its cgroup with a tree, from its
// process tree without one (macOS), and the compose projects it started.
// The sampler's Tick calls it, outside every lock.
func (s *Services) Tick(tr *Tree, threads []string) {
	if s == nil {
		return
	}
	now := s.now()
	found := map[string][]Service{}
	if tr != nil && s.Match != nil {
		for _, th := range threads {
			if th == "probe" || th == DeployDir {
				continue
			}
			found[th] = s.read(tr, th)
		}
	} else if tr == nil && s.Match != nil && s.Trees != nil && s.CLIs != nil {
		if clis := s.CLIs(); len(clis) > 0 {
			if tab, ok := s.Trees.Table(now); ok {
				kids := tab.kids()
				for _, th := range threads {
					if th == "probe" || th == DeployDir {
						continue
					}
					if cli := clis[th]; cli > 0 {
						if svcs := s.readTree(tab, kids, th, cli); len(svcs) > 0 {
							found[th] = svcs
						}
					}
				}
			}
		}
	}
	s.mu.Lock()
	s.loadLocked()
	compose := map[string]map[string]time.Time{}
	for th, ps := range s.compose {
		compose[th] = map[string]time.Time{}
		for p, at := range ps {
			compose[th][p] = at
		}
	}
	s.mu.Unlock()
	// The containers, outside every lock (docker is asked at most every 10 s).
	for th, ps := range compose {
		counted := map[string]bool{}
		for i, v := range found[th] {
			if v.Project != "" {
				counted[v.Project] = true
				found[th][i].Containers, _ = s.Docker.Memory(v.Project)
			}
		}
		for p, at := range ps {
			if counted[p] {
				continue
			}
			mem, n := s.Docker.Memory(p)
			if n == 0 {
				if now.Sub(at) >= composeGrace {
					s.mu.Lock()
					delete(s.compose[th], p)
					s.mu.Unlock()
				}
				continue
			}
			found[th] = append(found[th], Service{Thread: th, Name: "compose " + p, Project: p, Containers: mem})
		}
	}
	// A compose client the thread runs counts its containers too.
	for th, vs := range found {
		if compose[th] != nil {
			continue
		}
		for i, v := range vs {
			if v.Project != "" {
				found[th][i].Containers, _ = s.Docker.Memory(v.Project)
			}
		}
	}
	s.mu.Lock()
	out := map[string][]Service{}
	for th, vs := range found {
		for _, v := range vs {
			class := v.Name
			if v.Project != "" {
				class = "compose " + v.Project
			}
			v.Reserve = s.learn(class, v.Memory+v.Containers, now)
			out[th] = append(out[th], v)
		}
	}
	var raw []byte
	if s.dirty && s.KV != nil && now.Sub(s.saved) >= time.Minute {
		raw, _ = json.Marshal(s.peaks)
		s.dirty, s.saved = false, now
	}
	s.mu.Unlock()
	s.pub.Store(&out)
	if raw != nil {
		s.KV.SetKV(servicePeaksKey, string(raw))
	}
}

// read finds thread's service processes from its cgroup.
func (s *Services) read(tr *Tree, thread string) []Service {
	td := tr.threadDir(unsafe.ReplaceAllString(thread, "_")) // its scope, or agents/<thread> (V0.agents-unit)
	proc := s.proc()
	table := map[int]procEntry{}
	for _, p := range undeclaredPids(td) {
		st, ok := readProcStat(proc, p)
		if !ok {
			continue
		}
		table[p] = procEntry{ppid: st.ppid, cmd: readCmdline(proc, p), rss: readRSS(proc, p)}
	}
	return s.findServices(thread, table, func(p int) string {
		cwd, _ := os.Readlink(filepath.Join(proc, strconv.Itoa(p), "cwd"))
		return cwd
	})
}

// readTree finds thread's service processes from the process tree under cli
// (hosts without cgroups, such as macOS).
func (s *Services) readTree(tab procTable, kids map[int][]int, thread string, cli int) []Service {
	if _, ok := tab[cli]; !ok {
		return nil
	}
	table := map[int]procEntry{}
	for _, p := range under(kids, cli) {
		if p == cli {
			continue
		}
		r := tab[p]
		table[p] = procEntry{ppid: r.ppid, cmd: r.args, rss: r.rss}
	}
	threadCwd := ""
	if s.Cwd != nil {
		threadCwd = s.Cwd(thread)
	}
	return s.findServices(thread, table, func(int) string { return threadCwd })
}

// findServices finds the topmost process of each subtree in table whose
// command is a service, with the subtree's memory.
func (s *Services) findServices(thread string, table map[int]procEntry, cwdOf func(pid int) string) []Service {
	children := map[int][]int{}
	for p, e := range table {
		children[e.ppid] = append(children[e.ppid], p)
	}
	var sum func(p, depth int) uint64
	sum = func(p, depth int) uint64 {
		if depth > 64 {
			return 0
		}
		n := table[p].rss
		for _, c := range children[p] {
			n += sum(c, depth+1)
		}
		return n
	}
	underSvc := func(p int) bool { // an ancestor in the thread is a service
		for q, n := table[p].ppid, 0; n < 64; q, n = table[q].ppid, n+1 {
			e, ok := table[q]
			if !ok {
				return false
			}
			if s.IsService(e.cmd) {
				return true
			}
		}
		return false
	}
	var out []Service
	for p, e := range table {
		if !s.IsService(e.cmd) || underSvc(p) {
			continue
		}
		v := Service{Thread: thread, Name: e.cmd, PID: p, Memory: sum(p, 0)}
		if len(v.Name) > serviceNameMax {
			v.Name = v.Name[:serviceNameMax]
		}
		if project, _, _ := ComposeProject(e.cmd, cwdOf(p)); project != "" {
			v.Project = project
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}
