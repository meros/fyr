// Package proc starts child processes so that time limits hold and nothing
// they start outlives them (#39). Each child runs in its own process group;
// stopping it kills the whole group, and Wait returns at most WaitDelay
// after the child exits or its context ends, even when a grandchild still
// holds an output pipe open.
package proc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// WaitDelay bounds how long Wait waits for the output pipes after the
// child exited or was stopped.
const WaitDelay = 5 * time.Second

// Command is exec.CommandContext with the child in its own process group:
// when ctx ends, the whole group is killed, and Wait returns at most
// WaitDelay later. A ctx from WithCgroup starts it in that cgroup.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	Group(cmd)
	if f, ok := ctx.Value(cgroupKey{}).(*os.File); ok {
		setCgroup(cmd, f)
	}
	cmd.Cancel = func() error { return Kill(cmd) }
	return cmd
}

// IntoCgroup makes cmd start in the cgroup directory dir (SPEC 0.5 V0.80):
// clone3 with CLONE_INTO_CGROUP, so the child and everything it starts are
// there from the first instruction and nothing has to move them. Call the
// returned func after Start. An empty dir is a no-op.
func IntoCgroup(cmd *exec.Cmd, dir string) (func(), error) {
	if dir == "" {
		return func() {}, nil
	}
	f, err := os.Open(dir)
	if err != nil {
		return func() {}, err
	}
	setCgroup(cmd, f)
	return func() { f.Close() }, nil
}

func setCgroup(cmd *exec.Cmd, f *os.File) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.UseCgroupFD = true
	cmd.SysProcAttr.CgroupFD = int(f.Fd())
}

type cgroupKey struct{}

// WithCgroup returns a context whose Commands start in the cgroup
// directory dir (V0.80), and a func that closes it once those commands
// have ended. An empty dir, or one that cannot be opened, changes nothing.
func WithCgroup(ctx context.Context, dir string) (context.Context, func()) {
	if dir == "" {
		return ctx, func() {}
	}
	f, err := os.Open(dir)
	if err != nil {
		return ctx, func() {}
	}
	return context.WithValue(ctx, cgroupKey{}, f), func() { f.Close() }
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
