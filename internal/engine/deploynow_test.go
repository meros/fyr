package engine

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
)

// SPEC 0.5 V0.83: host.deployNow is the owner's alone; it reaches the
// deployer and answers its refusal.
func TestDeployNowCommand(t *testing.T) {
	e, _, _ := bare(t)
	if r := e.Command(api.Cmd{CommandID: "n0", Action: core.KindDeployNow}); r.OK || r.Status != 409 {
		t.Fatalf("without a deployer: %+v", r)
	}
	calls := 0
	e.DeployNow = func() error { calls++; return nil }
	if r := e.Command(api.Cmd{CommandID: "n1", Action: core.KindDeployNow}); !r.OK {
		t.Fatalf("deployNow: %+v", r)
	}
	if calls != 1 {
		t.Fatalf("calls %d", calls)
	}
	e.DeployNow = func() error { return errors.New("No update is waiting for running threads") }
	if r := e.Command(api.Cmd{CommandID: "n2", Action: core.KindDeployNow}); r.OK || r.Status != 409 || !strings.Contains(r.Error, "No update") {
		t.Fatalf("refused: %+v", r)
	}
	id := bareThread(t, e)
	e.DeployNow = func() error { calls++; return nil }
	if r := e.submit(core.Input{Source: core.FromAgent, From: id, Kind: core.KindDeployNow, CmdID: "n3"}); r.OK {
		t.Fatalf("an agent forced a deploy: %+v", r)
	}
	if calls != 1 {
		t.Fatalf("an agent's command reached the deployer")
	}
}

// sends records the messages the engine hands to the agents.
type sends struct {
	mu  sync.Mutex
	got []string
}

func (s *sends) Do(th string, ef core.Effect) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ef.Kind == core.EffectSend {
		s.got = append(s.got, th+" "+ef.Text)
	}
	return nil
}

// SPEC 0.5 V0.83: after a forced restart a cut thread gets "continue" as the
// owner's message, once per command id; a thread settled or archived since
// gets nothing.
func TestContinueAfterForcedRestart(t *testing.T) {
	e, _, _ := bare(t)
	// V0.147: the agents are in place before any command; the settle's and
	// the archive's effects run on their own goroutines and read e.Agents.
	ag := &sends{}
	e.Agents = ag
	live, settled, archived := bareThread(t, e), bareThread(t, e), bareThread(t, e)
	if r := e.Command(api.Cmd{CommandID: "s1", Action: "thread.settle", ThreadID: settled}); !r.OK {
		t.Fatalf("settle: %+v", r)
	}
	if r := e.Command(api.Cmd{CommandID: "s2", Action: "thread.archive", ThreadID: archived}); !r.OK {
		t.Fatalf("archive: %+v", r)
	}
	count := func(id string) int {
		ag.mu.Lock()
		defer ag.mu.Unlock()
		n := 0
		for _, s := range ag.got {
			if s == id+" continue" {
				n++
			}
		}
		return n
	}
	if !e.Continue(live, "deploy-b1-continue-"+live) || !e.Continue(live, "deploy-b1-continue-"+live) {
		t.Fatal("the live thread did not take continue")
	}
	// Bounded: up to 5 s for the first send, then 100 ms in which a second
	// one would land.
	for end := time.Now().Add(5 * time.Second); count(live) == 0 && time.Now().Before(end); {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if n := count(live); n != 1 {
		t.Fatalf("live thread got %d continues", n)
	}
	if e.Continue(settled, "k2") || count(settled) != 0 {
		t.Fatal("a settled thread got continue")
	}
	if e.Continue(archived, "k3") || count(archived) != 0 {
		t.Fatal("an archived thread got continue")
	}
	if e.Continue("t_gone", "k4") {
		t.Fatal("an unknown thread got continue")
	}
}
