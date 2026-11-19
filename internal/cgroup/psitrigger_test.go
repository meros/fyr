// V0.guard-wake (decision 0036 E5): a PSI trigger on the agents' memory.pressure; without a tree or a kernel that takes
// it, the caller is told and keeps the tick.
package cgroup

import (
	"context"
	"os"
	"runtime"
	"testing"
)

func TestPressureTriggerNeedsATree(t *testing.T) {
	if err := WatchAgentsPressure(context.Background(), nil, func() {}); err == nil {
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
