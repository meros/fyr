// V0.guard-wake (decision 0036 E5): a PSI trigger on the agents' memory.pressure; without a tree or a kernel that takes
// it, the caller is told and keeps the tick. V0.psi-trigger-slice: a refused open is tried again.
package cgroup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestPressureTriggerNeedsATree(t *testing.T) {
	if err := WatchAgentsPressure(context.Background(), nil, func() {}, nil); err == nil {
		t.Fatal("no error without a tree")
	}
}

func TestPressureTriggerOnTheHostsPSI(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("PSI triggers are Linux only")
	}
	if _, err := os.Stat("/proc/pressure/memory"); err != nil {
		t.Skip("no PSI")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The unprivileged form: a window that is a multiple of 2 s.
	if err := watchPressure(ctx, "/proc/pressure/memory", PressureTrigger, func() {}); err != nil {
		t.Skipf("the kernel refused the trigger here: %v", err)
	}
}

// V0.psi-trigger-slice: on the live host the slice's memory.pressure was
// root's and fyrd gave up for good. A refused open names the file and is
// tried again, so a later chown or a slice that appears after fyrd is picked
// up; an error that will not change (EINVAL: no trigger from this kernel) is
// not.
func TestPressureTriggerRetriesARefusedOpen(t *testing.T) {
	oldWatch, oldRetry := watchFile, PressureRetry
	defer func() { watchFile, PressureRetry = oldWatch, oldRetry }()
	PressureRetry = 5 * time.Millisecond
	var calls atomic.Int32
	watchFile = func(_ context.Context, file, trigger string, _ func()) error {
		if calls.Add(1) < 3 {
			return &fs.PathError{Op: "open", Path: file, Err: fs.ErrPermission}
		}
		return nil
	}
	// What unix.Open returns for a root:root 0644 file or a missing slice.
	for _, e := range []error{syscall.EACCES, syscall.EPERM, syscall.ENOENT} {
		if !PressureRetrying(fmt.Errorf("x: %w", e)) {
			t.Errorf("%v is not retried", e)
		}
	}
	tree := &Tree{root: "/sys/fs/cgroup/system.slice/fyr.service", agents: "/sys/fs/cgroup/fyr.slice/fyr-agents.slice"}
	armed := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := WatchAgentsPressure(ctx, tree, func() {}, func() { close(armed) })
	if err == nil || !PressureRetrying(err) {
		t.Fatalf("err = %v, want a refused open", err)
	}
	if want := filepath.Join(tree.agents, "memory.pressure"); !strings.Contains(err.Error(), want) {
		t.Errorf("err = %v, does not name %s", err, want)
	}
	select {
	case <-armed:
	case <-time.After(2 * time.Second):
		t.Fatalf("not armed after %d tries", calls.Load())
	}

	// A kernel that takes no trigger: one try, no retry.
	calls.Store(0)
	watchFile = func(context.Context, string, string, func()) error {
		calls.Add(1)
		return fmt.Errorf("write trigger: %w", errors.New("invalid argument"))
	}
	if err := WatchAgentsPressure(ctx, tree, func() {}, func() { t.Error("armed") }); err == nil || PressureRetrying(err) {
		t.Fatalf("err = %v, want a final error", err)
	}
	time.Sleep(30 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Errorf("%d tries, want 1", n)
	}
}

// V0.psi-stale-fire: 20 Nov, a trigger set on the agents' slice fired at
// +0.3 s, +2.3 s and +4.2 s with 13, 17 and 0 ms of full stall since it was
// set; 200 ms is what a fire stands for.
func TestPSIStaleFireIsDroppedARealOneIsNot(t *testing.T) {
	f := filepath.Join(t.TempDir(), "memory.pressure")
	at := func(total int) {
		write(t, f, fmt.Sprintf("some avg10=0.00 avg60=0.00 avg300=0.00 total=%d\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=%d\n", total*2, total))
	}
	at(5_000_000)
	g := newStallGate(f, PressureTrigger)
	for _, c := range []struct {
		total int
		real  bool
	}{
		{5_013_000, false}, // +13 ms: the first fire after the trigger was set
		{5_030_000, false}, // +17 ms
		{5_260_000, true},  // +230 ms: a stall
		{5_300_000, false}, // +40 ms since that fire
		{5_500_000, true},  // +200 ms: the trigger's own threshold counts
	} {
		at(c.total)
		if got, _ := g.real(); got != c.real {
			t.Fatalf("total=%d: real=%v, want %v", c.total, got, c.real)
		}
	}
	// Without a reading every fire counts, as before.
	if got, _ := newStallGate(filepath.Join(t.TempDir(), "none"), PressureTrigger).real(); !got {
		t.Fatal("no memory.pressure: a fire counts")
	}
}
