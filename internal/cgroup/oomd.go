package cgroup

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// V0.oomd-guard (decision 0036 slice 0): on 18 Nov systemd-oomd, watching
// fyr.service, marked fyr.service/server (fyrd itself, its own pressure 0)
// for killing. In the split unit layout oomd honours user.oomd_omit on a
// pressure kill when the cgroup has the same owner as the monitored one, so
// fyrd marks server/. In the scope layout (V0.agents-unit) fyrd's unit is
// root-owned with ManagedOOMPreference=omit and oomd's rule is on the agents'
// slice, so the guard reads that slice's limit and stall instead.

// OomdNearMargin is how close (percentage points) the unit's memory stall
// may come to oomd's limit before the guard logs it.
const OomdNearMargin = 10.0

// UnitOomdLimit reads the unit's ManagedOOMMemoryPressureLimit in percent; 0
// when oomd has no pressure rule on it or systemctl cannot say. fyrd passes
// it as Options.OomdLimit.
func UnitOomdLimit(unit string) float64 {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "show", "--value",
		"-p", "ManagedOOMMemoryPressure", "-p", "ManagedOOMMemoryPressureLimit", unit).Output()
	if err != nil {
		return 0
	}
	return parseOomdLimit(string(out))
}

// parseOomdLimit reads systemctl's two lines: the mode, then the limit
// ("70%", "70.00%" or "0").
func parseOomdLimit(out string) float64 {
	lines := strings.Fields(out)
	if len(lines) < 2 || lines[0] != "kill" {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(lines[1], "%"), 64)
	if err != nil || v <= 0 || v > 100 {
		return 0
	}
	return v
}

// OomdLimit is oomd's pressure limit on the unit in percent; 0 for none or
// a nil Tree.
func (t *Tree) OomdLimit() float64 {
	if t == nil {
		return 0
	}
	return t.oomdLimit
}

// watchOomd reads oomd's limit on unit, whose cgroup is dir, and logs it;
// nothing without o.OomdLimit (tests).
func (t *Tree) watchOomd(o Options, unit, dir string) {
	t.oomdDir = dir
	if o.OomdLimit == nil {
		return
	}
	t.oomdLimit = o.OomdLimit(unit)
	if t.oomdLimit > 0 {
		o.Logf("cgroups: oomd kills under %s at %.0f%% memory pressure; the guard logs when it comes within %.0f points", unit, t.oomdLimit, OomdNearMargin)
	}
}

// unitStall is the memory.pressure full avg10 of the unit oomd watches for the
// agents, what oomd compares with its limit; ok false without it.
func (t *Tree) unitStall() (float64, bool) {
	dir := t.oomdDir
	if dir == "" {
		dir = t.root
	}
	p, ok := readPSI(filepath.Join(dir, "memory.pressure"))
	if !ok || !p.hasFull {
		return 0, false
	}
	return p.full, true
}

// noteOomd logs, at most once a minute, that the unit's stall is within
// OomdNearMargin of oomd's limit: oomd is about to kill, and the guard has not
// brought the stall down.
func (t *Tree) noteOomd(now time.Time, stall float64) {
	if t.oomdLimit <= 0 || stall < t.oomdLimit-OomdNearMargin {
		return
	}
	t.mu.Lock()
	quiet := now.Sub(t.oomdNoted) < time.Minute
	if !quiet {
		t.oomdNoted = now
	}
	t.mu.Unlock()
	if !quiet {
		t.logf("guard: near oomd's limit: unit_full=%.0f%% limit=%.0f%%: oomd kills under this unit after its duration", stall, t.oomdLimit)
	}
}
