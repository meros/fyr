package engine

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// #39: a script that starts a background child returns at its time limit,
// and the child does not outlive it.
func TestScriptActionLimitKillsBackgroundChild(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	out, exit, err := runScriptLive(ctx, dir, []string{"sh", "-c", "sleep 600 & echo $! > child.pid; wait"}, nil)
	took := time.Since(start)
	if err == nil {
		t.Fatalf("a script past its limit reported success: %q", out)
	}
	if exit == 0 {
		t.Errorf("exit = 0 after the limit")
	}
	if !strings.Contains(out, "stopped after the time limit") {
		t.Errorf("output does not say the limit stopped it: %q", out)
	}
	if took > 3*time.Second {
		t.Errorf("runScript returned after %s, want about the 300ms limit", took)
	}
	b, rerr := os.ReadFile(pidFile)
	if rerr != nil {
		t.Fatalf("the script did not record its child: %v", rerr)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid <= 0 {
		t.Fatalf("bad child pid %q", b)
	}
	if !gone(pid, 2*time.Second) {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("the background child %d is still running after the script returned", pid)
	}
}

// gone reports whether pid stops existing (or is only a zombie) within d.
func gone(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
		if st, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
			// The state follows the ")" that ends the command name.
			if i := strings.LastIndexByte(string(st), ')'); i >= 0 && strings.HasPrefix(string(st[i+1:]), " Z") {
				return true
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}
