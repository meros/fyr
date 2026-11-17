//go:build !linux && !darwin

package cgroup

// CanBurst: no CPU clock per thread here; the slowdown stays absent.
func CanBurst() bool { return false }

func Burst() (float64, bool) { return 0, false }
