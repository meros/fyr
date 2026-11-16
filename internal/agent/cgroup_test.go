// SPEC 0.5 V0.80: a thread's CLI starts in the thread's cgroup, and the
// cgroup is given back when the process ends.
package agent

import (
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/provider"
)

type fakeCgroups struct {
	mu   sync.Mutex
	refs map[string]int
}

func (f *fakeCgroups) Thread(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refs[id]++
	return "/cg/agents/" + id
}

func (f *fakeCgroups) Release(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refs[id]--
}

func (f *fakeCgroups) held(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refs[id]
}

// launchAdapter records the cgroup each start asked for.
type launchAdapter struct {
	*stubAdapter
	mu   sync.Mutex
	dirs []string
}

func (a *launchAdapter) Start(l cli.Launch, h cli.Handlers) (cli.Session, error) {
	a.mu.Lock()
	a.dirs = append(a.dirs, l.Cgroup)
	a.mu.Unlock()
	return a.stubAdapter.Start(l, h)
}

func TestV080ThreadProcessStartsInItsCgroup(t *testing.T) {
	a := &launchAdapter{stubAdapter: &stubAdapter{}}
	cg := &fakeCgroups{refs: map[string]int{}}
	m := New(stubHost{cwd: t.TempDir()}, Config{
		Profile:  func(id string) provider.Profile { return provider.Profile{ID: id, CLI: "stub"} },
		Adapters: map[string]cli.Adapter{"stub": a},
		Prompt:   "-",
		Cgroups:  cg,
	})
	t.Cleanup(m.StopAll)
	if _, err := m.ensure("t_1"); err != nil {
		t.Fatal(err)
	}
	if len(a.dirs) != 1 || a.dirs[0] != "/cg/agents/t_1" || cg.held("t_1") != 1 {
		t.Fatalf("dirs %v held %d", a.dirs, cg.held("t_1"))
	}
	m.Stop("t_1")
	if !waitFor(2*time.Second, func() bool { return cg.held("t_1") == 0 }) {
		t.Fatalf("still held %d after the process ended", cg.held("t_1"))
	}
}

func TestV080WithoutCgroupsTheLaunchHasNone(t *testing.T) {
	a := &launchAdapter{stubAdapter: &stubAdapter{}}
	m := New(stubHost{cwd: t.TempDir()}, Config{
		Profile:  func(id string) provider.Profile { return provider.Profile{ID: id, CLI: "stub"} },
		Adapters: map[string]cli.Adapter{"stub": a},
		Prompt:   "-",
	})
	t.Cleanup(m.StopAll)
	if _, err := m.ensure("t_1"); err != nil {
		t.Fatal(err)
	}
	if len(a.dirs) != 1 || a.dirs[0] != "" {
		t.Fatalf("dirs %v", a.dirs)
	}
}
