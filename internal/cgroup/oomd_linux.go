package cgroup

import "golang.org/x/sys/unix"

// omitFromOomd sets user.oomd_omit=1 on dir (V0.oomd-guard).
func omitFromOomd(dir string) error {
	return unix.Setxattr(dir, "user.oomd_omit", []byte("1"), 0)
}
