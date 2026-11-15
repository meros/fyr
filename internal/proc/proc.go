// Package proc starts child processes so that time limits hold and nothing
// they start outlives them (#39). Each child runs in its own process group;
// stopping it kills the whole group, and Wait returns at most WaitDelay
// after the child exits or its context ends, even when a grandchild still
// holds an output pipe open.
package proc

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// WaitDelay bounds how long Wait waits for the output pipes after the
// child exited or was stopped.
const WaitDelay = 5 * time.Second

// Command is exec.CommandContext with the child in its own process group:
// when ctx ends, the whole group is killed, and Wait returns at most
// WaitDelay later.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	Group(cmd)
	cmd.Cancel = func() error { return Kill(cmd) }
	return cmd
}

// Group puts cmd in its own process group and bounds its Wait by WaitDelay.
// Use it for a child without a context (Kill stops it); call it before
// Start.
func Group(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.WaitDelay = WaitDelay
}

// Kill kills cmd's process group: the child and everything it started that
// did not leave the group. It is a no-op before Start.
func Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
