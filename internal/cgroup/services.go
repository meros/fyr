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

	"github.com/meros/fyr/internal/heavy"
	"github.com/meros/fyr/internal/kvstore"
	"github.com/meros/fyr/internal/slots"
)

const (
	servicePeaksKey = "service-peaks"
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
// (*heavy.Services); Cwd a thread's folder, for a compose project named by
// its directory. Each optional.
type Services struct {
	Match  func(command string) bool
	Docker *Docker
	KV     kvstore.Table
	Cwd    func(thread string) string
	Now    func() time.Time
	Proc   string

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
	project, up, down := heavy.ComposeProject(command, cwd)
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
func (s *Services) Extras(name func(thread string) string) []slots.Extra {
	if s == nil {
		return nil
	}
	p := s.pub.Load()
	if p == nil {
		return nil
	}
	var out []slots.Extra
	for th, vs := range *p {
		var r uint64
		for _, v := range vs {
			r += v.Reserve
		}
		if r > 0 {
			out = append(out, slots.Extra{Owner: th, Name: name(th), Need: slots.Need{Mem: r}, Service: true})
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
	now := s.now()
	if now.Sub(s.noticed[thread]) < serviceNoticeEvery {
		return ""
	}
	if s.noticed == nil {
		s.noticed = map[string]time.Time{}
	}
	s.noticed[thread] = now
	return fmt.Sprintf("[Fyr] The containers of compose project %s, which this thread started, hold %s outside this thread's own memory. "+
		"Fyr counts them as this thread's service: it reserves that memory before admitting heavy jobs and never pauses them. "+
		"Stop them (`docker compose -p %s down`) when this thread no longer needs them.", big.Project, slots.GB(float64(big.Containers)), big.Project)
}

func (s *Services) loadLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	s.peaks = map[string]servicePeak{}
	if s.KV == nil {
		return
	}
	if raw, _ := s.KV.GetKV(servicePeaksKey); raw != "" {
		json.Unmarshal([]byte(raw), &s.peaks)
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

// Tick reads each thread's services: from its cgroup with a tree, and the
// compose projects it started. The sampler's Tick calls it, outside every lock.
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

// read finds thread's service processes: the topmost process of each
// subtree whose command is a service, with the subtree's memory.
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
	under := func(p int) bool { // an ancestor in the cgroup is a service
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
		if !s.IsService(e.cmd) || under(p) {
			continue
		}
		v := Service{Thread: thread, Name: e.cmd, PID: p, Memory: sum(p, 0)}
		if len(v.Name) > serviceNameMax {
			v.Name = v.Name[:serviceNameMax]
		}
		cwd, _ := os.Readlink(filepath.Join(proc, strconv.Itoa(p), "cwd"))
		if project, _, _ := heavy.ComposeProject(e.cmd, cwd); project != "" {
			v.Project = project
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}
