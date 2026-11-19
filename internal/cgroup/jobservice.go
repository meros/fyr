package cgroup

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Holds says pid is in thread's job cgroup or its undeclared-work cgroup (or
// a part under it): what the guard would freeze. Decision 0036 (D): the guard
// never freezes a service, so a thread whose job or undeclared work holds one
// of its services is passed over.
func (g *JobGroups) Holds(thread string, pid int) bool {
	if g == nil || g.Tree == nil || pid <= 0 {
		return false
	}
	want := strconv.Itoa(pid)
	for _, root := range []string{g.dir(thread), g.udir(thread)} {
		found := false
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || found || !d.IsDir() {
				return nil
			}
			b, _ := os.ReadFile(filepath.Join(p, "cgroup.procs"))
			for _, f := range strings.Fields(string(b)) {
				if f == want {
					found = true
					return filepath.SkipAll
				}
			}
			return nil
		})
		if found {
			return true
		}
	}
	return false
}
