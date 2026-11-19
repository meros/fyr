package cgroup

import "golang.org/x/sys/unix"

// releaseMemory opens a pidfd for each process before the kill; the returned
// func, after it, asks the kernel to free each one's memory now
// (process_mrelease) instead of while it exits, and closes the pidfds.
func releaseMemory(ps []int) func(killed bool) {
	var fds []int
	for _, p := range ps {
		if fd, err := unix.PidfdOpen(p, 0); err == nil {
			fds = append(fds, fd)
		}
	}
	return func(killed bool) {
		for _, fd := range fds {
			if killed {
				unix.Syscall(unix.SYS_PROCESS_MRELEASE, uintptr(fd), 0, 0) // best effort: an old kernel says ENOSYS
			}
			unix.Close(fd)
		}
	}
}
