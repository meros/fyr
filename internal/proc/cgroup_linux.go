package proc

import (
	"os"
	"os/exec"
	"syscall"
)

func setCgroup(cmd *exec.Cmd, f *os.File) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.UseCgroupFD = true
	cmd.SysProcAttr.CgroupFD = int(f.Fd())
}
