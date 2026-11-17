package cgroup

// Decision 0029 section 0 (#185): what a host may give its agents (its
// capacity C) and the signals the slots read, each as a share or a level so
// the same rules hold on any host. A provider reads them: "cgroups" (a
// delegated cgroup v2 tree), "proc" (Linux /proc without the tree) or none
// (no /proc: macOS for now, which keeps the fixed count).

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// MemoryShare is the share of the RAM agents may use without a cgroup memory
// limit.
const MemoryShare = 0.8

// The memory stall levels from PSI memory full avg10, in percent.
const (
	StallElevated = 5.0
	StallCritical = 10.0
)

// Capacity is what the host's agents may use: cores and bytes.
type Capacity struct {
	CPU    float64 `json:"cpu"`
	Memory uint64  `json:"memory"`
}

// psi is one pressure file: the "some" and "full" lines' avg10 (percent)
// and total (µs stalled).
type psi struct {
	some, full           float64
	someTotal, fullTotal uint64
	hasFull              bool
}

func readPSI(path string) (psi, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return psi{}, false
	}
	var p psi
	ok := false
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		var avg float64
		var total uint64
		for _, w := range f[1:] {
			if v, found := strings.CutPrefix(w, "avg10="); found {
				avg, _ = strconv.ParseFloat(v, 64)
			} else if v, found := strings.CutPrefix(w, "total="); found {
				total, _ = strconv.ParseUint(v, 10, 64)
			}
		}
		switch f[0] {
		case "some":
			p.some, p.someTotal, ok = avg, total, true
		case "full":
			p.full, p.fullTotal, p.hasFull = avg, total, true
		}
	}
	return p, ok
}

// meminfo reads /proc/meminfo's MemTotal and MemAvailable in bytes.
func meminfo(proc string) (total, avail uint64, ok bool) {
	f, err := os.Open(filepath.Join(proc, "meminfo"))
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), ":")
		n, _ := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(v), " kB"), 10, 64)
		switch k {
		case "MemTotal":
			total = n << 10
		case "MemAvailable":
			avail = n << 10
		}
	}
	return total, avail, total > 0
}

// cpuCount counts the CPUs of a cpuset list ("0-3,6"); 0 when empty.
func cpuCount(list string) int {
	n := 0
	for _, part := range strings.Split(strings.TrimSpace(list), ",") {
		if part == "" {
			continue
		}
		lo, hi, rng := strings.Cut(part, "-")
		a, err1 := strconv.Atoi(lo)
		if !rng {
			if err1 == nil {
				n++
			}
			continue
		}
		b, err2 := strconv.Atoi(hi)
		if err1 == nil && err2 == nil && b >= a {
			n += b - a + 1
		}
	}
	return n
}

// capacity is C: the smallest of agents/cpu.max, the cpuset and the CPU
// count; agents/memory.high (else memory.max), else share of the RAM.
func capacity(t *Tree, proc string, cores int, share float64) Capacity {
	c := Capacity{CPU: float64(cores)}
	if t != nil {
		agents := filepath.Join(t.root, "agents")
		if q := cpuMax(filepath.Join(agents, "cpu.max")); q > 0 && q < c.CPU {
			c.CPU = q
		}
		if b, err := os.ReadFile(filepath.Join(agents, "cpuset.cpus.effective")); err == nil {
			if n := cpuCount(string(b)); n > 0 && float64(n) < c.CPU {
				c.CPU = float64(n)
			}
		}
		c.Memory = readBytes(filepath.Join(agents, "memory.high"))
		if c.Memory == 0 {
			c.Memory = readBytes(filepath.Join(agents, "memory.max"))
		}
	}
	if c.Memory == 0 {
		if total, _, ok := meminfo(proc); ok {
			c.Memory = uint64(float64(total) * share)
		}
	}
	return c
}

// cpuMax reads a cpu.max file as cores; 0 for "max" or none.
func cpuMax(path string) float64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) != 2 || f[0] == "max" {
		return 0
	}
	q, _ := strconv.ParseFloat(f[0], 64)
	p, _ := strconv.ParseFloat(f[1], 64)
	if p <= 0 {
		return 0
	}
	return q / p
}

// Stall is the memory stall level.
type Stall string

// The memory stall levels.
const (
	StallOK    Stall = "ok"
	StallHigh  Stall = "elevated"
	StallWorst Stall = "critical"
)

// stallLevel names memory PSI full avg10.
func stallLevel(full float64) Stall {
	switch {
	case full >= StallCritical:
		return StallWorst
	case full >= StallElevated:
		return StallHigh
	}
	return StallOK
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func round2(v float64) float64 { return float64(int(v*100+0.5)) / 100 }

// signals fills l's decision 0029 fields; agentsMem is agents/memory.current
// (0 without the tree).
func (s *Sampler) signals(l *Load, proc string, agentsMem uint64) {
	total, avail, ok := meminfo(proc)
	if !ok {
		return // no provider: the fields stay absent
	}
	_ = total
	l.Provider = "proc"
	if s.Tree != nil {
		l.Provider = "cgroups"
	}
	c := capacity(s.Tree, proc, l.Cores, MemoryShare)
	l.Capacity = &c
	if c.Memory > 0 {
		h := float64(avail) / float64(c.Memory)
		if s.Tree != nil {
			// The working set: page cache the kernel drops first
			// (inactive_file) is not used memory; memory.current fills to
			// memory.high with it.
			ws := agentsMem
			if inactive, ok := readKey(filepath.Join(s.Tree.root, "agents", "memory.stat"), "inactive_file"); ok && inactive <= ws {
				ws -= inactive
			}
			if ws <= c.Memory {
				h = min(h, float64(c.Memory-ws)/float64(c.Memory))
			} else {
				h = 0
			}
		}
		h = round2(clamp01(h)*1000) / 1000
		l.Headroom = &h
	}
	if p, ok := readPSI(filepath.Join(proc, "pressure", "memory")); ok && p.hasFull {
		f := p.full
		l.MemoryPressure = &f
		l.MemoryStall = stallLevel(f)
	}
	if s.Tree != nil {
		if p, ok := readPSI(filepath.Join(s.Tree.root, "agents", "cpu.pressure")); ok {
			v := p.some
			l.AgentsPressure = &v
		}
	}
	psiNow := l.AgentsPressure
	if psiNow == nil {
		psiNow = l.CPUPressure
	}
	l.Slowdown = s.Probe.Value(s.now(), psiNow)
}
