package main

import (
	"context"
	"log"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/cgroup"
	"github.com/meros/fyr/internal/engine"
)

// leftoverStart: the first look waits for the runner's CLIs to be adopted
// after a restart, so an adopted CLI is never taken for a leftover.
const leftoverStart = 2 * time.Minute

// runLeftovers starts V0.176: the leftover tracker (every LeftoverTick, at a
// run's end and at a settle) and the temp sweep (scratch and layout profiles
// every 10 min with the worktree list).
//
// V0.worktree-sweep: the sweep also removes the worktrees that may go (unless
// worktrees is "list"), and a thread that starts in one makes it again.
func runLeftovers(ctx context.Context, tree *cgroup.Tree, sampler *cgroup.Sampler, eng *engine.Engine, mgr *agent.Manager, h *host, worktrees string) {
	lt := &cgroup.Leftovers{Tree: tree, CLIs: mgr.LeftoverCLIs, KV: h.st, Logf: log.Printf,
		Thread: func(name string) cgroup.ThreadState {
			k, a, r := eng.LeftoverThread(name)
			return cgroup.ThreadState{Known: k, Asleep: a, Running: r}
		},
		OnEnd: func(thread string, ended []cgroup.Leftover, why string) {
			items := make([]engine.LeftoverItem, len(ended))
			for i, x := range ended {
				items[i] = engine.LeftoverItem{Name: x.Name, RSS: x.RSS}
			}
			eng.LeftoversEnded(thread, items, why)
		}}
	var cmdlines func() []string
	if tree == nil && runtime.GOOS == "darwin" {
		// No cgroups and no /proc: the process table from ps (section 5).
		lt.PS = func() (string, error) { return cgroup.PS(ctx, "-A", "-o", "pid=,ppid=,rss=,etime=,args=") }
		cmdlines = func() []string {
			out, _ := cgroup.PS(ctx, "-A", "-o", "args=")
			return splitLines(out)
		}
	} else {
		cmdlines = func() []string { return cgroup.Cmdlines("") }
	}
	tp := &cgroup.Temps{Logf: log.Printf, Cmdlines: cmdlines, Threads: func() []cgroup.TempThread {
		clis := mgr.LeftoverCLIs()
		var out []cgroup.TempThread
		for _, t := range eng.TempThreads() {
			_, live := clis[t.ID]
			out = append(out, cgroup.TempThread{ID: t.ID, SessionID: t.SessionID, Cwd: t.Cwd, Asleep: t.Asleep, LiveCLI: live, Branches: t.Branches, Done: t.Done})
		}
		return out
	},
		Repos: eng.ProjectFolders, Cwds: func() []string { return cgroup.Cwds("") }, Auto: worktrees != "list",
		KV: h.st, OnRemoved: eng.WorktreeRemoved}
	mgr.SetRestoreDir(tp.Restore)
	sampler.Leftovers, sampler.Temps = lt, tp
	var started atomic.Bool
	eng.LeftoverDeps = engine.LeftoverDeps{
		LeftoverEnd: lt.End, LeftoverKeep: lt.Keep, TempRemove: tp.Remove,
		ThreadPorts: func(thread string) []int {
			return lt.ThreadPorts(thread, func() (string, error) { return cgroup.LsofListen(ctx) })
		},
		LeftoverDeclare: func(thread string, pid int, match string, d time.Duration, why string) error {
			return lt.Declare(thread, pid, match, d, why, "agent")
		},
		LeftoverSweep: func() {
			if started.Load() {
				lt.Tick()
				tp.Sweep(false)
			}
		},
	}
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(leftoverStart):
		}
		started.Store(true)
		t := time.NewTicker(cgroup.LeftoverTick)
		defer t.Stop()
		n := 0
		for {
			lt.Tick()
			tp.Sweep(false)
			if n%10 == 0 {
				go tp.SweepWorktrees() // git for every worktree: off the minute's loop
			}
			n++
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
