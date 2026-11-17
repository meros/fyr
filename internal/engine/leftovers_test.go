// V0.175: the line Fyr leaves when it ends a thread's leftovers, the owner's
// End, Keep and Remove, keep_process, and the look at a run's end and a settle.
package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
)

func TestV175TheEndedLineIsOnTheThreadAndANoticeNotAWake(t *testing.T) {
	e, _, _ := bare(t)
	a := bareThread(t, e)
	e.LeftoversEnded(a, []LeftoverItem{{Name: "chromium", RSS: 2 << 30}, {Name: "http.server", RSS: 20 << 20}}, "the thread settled")
	ns := notices(e, a, "leftovers")
	if len(ns) != 1 || ns[0].Urgency != core.Next {
		t.Fatalf("%+v", ns)
	}
	for _, want := range []string{"Fyr ended 2 leftover processes (the thread settled): chromium (2.0 GB), http.server", "keep_process"} {
		if !strings.Contains(ns[0].What, want) {
			t.Errorf("missing %q in %q", want, ns[0].What)
		}
	}
	// A cgroup that is no thread's leaves no line (and no error).
	e.LeftoversEnded("t_gone", []LeftoverItem{{Name: "sleep"}}, "in the cgroup of no thread")
	if got := LeftoverLine([]LeftoverItem{{Name: "kubectl port-forward"}}, "idle 30 min after the run ended"); got != "Fyr ended a leftover process (idle 30 min after the run ended): kubectl port-forward" {
		t.Fatal(got)
	}
}

func TestV175TheOwnersCommandsReachTheHostAndAreTheOwnersOnly(t *testing.T) {
	e, _, _ := bare(t)
	var ended, kept, removed []string
	var keptFor time.Duration
	e.LeftoverEnd = func(id string) error { ended = append(ended, id); return nil }
	e.LeftoverKeep = func(id string, d time.Duration) error { kept, keptFor = append(kept, id), d; return nil }
	e.TempRemove = func(p string) error { removed = append(removed, p); return nil }
	for _, k := range []string{core.KindLeftoverEnd, core.KindLeftoverKeep, core.KindTempRemove} {
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: k, Args: map[string]any{"id": "1@2", "path": "/x"}}); err == nil {
			t.Errorf("%s accepted from an observer", k)
		}
	}
	if r := e.Command(api.Cmd{CommandID: "c1", Action: core.KindLeftoverEnd, Args: map[string]any{"id": "123@456"}}); !r.OK || len(ended) != 1 || ended[0] != "123@456" {
		t.Fatalf("%+v %v", r, ended)
	}
	if r := e.Command(api.Cmd{CommandID: "c2", Action: core.KindLeftoverKeep, Args: map[string]any{"id": "123@456"}}); !r.OK || keptFor != 24*time.Hour {
		t.Fatalf("%+v %v", r, keptFor)
	}
	if r := e.Command(api.Cmd{CommandID: "c3", Action: core.KindLeftoverKeep, Args: map[string]any{"id": "1@1", "hours": 500.0}}); r.OK {
		t.Fatal("a keep over a week")
	}
	if r := e.Command(api.Cmd{CommandID: "c4", Action: core.KindTempRemove, Args: map[string]any{"path": "relative"}}); r.OK {
		t.Fatal("a relative path")
	}
	if r := e.Command(api.Cmd{CommandID: "c5", Action: core.KindTempRemove, Args: map[string]any{"path": "/tmp/x"}}); !r.OK || removed[0] != "/tmp/x" {
		t.Fatalf("%+v", r)
	}
	_ = kept
}

func TestV175ARunsEndAndASettleLookAtOnce(t *testing.T) {
	e, _, now := bare(t)
	a := bareThread(t, e)
	looked := make(chan struct{}, 4)
	e.LeftoverSweep = func() { looked <- struct{}{} }
	running(e, a)
	endRun(e, a, *now)
	wait := func(what string) {
		select {
		case <-looked:
		case <-time.After(2 * time.Second):
			t.Fatalf("no look at %s", what)
		}
	}
	wait("the run's end")
	coretest.Patch(e, a, func(th *core.Thread, at time.Time) []core.Event { th.Settle.At = &at; return nil })
	wait("the settle")
	if k, asleep, run := e.LeftoverThread(a); !k || !asleep || run {
		t.Fatalf("state %v %v %v", k, asleep, run)
	}
	if k, _, _ := e.LeftoverThread("t_none"); k {
		t.Fatal("an unknown thread is known")
	}
}

func TestV175KeepProcessDeclaresForTheCallingThread(t *testing.T) {
	e, _, _ := bare(t)
	var got struct {
		th, match, why string
		pid            int
		d              time.Duration
	}
	e.LeftoverDeclare = func(th string, pid int, match string, d time.Duration, why string) error {
		got.th, got.pid, got.match, got.d, got.why = th, pid, match, d, why
		return nil
	}
	out, isErr := callAs(t, e, "t_a", "keep_process", map[string]any{"pid": 4242, "hours": 99, "why": "dev server"})
	if isErr || !strings.Contains(out, "Kept for 24 h") {
		t.Fatalf("%q %v", out, isErr)
	}
	if got.th != "t_a" || got.pid != 4242 || got.d != 24*time.Hour || got.why != "dev server" {
		t.Fatalf("%+v", got)
	}
}
