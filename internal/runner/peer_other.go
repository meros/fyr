//go:build !linux && !darwin

package runner

import "errors"

func peerPidFd(int) (int, error) { return 0, errors.ErrUnsupported }
