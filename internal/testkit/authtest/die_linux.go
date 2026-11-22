package authtest

import (
	"os/exec"
	"syscall"
)

// dieWithTest ends a test's fyrd when the test process dies.
func dieWithTest(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL} }
