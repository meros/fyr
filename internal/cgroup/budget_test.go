// V0.agents-budget-share (#198): a budget in percent is a share of the machine, and fyrd says the budget it runs with.
package cgroup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBudgetShareResolvesAgainstTheMachine(t *testing.T) {
	o, l := fakeFS(t, "0::/"+unit+"\n")
	o.Proc = t.TempDir()
	write(t, filepath.Join(o.Proc, "meminfo"), "MemTotal:       31457280 kB\nMemAvailable:   1 kB\nSwapTotal:      35651584 kB\n")
	o.AgentsMemoryHigh, o.AgentsMemoryMax, o.AgentsMemorySwapMax = "55%", "60%", "50%"
	if Setup(o) == nil {
		t.Fatalf("Setup is off: %v", l.lines)
	}
	agents := filepath.Join(o.FS, unit, "agents")
	// 30 GiB RAM, 34 GiB swap.
	for f, want := range map[string]string{
		"memory.high":     "17716740096", // 55% of 30 GiB, page-aligned
		"memory.max":      "19327352832",
		"memory.swap.max": "18253611008",
	} {
		if got := read(t, filepath.Join(agents, f)); got != want {
			t.Errorf("%s = %s, want %s", f, got, want)
		}
	}
	line := strings.Join(l.lines, "\n")
	for _, want := range []string{"agents' budget:", "memory.high 16.5 GiB (55% of 30.0 GiB RAM)", "memory.swap.max 17.0 GiB (50% of 34.0 GiB swap)", "cpu.max 4.0 cores"} {
		if !strings.Contains(line, want) {
			t.Errorf("the log does not say %q: %s", want, line)
		}
	}
}

func TestBudgetShareWithoutMeminfoLeavesTheFileAndSaysWhy(t *testing.T) {
	o, l := fakeFS(t, "0::/"+unit+"\n")
	o.Proc = t.TempDir() // no meminfo
	o.AgentsMemoryHigh = "55%"
	if Setup(o) == nil {
		t.Fatalf("Setup is off: %v", l.lines)
	}
	if _, err := os.Stat(filepath.Join(o.FS, unit, "agents", "memory.high")); err == nil {
		t.Errorf("memory.high was written without a total")
	}
	if line := strings.Join(l.lines, "\n"); !strings.Contains(line, "budget not fully set: memory.high") {
		t.Errorf("the log does not say why: %s", line)
	}
}

func TestResolveShareKeepsBytesAndRejectsNonsense(t *testing.T) {
	if v, err := resolveShare("13G", 1<<30); v != "13G" || err != nil {
		t.Errorf("13G = %q, %v", v, err)
	}
	for _, bad := range []string{"x%", "120%", "-1%"} {
		if _, err := resolveShare(bad, 1<<30); err == nil {
			t.Errorf("%s resolved", bad)
		}
	}
}
