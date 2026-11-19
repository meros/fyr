//go:build !linux

package cgroup

import "errors"

// probe fails off Linux: there is no CLONE_INTO_CGROUP, so Setup runs
// every process in the daemon's own cgroup.
func probe(dir string) error { return errors.New("cgroups need Linux") }

// probeScope fails off Linux: no systemd scopes.
func probeScope(prefix []string) (string, string, error) {
	return "", "", errors.New("scopes need Linux")
}
