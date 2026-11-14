//go:build !linux

package pty

import (
	"errors"
	"os"
	"os/exec"
)

// Start is Linux only.
func Start(cmd *exec.Cmd, cols, rows int) (*os.File, error) {
	return nil, errors.New("no terminal on this system")
}
