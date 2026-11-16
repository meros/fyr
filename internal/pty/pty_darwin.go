package pty

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// openPty opens a new terminal: its master side and its slave side. macOS
// names the slave with TIOCPTYGNAME, not a /dev/pts number.
func openPty() (*os.File, *os.File, error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("no terminal: %w", err)
	}
	if err := ioctl(m.Fd(), syscall.TIOCPTYGRANT, 0); err != nil {
		m.Close()
		return nil, nil, fmt.Errorf("no terminal: %w", err)
	}
	if err := ioctl(m.Fd(), syscall.TIOCPTYUNLK, 0); err != nil {
		m.Close()
		return nil, nil, fmt.Errorf("no terminal: %w", err)
	}
	var name [128]byte
	if err := ioctl(m.Fd(), syscall.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0]))); err != nil {
		m.Close()
		return nil, nil, fmt.Errorf("no terminal: %w", err)
	}
	n := name[:]
	if i := bytes.IndexByte(n, 0); i >= 0 {
		n = n[:i]
	}
	s, err := os.OpenFile(string(n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		m.Close()
		return nil, nil, fmt.Errorf("no terminal: %w", err)
	}
	return m, s, nil
}
