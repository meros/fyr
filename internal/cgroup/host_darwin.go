package cgroup

// Decision 0029 section 0 (c), the macOS column: no /proc, no cgroups and
// no PSI, so the memory and the load come from sysctl (x/sys/unix, no cgo)
// and the slowdown from the probe alone.

import (
	"encoding/binary"

	"golang.org/x/sys/unix"
)

// hostMemory reads the RAM, what is available and the kernel's memory
// pressure level (1 normal, 2 warn, 4 critical). Available is the kernel's
// own share (`kern.memorystatus_level`, what memory_pressure prints): macOS
// keeps few pages free and compresses instead, so the free pages and the
// file cache alone read far too low. Without it, those pages are the floor.
func hostMemory() (total, avail uint64, stall Stall, ok bool) {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil || total == 0 {
		return 0, 0, "", false
	}
	if pct, err := unix.SysctlUint32("kern.memorystatus_level"); err == nil && pct <= 100 {
		avail = total / 100 * uint64(pct)
	} else if page, err := unix.SysctlUint32("hw.pagesize"); err == nil {
		var pages uint64
		for _, k := range []string{"vm.page_free_count", "vm.page_speculative_count", "vm.page_purgeable_count", "vm.page_pageable_external_count"} {
			n, _ := unix.SysctlUint32(k)
			pages += uint64(n)
		}
		avail = min(pages*uint64(page), total)
	} else {
		return 0, 0, "", false
	}
	stall = StallOK
	if lvl, err := unix.SysctlUint32("kern.memorystatus_vm_pressure_level"); err == nil {
		stall = pressureLevel(lvl)
	}
	return total, avail, stall, true
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
