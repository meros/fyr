//go:build !linux

package cgroup

import (
	"os"
	"os/exec"
)

func dieWithParent(*exec.Cmd) {}

func intoCgroup(*exec.Cmd, *os.File) {}
