//go:build !linux

package cgroup

import (
	"os"
	"os/exec"
)

// CanBurst: no CPU clock per thread here; the slowdown stays absent
// (decision 0029: macOS keeps the fixed count for now).
func CanBurst() bool { return false }

func Burst() (float64, bool) { return 0, false }

func dieWithParent(*exec.Cmd) {}

func intoCgroup(*exec.Cmd, *os.File) {}
