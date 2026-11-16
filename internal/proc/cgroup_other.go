//go:build !linux

package proc

import (
	"os"
	"os/exec"
)

// setCgroup is a no-op off Linux: there are no cgroups, and the child
// runs in the daemon's process tree as before.
func setCgroup(cmd *exec.Cmd, f *os.File) {}
