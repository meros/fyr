// V0.services-not-jobs (decision 0036 (D)): a thread's services are found in its cgroup and among the compose
// projects it started; their memory is reserved from a decaying peak, left out of the undeclared work, and never
// a root of a freeze. The cgroupfs, /proc and docker are fakes.
package cgroup

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
	"github.com/meros/fyr/internal/heavy"
	"github.com/meros/fyr/internal/slots"
)

type svcKV map[string]string

func (m svcKV) GetKV(k string) (string, error) { return m[k], nil }
func (m svcKV) SetKV(k, v string) error        { m[k] = v; return nil }

// The 19 Nov case: a process-compose stack and a compose client beside an e2e run in one thread.
func svcThread(t *testing.T) (*Tree, string, string) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	proc := t.TempDir()
	write(t, filepath.Join(proc, "stat"), fmt.Sprintf("cpu 1 2 3\nbtime %d\n", boot.Unix()))
	tick := func(s float64) int { return int(s * 100) }
	snap := "/home/u/.claude/shell-snapshots/snapshot-zsh-1.sh"
	fakeProc2(t, proc, 10, 1, tick(100), 100, "claude", "--output-format", "stream-json")
	fakeProc2(t, proc, 20, 10, tick(1000), 10, "/bin/zsh", "-c", "source "+snap+" && eval 'process-compose --config process-compose.yaml --tui=false'")
	fakeProc2(t, proc, 21, 20, tick(1001), 1000, "process-compose", "--config", "process-compose.yaml", "--tui=false")
	fakeProc2(t, proc, 22, 21, tick(1002), 900000, "postgres") // about 3.4 GB
	fakeProc2(t, proc, 30, 10, tick(1100), 10, "/bin/zsh", "-c", "source "+snap+" && eval 'docker compose -f devdocker/docker-compose.yaml --project-name acme-dev up'")
	fakeProc2(t, proc, 31, 30, tick(1101), 5000, "docker", "compose", "-f", "devdocker/docker-compose.yaml", "--project-name", "acme-dev", "up")
	fakeProc2(t, proc, 40, 10, tick(7200), 10, "/bin/zsh", "-c", "source "+snap+" && eval 'yarn e2e'")
	fakeProc2(t, proc, 41, 40, tick(7201), 500000, "node", "e2e.js")
	write(t, filepath.Join(td, "cgroup.procs"), "10\n20\n21\n22\n30\n31\n40\n41\n")
	return tr, td, proc
}

func TestSvc1AThreadsServicesAndTheirContainers(t *testing.T) {
	tr, _, proc := svcThread(t)
	fs := t.TempDir()
	write(t, filepath.Join(fs, "system.slice", "docker-aaa.scope", "memory.current"), "1073741824\n")
	write(t, filepath.Join(fs, "system.slice", "docker-bbb.scope", "memory.current"), "1073741824\n")
	asked := 0
	now := boot.Add(3 * time.Hour)
	d := &Docker{FS: fs, Now: func() time.Time { return now }, PS: func() (string, error) {
		asked++
		return "aaa\tacme-dev\nbbb\tacme-dev\nccc\tother\n", nil
	}}
	book := &heavy.Services{}
	s := &Services{Match: book.Match, Docker: d, Proc: proc, Now: func() time.Time { return now }}
	s.Tick(tr, []string{"t_a"})
	got := s.Of("t_a")
	if len(got) != 2 || got[0].PID != 21 || got[1].PID != 31 {
		t.Fatalf("services %+v", got)
	}
	page := uint64(pageSize())
	if got[0].Memory != 901000*page || got[0].Project != "" {
		t.Fatalf("process-compose %+v", got[0])
	}
	if got[1].Project != "acme-dev" || got[1].Containers != 2<<30 || got[1].Reserve != 5000*page+2<<30 {
		t.Fatalf("compose %+v", got[1])
	}
	if s.InCgroup("t_a") != 906000*page {
		t.Fatalf("in cgroup %d", s.InCgroup("t_a"))
	}
	// Docker is asked at most every 10 s.
	s.Tick(tr, []string{"t_a"})
	if asked != 1 {
		t.Fatalf("docker asked %d times", asked)
	}
	// The reservation is the learned peak: a service that shrank keeps it, decaying with a 48 h half-life.
	write(t, filepath.Join(proc, "22", "statm"), "100 1000 0 0 0 0 0\n")
	now = now.Add(48 * time.Hour)
	s.Tick(tr, []string{"t_a"})
	if r := s.Of("t_a")[0].Reserve; r < 450000*page || r > 452000*page {
		t.Fatalf("reserve after 48 h %d, want about half of %d", r, 901000*page)
	}
	ex := s.Extras(func(string) string { return "Shop" })
	if len(ex) != 1 || !ex[0].Service || ex[0].Name != "Shop" || ex[0].Need.Mem == 0 {
		t.Fatalf("extras %+v", ex)
	}
}

