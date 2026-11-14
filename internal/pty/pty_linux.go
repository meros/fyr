// Package pty starts a program on a pseudo-terminal (SPEC 0.5 V0.53): some
// CLI screens (`claude setup-token`) draw only when they run in a terminal.
// Linux only, with the standard library (no cgo, no new module).
package pty

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"
)

func ioctl(fd, req, arg uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg); e != 0 {
		return e
	}
	return nil
}

// Start runs cmd with its stdin, stdout and stderr on a new terminal of
// cols × rows, as the leader of its own session (so a kill of -pid reaches
// its children). It returns the terminal's master side: what the program
// writes is read from it, and what is written to it is the program's
// keyboard. The caller closes it.
func Start(cmd *exec.Cmd, cols, rows int) (*os.File, error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("no terminal: %w", err)
	}
	var unlock int32
	if err := ioctl(m.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); err != nil {
		m.Close()
		return nil, fmt.Errorf("no terminal: %w", err)
	}
	var n uint32
	if err := ioctl(m.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); err != nil {
		m.Close()
		return nil, fmt.Errorf("no terminal: %w", err)
	}
	s, err := os.OpenFile("/dev/pts/"+strconv.Itoa(int(n)), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		m.Close()
		return nil, fmt.Errorf("no terminal: %w", err)
	}
	defer s.Close()
	ws := struct{ Row, Col, X, Y uint16 }{Row: uint16(rows), Col: uint16(cols)}
	if err := ioctl(s.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws))); err != nil {
		m.Close()
		return nil, fmt.Errorf("no terminal: %w", err)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = s, s, s
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setctty = true
	cmd.SysProcAttr.Ctty = 0
	if err := cmd.Start(); err != nil {
		m.Close()
		return nil, err
	}
	return m, nil
}
