package runner

import "golang.org/x/sys/unix"

func peerPidFd(fd int) (int, error) {
	return unix.GetsockoptInt(fd, unix.SOL_LOCAL, unix.LOCAL_PEERPID)
}
