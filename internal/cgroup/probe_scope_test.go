// V0.probe-scope: in the scope layout the slowdown probe runs in a scope of
// its own under the agents' slice (the slice itself is root's), a probe that
// cannot start falls back to the burst in fyrd, and the probe's scope is no
// thread.
package cgroup

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestProbeScopeStartsThroughTheScopePrefix(t *testing.T) {
	o, l, _ := scopeFS(t, false)
	tr := Setup(o)
	if tr == nil || tr.Layout() != LayoutScope {
		t.Fatalf("layout %v: %v", tr, l.lines)
	}
	start := ProbeCommand(tr, "/run/fyr/fyrd")
	if start == nil {
		t.Fatal("no probe command")
	}
	cmd, done, err := start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if cmd.Path != o.SystemdRun {
		t.Fatalf("the probe starts %s, not through systemd-run: %q", cmd.Path, cmd.Args)
	}
	i := slices.IndexFunc(cmd.Args, func(a string) bool { return strings.HasPrefix(a, "--unit=") })
	if i < 0 {
		t.Fatalf("no unit: %q", cmd.Args)
	}
	unit := strings.TrimPrefix(cmd.Args[i], "--unit=")
	if !strings.HasPrefix(unit, "fyr-agent-probe-") || !strings.HasSuffix(unit, ".scope") {
		t.Fatalf("unit %q is not one the polkit rule allows as a probe", unit)
	}
	for _, p := range []string{"--slice=fyr-agents.slice", "--property=CPUWeight=" + threadWeight} {
		if !slices.Contains(cmd.Args, p) {
			t.Errorf("args lack %s: %q", p, cmd.Args)
		}
	}
	if tail := cmd.Args[len(cmd.Args)-2:]; !slices.Equal(tail, []string{"/run/fyr/fyrd", "loadprobe"}) {
		t.Fatalf("args end %q", tail)
	}
	if pathExists(filepath.Join(tr.AgentsDir(), "probe")) {
		t.Fatal("the probe made a cgroup in the slice itself")
	}
	// The next start is another unit: the last one may still be collected.
	cmd2, done2, err := start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer done2()
	if slices.Equal(cmd.Args, cmd2.Args) {
		t.Fatalf("two starts share a unit: %q", cmd2.Args)
	}
}

func TestProbeScopeIsNoThread(t *testing.T) {
	o, l, _ := scopeFS(t, false)
	tr := Setup(o)
	if tr == nil || tr.Layout() != LayoutScope {
		t.Fatalf("layout %v: %v", tr, l.lines)
	}
	for _, u := range []string{"fyr-agent-probe-42.scope", "fyr-agent-probe-42-3.scope", "fyr-agent-t_a.scope"} {
		if err := os.MkdirAll(filepath.Join(tr.AgentsDir(), u), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var names []string
	for _, c := range tr.threadCgroups() {
		names = append(names, c.Name)
	}
	if !slices.Equal(names, []string{"t_a"}) {
		t.Fatalf("threads %q", names)
	}
	if n, _, ok := scopeThread("fyr-agent-probe-42-3.scope"); ok {
		t.Fatalf("the probe scope parses as thread %q", n)
	}
}

func TestProbeStartFailureFallsBackToTheBurst(t *testing.T) {
	if !CanBurst() {
		t.Skip("no CPU clock per thread")
	}
	for name, start := range map[string]func(context.Context) (*exec.Cmd, func(), error){
		"start error": func(context.Context) (*exec.Cmd, func(), error) {
			return nil, nil, errors.New("mkdir: permission denied")
		},
		"no output": func(context.Context) (*exec.Cmd, func(), error) {
			return exec.Command("true"), func() {}, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			var s Slowdown
			ctx, cancel := context.WithCancel(context.Background())
			ended := make(chan struct{})
			go func() { s.RunProbe(ctx, start, func() *float64 { return nil }); close(ended) }()
			defer func() { cancel(); <-ended }()
			deadline := time.Now().Add(5 * time.Second)
			for s.Value(time.Now(), nil) == nil {
				if time.Now().After(deadline) {
					t.Fatal("no slowdown after a failed probe start")
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
	}
}
