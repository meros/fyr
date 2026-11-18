//go:build !darwin

package cgroup

import "context"

// hostMemory and hostLoadavg: Linux reads /proc instead.
func hostMemory() (total, avail uint64, stall Stall, ok bool) { return 0, 0, "", false }

func hostLoadavg() ([3]float64, bool) { return [3]float64{}, false }

// hostQueue: Linux has the probe and PSI.
func hostQueue(context.Context) (float64, bool) { return 0, false }
