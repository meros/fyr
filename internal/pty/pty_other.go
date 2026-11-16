//go:build !linux && !darwin

package pty

import (
	"errors"
	"os"
	"os/exec"
)

// Start is Linux and macOS only.
func Start(cmd *exec.Cmd, cols, rows int) (*os.File, error) {
	return nil, errors.New("no terminal on this system")
}
