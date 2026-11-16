package cgroup

import (
	"os"
	"os/exec"
	"syscall"
)

func probe(dir string) error {
	bin, err := exec.LookPath("true")
	if err != nil {
		return nil // nothing to try it with; the first agent will tell
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.Command(bin)
	cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(f.Fd())}
	return cmd.Run()
}
