package cgroup

// Decision 0029 section 0 (c), the macOS column: no /proc, no cgroups and
// no PSI, so the memory and the load come from sysctl (x/sys/unix, no cgo)
// and the slowdown from the probe and the run queue.

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// hostMemory reads the RAM, what is available and the kernel's memory
// pressure level (1 normal, 2 warn, 4 critical). Available is the smaller of
// the kernel's own share (`kern.memorystatus_level`, what memory_pressure
// prints) and the page counts (availablePages): the kernel's share stands
// still until memory is reclaimed (85 % with 10 GB of 24 just allocated and
// 0.1 GB free), and is right from there on.
func hostMemory() (total, avail uint64, stall Stall, ok bool) {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil || total == 0 {
		return 0, 0, "", false
	}
	var pages uint64
	if page, err := unix.SysctlUint32("hw.pagesize"); err == nil {
		n := func(k string) uint64 { v, _ := unix.SysctlUint32(k); return uint64(v) }
		pages = min(availablePages(n("vm.page_free_count"), n("vm.page_speculative_count"), n("vm.page_purgeable_count"),
			n("vm.page_pageable_external_count"), n("vm.page_pageable_internal_count"))*uint64(page), total)
	}
	pct, err := unix.SysctlUint32("kern.memorystatus_level")
	switch {
	case err == nil && pct <= 100 && pages > 0:
		avail = min(total/100*uint64(pct), pages)
	case err == nil && pct <= 100:
		avail = total / 100 * uint64(pct)
	case pages > 0:
		avail = pages
	default:
		return 0, 0, "", false
	}
	stall = StallOK
	if lvl, err := unix.SysctlUint32("kern.memorystatus_vm_pressure_level"); err == nil {
		stall = pressureLevel(lvl)
	}
	return total, avail, stall, true
}

// availablePages is the pages a new job can get: the free ones, the ones the
// kernel drops (speculative, purgeable, file cache) and half of the programs'
// own, which it compresses instead of leaving them out altogether; macOS keeps
// few pages free, so without those the figure reads far too low.
func availablePages(free, speculative, purgeable, external, internal uint64) uint64 {
	return free + speculative + purgeable + external + (internal-min(internal, purgeable))/2
}

// pressureLevel names kern.memorystatus_vm_pressure_level.
func pressureLevel(lvl uint32) Stall {
	switch {
	case lvl >= 4:
		return StallWorst
	case lvl >= 2:
		return StallHigh
	}
	return StallOK
}

// hostLoadavg reads vm.loadavg: struct loadavg { fixpt_t ldavg[3]; long fscale; }.
func hostLoadavg() ([3]float64, bool) {
	b, err := unix.SysctlRaw("vm.loadavg")
	if err != nil {
		return [3]float64{}, false
	}
	return parseLoadavg(b)
}

func parseLoadavg(b []byte) ([3]float64, bool) {
	var l [3]float64
	if len(b) < 24 {
		return l, false
	}
	scale := float64(binary.LittleEndian.Uint64(b[16:24]))
	if scale <= 0 {
		return l, false
	}
	for i := range l {
		l[i] = round2(float64(binary.LittleEndian.Uint32(b[i*4:])) / scale)
	}
	return l, true
}

// queueBusy is the %CPU a runnable thread must show to count in the run queue:
// a thread that wakes for a moment is runnable too, and costs a job nothing.
const queueBusy = 2.0

// hostQueue is the slowdown the run queue says: macOS runs a thread that just
// slept ahead of the busy ones, so the probe's burst reads 1.0 on a saturated
// host (32 busy loops on 8 cores: burst 1.01, a real job 3.6× slower). A new
// job shares the cores with the threads that compute, which is what this
// counts: (busy + 1) ÷ cores, within 3 % of a job timed under 0 to 32 loops.
func hostQueue(ctx context.Context) (float64, bool) {
	ctx, cancel := context.WithTimeout(ctx, ProbeEvery-time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ps", "-A", "-M", "-o", "%cpu=,state=")
	cmd.Env = append(os.Environ(), "LC_ALL=C") // %cpu has a decimal comma in many locales
	out, err := cmd.Output()
	if err != nil {
		return 0, false
	}
	return queueSlowdown(string(out), runtime.NumCPU())
}

// queueSlowdown reads `ps -A -M -o %cpu=,state=`: ps keeps its own columns
// with -M, so the two asked for are each line's last.
func queueSlowdown(out string, cores int) (float64, bool) {
	if cores <= 0 {
		return 0, false
	}
	busy, lines := 0, 0
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		cpu, err := strconv.ParseFloat(f[len(f)-2], 64)
		if err != nil {
			continue
		}
		lines++
		if cpu >= queueBusy && strings.HasPrefix(f[len(f)-1], "R") {
			busy++
		}
	}
	if lines == 0 {
		return 0, false
	}
	return max(1, float64(busy+1)/float64(cores)), true
}
