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
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// hostMemory reads the RAM, what is available and the memory stall: the
// kernel's pressure level (1 normal, 2 warn, 4 critical) or the swap traffic
// (swapStall), whichever is worse. Available is the smaller of the kernel's
// own share (`kern.memorystatus_level`, what memory_pressure prints) and the
// page counts (availablePages): the kernel's share stands still until memory
// is reclaimed (85 % with 10 GB of 24 just allocated and 0.1 GB free), and is
// right from there on.
func hostMemory() (total, avail uint64, stall Stall, ok bool) {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil || total == 0 {
		return 0, 0, "", false
	}
	swap := hostSwap.read(time.Now())
	var pages uint64
	if page, err := unix.SysctlUint32("hw.pagesize"); err == nil {
		n := func(k string) uint64 { v, _ := unix.SysctlUint32(k); return uint64(v) }
		internal := n("vm.page_pageable_internal_count")
		if swap.outRecent {
			internal = 0 // the compressor made no room: the kernel swaps instead
		}
		pages = min(availablePages(n("vm.page_free_count"), n("vm.page_speculative_count"), n("vm.page_purgeable_count"),
			n("vm.page_pageable_external_count"), internal)*uint64(page), total)
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
	return total, avail, worseStall(stall, swapStall(swap.in, swap.out)), true
}

// The swap traffic, in bytes/s, that makes a Mac's memory stall: macOS has no
// PSI, and its pressure level stays at warn (elevated) while the machine
// thrashes. Measured on an 8-core, 24 GB Mac: calm, swap-ins of 0 to 15 MB/s
// and no swap-outs; a job spilling into swap, 190 to 430 MB/s out; a job
// re-reading 17 GB, 200 to 300 MB/s each way for minutes, the headroom at 29 %
// and the level at warn throughout.
const (
	swapElevated = 16 << 20 // out, or in at swapCritical alone
	swapCritical = 64 << 20 // in and out both: what is read back pushes other pages out
)

// swapStall is the stall the swap traffic says: critical when pages are read
// back while others are pushed out (thrashing), elevated while the kernel swaps
// out or reads back a lot.
func swapStall(in, out float64) Stall {
	switch {
	case in >= swapCritical && out >= swapCritical:
		return StallWorst
	case out >= swapElevated || in >= swapCritical:
		return StallHigh
	}
	return StallOK
}

func worseStall(a, b Stall) Stall {
	rank := map[Stall]int{StallOK: 1, StallHigh: 2, StallWorst: 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// swapWindow is the shortest gap a swap rate is read over; a read sooner gives
// the last rates. swapGapMax is the longest; a longer one starts again.
// swapRecent is how long after a swap-out the compressor counts for nothing.
const (
	swapWindow = 5 * time.Second
	swapGapMax = 2 * time.Minute
	swapRecent = time.Minute
)

// swapRates is the swap traffic in bytes/s, and whether the kernel swapped out
// within swapRecent.
type swapRates struct {
	in, out   float64
	outRecent bool
}

// swapMeter turns the kernel's swap counters (pages) into rates.
type swapMeter struct {
	mu      sync.Mutex
	counts  func() (in, out, page uint64, ok bool)
	at      time.Time
	in, out uint64
	last    swapRates
	outAt   time.Time
}

var hostSwap = &swapMeter{counts: func() (uint64, uint64, uint64, bool) {
	in, e1 := unix.SysctlUint64("vm.compressor.swapper.swapins_total")
	out, e2 := unix.SysctlUint64("vm.compressor.swapper.swapouts_total")
	page, e3 := unix.SysctlUint32("hw.pagesize")
	return in, out, uint64(page), e1 == nil && e2 == nil && e3 == nil
}}

func (m *swapMeter) read(now time.Time) swapRates {
	m.mu.Lock()
	defer m.mu.Unlock()
	in, out, page, ok := m.counts()
	if !ok {
		return swapRates{}
	}
	gap := now.Sub(m.at)
	switch {
	case m.at.IsZero() || gap > swapGapMax || in < m.in || out < m.out:
		m.at, m.in, m.out, m.last = now, in, out, swapRates{}
	case gap >= swapWindow:
		s := gap.Seconds()
		m.last = swapRates{in: float64((in-m.in)*page) / s, out: float64((out-m.out)*page) / s}
		if m.last.out >= swapElevated {
			m.outAt = now
		}
		m.at, m.in, m.out = now, in, out
	}
	r := m.last
	r.outRecent = !m.outAt.IsZero() && now.Sub(m.outAt) < swapRecent
	return r
}

// availablePages is the pages a new job can get: the free ones, the ones the
// kernel drops (speculative, purgeable, file cache) and half of the programs'
// own, which it compresses instead of leaving them out altogether; macOS keeps
// few pages free, so without those the figure reads far too low. While the
// kernel swaps out, the compressor has made no room and hostMemory counts none
// of the programs' own (a job of 17 GB that does not compress, swapping: 30 %
// free with them, 16 % without).
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
