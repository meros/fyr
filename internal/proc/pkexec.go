package proc

import (
	"os"
	"os/exec"
)

// PkexecInstalled reports whether this host has pkexec, on PATH or as the
// NixOS setuid wrapper (V0.149 (4), V0.prompt-fits-setup).
func PkexecInstalled() bool {
	if _, err := exec.LookPath("pkexec"); err == nil {
		return true
	}
	_, err := os.Stat("/run/wrappers/bin/pkexec")
	return err == nil
}
