package cgroup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

var errNoTree = errors.New("no cgroup tree")

// watchPressure opens file, writes the trigger and polls for POLLPRI; each
// event calls wake. The poll wakes every second to see ctx.
func watchPressure(ctx context.Context, file, trigger string, wake func()) error {
	fd, err := unix.Open(file, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	if _, err := unix.Write(fd, []byte(trigger+"\x00")); err != nil {
		unix.Close(fd)
		return fmt.Errorf("write trigger: %w", err)
	}
	go func() {
		defer unix.Close(fd)
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLPRI}}
		for ctx.Err() == nil {
			n, err := unix.Poll(fds, 1000)
			if err != nil {
				if errors.Is(err, unix.EINTR) {
					continue
				}
				return
			}
			if n == 0 {
				continue
			}
			if fds[0].Revents&unix.POLLERR != 0 {
				return // the cgroup went away
			}
			if fds[0].Revents&unix.POLLPRI != 0 {
				wake()
				// One event per window at most from the kernel; a short
				// pause keeps a storm of them from spinning.
				time.Sleep(200 * time.Millisecond)
			}
		}
	}()
	return nil
}