func TestSvc2TheGuardNeverTakesAServiceAsARootOrAnElder(t *testing.T) {
	tr, _, proc := svcThread(t)
	s := &Services{Match: (&heavy.Services{}).Match}
	all := ReadRootsWith(tr, proc, "t_a", boot.Add(900*time.Second), Elders{CLI: 10, Min: 1})
	if len(all) < 3 {
		t.Fatalf("without the rule %+v", all)
	}
	roots := ReadRootsWith(tr, proc, "t_a", boot.Add(900*time.Second), Elders{CLI: 10, Min: 1, Service: s.IsService})
	if len(roots) != 1 || roots[0].PID != 40 {
		t.Fatalf("roots %+v", roots)
	}
	if s.IsService("/bin/zsh -c source /h/.claude/shell-snapshots/snapshot-zsh-1.sh && eval 'npm run dev'") || !s.IsService("npm run dev") {
		t.Fatal("the CLI's shell is not the service; its child is")
	}
}

func TestSvc3ComposeUpDetachedCountsUntilItsContainersAreGone(t *testing.T) {
	fs := t.TempDir()
	write(t, filepath.Join(fs, "docker", "aaa", "memory.current"), "3G")
	now := boot
	ps := "aaa\tacme-dev\n"
	d := &Docker{FS: fs, Now: func() time.Time { return now }, PS: func() (string, error) { return ps, nil }}
	s := &Services{Docker: d, Now: func() time.Time { return now }, Cwd: func(string) string { return "/w/repo" }}
	s.Bash("t_a", "docker compose -f devdocker/docker-compose.yaml --project-name acme-dev up -d")
	s.Bash("t_a", "docker compose ps")
	s.Tick(nil, nil)
	got := s.Of("t_a")
	if len(got) != 1 || got[0].Name != "compose acme-dev" || got[0].Containers != 3<<30 {
		t.Fatalf("%+v", got)
	}
	// Told once an hour, when the containers hold a share of the agents' memory.
	c := Capacity{CPU: 8, Memory: 12 << 30}
	n := s.Notice("t_a", c)
	if !strings.Contains(n, "compose project acme-dev") || !strings.Contains(n, "docker compose -p acme-dev down") {
		t.Fatalf("notice %q", n)
	}
	if s.Notice("t_a", c) != "" || s.Notice("t_b", c) != "" {
		t.Fatal("told again within the hour, or another thread")
	}
	// down ends it; containers gone end it after a grace.
	s.Bash("t_a", "docker compose -p acme-dev down")
	now = now.Add(time.Minute)
	s.Tick(nil, nil)
	if len(s.Of("t_a")) != 0 {
		t.Fatal("still counted after down")
	}
	s.Bash("t_a", "docker compose -p acme-dev up -d")
	ps = ""
	now = now.Add(time.Minute)
	s.Tick(nil, nil)
	now = now.Add(3 * time.Minute)
	s.Tick(nil, nil)
	if len(s.Of("t_a")) != 0 || len(s.compose["t_a"]) != 0 {
		t.Fatal("a project with no container kept")
	}
}

func TestSvc4ThePeaksOutliveARestart(t *testing.T) {
	kv := svcKV{}
	now := boot
	s := &Services{KV: kv, Now: func() time.Time { return now }}
	s.mu.Lock()
	s.loadLocked()
	s.learn("compose x", 4<<30, now)
	s.dirty = true
	s.mu.Unlock()
	s.Tick(nil, nil)
	if !strings.Contains(kv[servicePeaksKey], "compose x") {
		t.Fatalf("kv %v", kv)
	}
	s2 := &Services{KV: kv}
	s2.mu.Lock()
	s2.loadLocked()
	got := s2.learn("compose x", 1<<30, now)
	s2.mu.Unlock()
	if got != 4<<30 {
		t.Fatalf("%d", got)
	}
}

func TestSvc5TheWaitReasonNamesAService(t *testing.T) {
	sig := resource.Signals{Provider: "cgroups", CPU: 8, Memory: 10 << 30}
	ok, why, _ := slots.DecideWith(resource.DefaultPolicy, sig, resource.Factors{CPU: 1, Mem: 1}, 4, []resource.Need{{CPU: 1, Mem: 1 << 30}},
		[]resource.Extra{{Owner: "t_b", Name: "Shop", Need: resource.Need{Mem: 8 << 30}, Service: true}}, "t_a", resource.Need{CPU: 1, Mem: 2 << 30})
	if ok || !strings.Contains(why, "Shop's services hold") {
		t.Fatalf("%v %q", ok, why)
	}
}
