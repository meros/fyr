package cgroup

// V0.cache-reclaim: the agents fill their memory.high with page cache (git,
// go and node builds read a lot), and the kernel then reclaims that cache in
// the context of whichever task charges the next page, throttling it. PSI
// counts that throttle as the agents' full memory stall, so on 20 Nov 48 of
// 74 "memory short" episodes came at 50 % headroom or more, with the slice at
// memory.high and 525 000 `high` events. fyrd now asks the kernel to drop the
// agents' file cache (memory.reclaim with swappiness=0: page cache only, no
// anonymous memory to swap) once the agents pass CacheReclaimAt of their
// memory.high, down to CacheReclaimTo, so no task is throttled for cache. A
// stall of the working set itself (anonymous memory, or cache read back as
// fast as it goes) still stalls and still makes memory short.

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const (
	// CacheReclaimAt is the share of the agents' memory.high past which their
	// page cache is dropped ahead of the kernel.
	CacheReclaimAt = 0.90
	// CacheReclaimTo is the share a reclaim brings them down to.
	CacheReclaimTo = 0.80
	// cacheReclaimMin is the least a cgroup is asked to drop: smaller writes
	// cost a syscall for nothing the kernel would notice.
	cacheReclaimMin = 16 << 20
	// cacheReclaimLog is how often the reclaims are summed into one log line.
	cacheReclaimLog = time.Minute
)

// cacheReclaim is the state of the Tree's cache reclaim.
type cacheReclaim struct {
	running bool
	off     bool // the kernel takes no swappiness=0: never again
	freed   uint64
	times   int
	logged  time.Time
}

// ReclaimCache drops the agents' page cache in the background when they are
// past CacheReclaimAt of their memory.high; one reclaim at a time, and none
// without a tree, without a memory.high or on a kernel without swappiness=
// on memory.reclaim. The sampler calls it every tick.
func (t *Tree) ReclaimCache(now time.Time) {
	if t == nil {
		return
	}
	agents := t.agentsDir()
	high := readBytes(filepath.Join(agents, "memory.high"))
	cur := readBytes(filepath.Join(agents, "memory.current"))
	if high == 0 || cur <= uint64(float64(high)*CacheReclaimAt) {
		return
	}
	t.cacheMu.Lock()
	if t.cache.running || t.cache.off {
		t.cacheMu.Unlock()
		return
	}
	t.cache.running = true
	t.cacheMu.Unlock()
	want := cur - uint64(float64(high)*CacheReclaimTo)
	go func() {
		freed, err := reclaimCache(agents, want)
		after := readBytes(filepath.Join(agents, "memory.current"))
		t.cacheMu.Lock()
		defer t.cacheMu.Unlock()
		t.cache.running = false
		if errors.Is(err, syscall.EINVAL) {
			t.cache.off = true
			if t.logf != nil {
				t.logf("cgroups: this kernel takes no swappiness=0 on memory.reclaim: the agents' cache is left to the kernel at memory.high")
			}
			return
		}
		t.cache.freed += freed
		t.cache.times++
		if t.logf != nil && now.Sub(t.cache.logged) >= cacheReclaimLog {
			t.logf("cgroups: dropped %s of the agents' page cache ahead of memory.high (%d reclaims; now %s of %s)",
				gib(t.cache.freed), t.cache.times, gib(after), gib(high))
			t.cache.freed, t.cache.times, t.cache.logged = 0, 0, now
		}
	}()
}

// reclaimCache asks the kernel to drop want bytes of page cache under agents:
// from agents itself when fyrd may write it (the unit layout), else from its
// child cgroups in proportion to their inactive_file (the scope layout, where
// the slice is systemd's and each thread's scope is fyrd's). It returns what
// memory.current fell by; EINVAL when the kernel takes no swappiness=.
func reclaimCache(agents string, want uint64) (uint64, error) {
	before := readBytes(filepath.Join(agents, "memory.current"))
	fell := func() uint64 {
		if after := readBytes(filepath.Join(agents, "memory.current")); after < before {
			return before - after
		}
		return 0
	}
	err := reclaim(agents, strconv.FormatUint(want, 10)+" swappiness=0")
	switch {
	case err == nil, errors.Is(err, syscall.EAGAIN):
		// EAGAIN: the kernel dropped less than asked; what it dropped counts.
		return fell(), nil
	case errors.Is(err, syscall.EINVAL):
		return 0, err
	}
	// Not ours to write (EACCES, or no file): the children are.
	type child struct {
		dir      string
		inactive uint64
	}
	var kids []child
	var total uint64
	es, _ := os.ReadDir(agents)
	for _, e := range es {
		if !e.IsDir() {
			continue
		}
		d := filepath.Join(agents, e.Name())
		if !pathExists(filepath.Join(d, "memory.reclaim")) {
			continue
		}
		n, ok := readKey(filepath.Join(d, "memory.stat"), "inactive_file")
		if !ok || n == 0 {
			continue
		}
		kids = append(kids, child{d, n})
		total += n
	}
	if total == 0 {
		return 0, nil
	}
	want = min(want, total)
	for _, k := range kids {
		share := uint64(float64(want) * float64(k.inactive) / float64(total))
		if share < cacheReclaimMin {
			continue
		}
		if err := reclaim(k.dir, strconv.FormatUint(share, 10)+" swappiness=0"); errors.Is(err, syscall.EINVAL) {
			return fell(), err
		}
	}
	return fell(), nil
}
