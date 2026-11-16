//go:build !linux && !darwin

package auth

import (
	"errors"
	"net"
)

func peerUID(net.Conn) (int, error) { return -1, errors.New("peer credentials are not supported here") }
