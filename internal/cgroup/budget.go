package cgroup

// V0.agents-budget-share (#198): the agents' budget is a share of the
// machine. The module's defaults are percentages ("55%"), which systemd takes
// as they are on the agents' slice (MemoryHigh and MemoryMax of the RAM,
// MemorySwapMax of the swap). In the unit layout fyrd writes agents/'s files
// itself, and the kernel takes only bytes, so fyrd resolves a percentage
// against /proc/meminfo first. Either way fyrd says the budget it runs with
// once, at start.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// procTotals reads /proc/meminfo's MemTotal and SwapTotal in bytes.
func procTotals(proc string) (mem, swap uint64) {
	f, err := os.Open(filepath.Join(proc, "meminfo"))
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), ":")
		n, _ := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(v), " kB"), 10, 64)
		switch k {
		case "MemTotal":
			mem = n << 10
		case "SwapTotal":
			swap = n << 10
		}
	}
	return mem, swap
}

// resolveShare turns "55%" into bytes of total, rounded down to a 4 KiB page;
// any other value is returned as it is. A percentage it cannot resolve (no
// total, a bad number) is "": the file is then left as it is, and the error
// names why.
func resolveShare(v string, total uint64) (string, error) {
	p, ok := strings.CutSuffix(strings.TrimSpace(v), "%")
	if !ok {
		return v, nil
	}
	f, err := strconv.ParseFloat(p, 64)
	if err != nil || f < 0 || f > 100 {
		return "", fmt.Errorf("%q is not a percentage", v)
	}
	if total == 0 {
		return "", fmt.Errorf("%q: the machine's total is unknown", v)
	}
	b := uint64(float64(total)*f/100) &^ 4095
	return strconv.FormatUint(b, 10), nil
}

// budgetLine is the agents' budget as the kernel holds it, for the
// "cgroups: on" line: each limit, and its share of the RAM or the swap.
func budgetLine(agents, proc string) string {
	mem, swap := procTotals(proc)
	part := func(file string, total uint64, of string) string {
		b, err := os.ReadFile(filepath.Join(agents, file))
		if err != nil {
			return file + " unread"
		}
		raw := strings.TrimSpace(string(b))
		if raw == "max" || raw == "" {
			return file + " none"
		}
		n := readBytes(filepath.Join(agents, file))
		if n == 0 || total == 0 {
			return file + " " + raw
		}
		return fmt.Sprintf("%s %s (%.0f%% of %s %s)", file, gib(n), 100*float64(n)/float64(total), gib(total), of)
	}
	cpu := "cpu.max none"
	if c := cpuMax(filepath.Join(agents, "cpu.max")); c > 0 {
		cpu = fmt.Sprintf("cpu.max %.1f cores", c)
	}
	return "agents' budget: " + strings.Join([]string{
		part("memory.high", mem, "RAM"),
		part("memory.max", mem, "RAM"),
		part("memory.swap.max", swap, "swap"),
		cpu,
	}, ", ")
}
