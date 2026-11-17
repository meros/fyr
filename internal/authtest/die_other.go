//go:build !linux

package authtest

import "os/exec"

// dieWithTest: no parent-death signal off Linux; Stop ends the fyrd.
func dieWithTest(*exec.Cmd) {}
