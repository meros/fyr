//go:build !linux

package cgroup

// omitFromOomd: only Linux has systemd-oomd.
func omitFromOomd(string) error { return nil }
