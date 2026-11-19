// V0.oomd-guard (decision 0036 slice 0): fyrd marks server/ for oomd to skip, reads oomd's limit on its unit, shows
// the unit's own stall in /api/load and logs when the stall comes near the limit.
package cgroup

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// V0.oomd-limit-parse: systemd 262 prints the limit as its raw fixed-point value (2^32 = 100 %). The first input is
// verbatim `systemctl show --value -p ManagedOOMMemoryPressure -p ManagedOOMMemoryPressureLimit fyr-agents.slice` on
// the host (85 %), the second that of fyr.service there.
func TestOomdLimitParsesSystemctl(t *testing.T) {
	for in, want := range map[string]float64{
		"kill\n3650722201\n": 85,
		"auto\n0\n":          0,
		"kill\n4294967296\n": 100,
		"kill\n2147483648\n": 50,
		"kill\n70%\n":        70, // older systemd: a percentage
		"kill\n70.00%\n":     70,
		"auto\n70%\n":        0, // no pressure rule
		"kill\n0\n":          0, // oomd's DefaultMemoryPressureLimit: unknown here
		"kill\nfoo\n":        0,
		"":                   0,
	} {
		if got := parseOomdLimit(in); got != want {
			t.Errorf("parseOomdLimit(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestOomdGuardReadsTheUnitAndWarnsNearTheLimit(t *testing.T) {
	o, l := fakeFS(t, "0::/"+unit+"\n")
	var asked string
	o.OomdLimit = func(u string) float64 { asked = u; return 70 }
	tr := Setup(o)
	if tr == nil {
		t.Fatalf("off: %v", l.lines)
	}
	if asked != filepath.Base(unit) {
		t.Fatalf("asked oomd's limit of %q", asked)
	}
	joined := strings.Join(l.lines, "\n")
	if !strings.Contains(joined, "oomd kills under fyr.service at 70% memory pressure") {
		t.Fatalf("the omit is not logged: %s", joined)
	}
	root := filepath.Join(o.FS, unit)
	proc := t.TempDir()
	write(t, filepath.Join(proc, "meminfo"), "MemTotal:       32000000 kB\nMemAvailable:   20000000 kB\n")
	now := time.Date(2026, 11, 18, 15, 29, 0, 0, time.UTC)
	s := &Sampler{Tree: tr, Proc: proc, Cores: 4, Now: func() time.Time { return now }}

	write(t, filepath.Join(root, "memory.pressure"), memPSI(40))
	lr := s.Get()
	if lr.UnitMemoryStall == nil || *lr.UnitMemoryStall != 40 || lr.OomdLimit != 70 {
		t.Fatalf("the unit's stall %v and oomd's limit %v", lr.UnitMemoryStall, lr.OomdLimit)
	}
	if n := countNear(l.lines); n != 0 {
		t.Fatalf("warned at 40 %% of 70 %%: %v", l.lines)
	}
	now = now.Add(MinGap)
	write(t, filepath.Join(root, "memory.pressure"), memPSI(62))
	s.Get()
	now = now.Add(MinGap)
	s.Get() // within the minute: one line
	if n := countNear(l.lines); n != 1 {
		t.Fatalf("near-limit lines %d: %v", n, l.lines)
	}
	if !strings.Contains(strings.Join(l.lines, "\n"), "guard: near oomd's limit: unit_full=62% limit=70%") {
		t.Fatalf("line: %v", l.lines)
	}
	now = now.Add(time.Minute)
	s.Get()
	if n := countNear(l.lines); n != 2 {
		t.Fatalf("near-limit lines after a minute %d", n)
	}
}

func TestOomdGuardWithoutARule(t *testing.T) {
	o, l := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o) // OomdLimit nil: tests never ask systemctl
	root := filepath.Join(o.FS, unit)
	write(t, filepath.Join(root, "memory.pressure"), memPSI(95))
	proc := t.TempDir()
	write(t, filepath.Join(proc, "meminfo"), "MemTotal:       32000000 kB\nMemAvailable:   20000000 kB\n")
	lr := (&Sampler{Tree: tr, Proc: proc, Cores: 4}).Get()
	if lr.OomdLimit != 0 || countNear(l.lines) != 0 {
		t.Fatalf("limit %v lines %v", lr.OomdLimit, l.lines)
	}
}

func countNear(lines []string) int {
	n := 0
	for _, s := range lines {
		if strings.Contains(s, "near oomd's limit") {
			n++
		}
	}
	return n
}

// In the scope layout (V0.agents-unit) fyrd's unit is root-owned with ManagedOOMPreference=omit and oomd's rule is on
// the agents' slice: the guard reads that slice's limit and stall, and does not mark server/.
func TestOomdGuardReadsTheAgentsSliceInTheScopeLayout(t *testing.T) {
	o, l, _ := scopeFS(t, false)
	var asked string
	o.OomdLimit = func(u string) float64 { asked = u; return 80 }
	tr := Setup(o)
	if tr == nil || tr.Layout() != LayoutScope {
		t.Fatalf("layout: %v", l.lines)
	}
	if asked != "fyr-agents.slice" {
		t.Fatalf("asked oomd's limit of %q", asked)
	}
	if strings.Contains(strings.Join(l.lines, "\n"), "oomd_omit") {
		t.Fatalf("server/ marked in the scope layout: %v", l.lines)
	}
	write(t, filepath.Join(o.FS, slice, "memory.pressure"), memPSI(75))
	proc := t.TempDir()
	write(t, filepath.Join(proc, "meminfo"), "MemTotal:       32000000 kB\nMemAvailable:   20000000 kB\n")
	lr := (&Sampler{Tree: tr, Proc: proc, Cores: 4}).Get()
	if lr.UnitMemoryStall == nil || *lr.UnitMemoryStall != 75 || lr.OomdLimit != 80 || countNear(l.lines) != 1 {
		t.Fatalf("stall %v limit %v lines %v", lr.UnitMemoryStall, lr.OomdLimit, l.lines)
	}
}
