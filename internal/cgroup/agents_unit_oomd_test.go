// V0.agents-unit (#221): an oomd kill under the agents' slice never reaches
// fyrd. oomdCandidates walks a cgroup tree as systemd-oomd does
// (oomd-manager.c, recursively_get_cgroup_context, v262): below a monitored
// cgroup every leaf is a candidate, and so is any cgroup with
// memory.oom.group=1, whose children are not walked. A kill takes the whole
// candidate (cgroup.kill).
package cgroup

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func oomdCandidates(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		if !e.IsDir() {
			continue
		}
		d := filepath.Join(dir, e.Name())
		if b, _ := os.ReadFile(filepath.Join(d, "memory.oom.group")); strings.TrimSpace(string(b)) == "1" {
			out = append(out, d)
			continue
		}
		if sub := oomdCandidates(t, d); len(sub) > 0 {
			out = append(out, sub...)
		} else if !hasChildDir(d) {
			out = append(out, d)
		}
	}
	return out
}

func hasChildDir(d string) bool {
	es, _ := os.ReadDir(d)
	for _, e := range es {
		if e.IsDir() {
			return true
		}
	}
	return false
}

// killed is every pid a kill of candidate c ends: the pids of its subtree.
func killed(c string) []int {
	var out []int
	filepath.WalkDir(c, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Name() == "cgroup.procs" {
			out = append(out, pids(p)...)
		}
		return nil
	})
	return out
}

func TestAgentsUnitOomdKillOfTheSliceNeverTakesFyrd(t *testing.T) {
	o, _, _ := scopeFS(t, false)
	fyrd := os.Getpid()
	tr := Setup(o)
	if tr == nil || tr.Layout() != LayoutScope {
		t.Fatal("no scope layout")
	}
	// What systemd and fyrd make: the CLI in its scope (OOMPolicy=kill sets
	// memory.oom.group), a job and undeclared work inside it, a deploy.
	sd := filepath.Join(o.FS, slice, "fyr-agent-t_a.scope")
	write(t, filepath.Join(sd, "cgroup.procs"), "1001\n")
	write(t, filepath.Join(sd, "memory.oom.group"), "1")
	write(t, filepath.Join(sd, jobDir, "cgroup.procs"), "1002\n1003\n")
	write(t, filepath.Join(sd, undeclaredDir, "u1", "cgroup.procs"), "1004\n")
	write(t, filepath.Join(o.FS, slice, "fyr-agent-t_b.scope", "cgroup.procs"), "2001\n")
	write(t, filepath.Join(o.FS, slice, "fyr-agent-t_b.scope", "memory.oom.group"), "1")
	write(t, filepath.Join(o.FS, slice, "fyr-deploy-1-1.scope", "cgroup.procs"), "3001\n")
	write(t, filepath.Join(o.FS, unit, "cgroup.procs"), "111\n"+strconv.Itoa(fyrd)+"\n")

	cands := oomdCandidates(t, filepath.Join(o.FS, slice))
	var names []string
	for _, c := range cands {
		names = append(names, filepath.Base(c))
		if slices.Contains(killed(c), fyrd) || slices.Contains(killed(c), 111) {
			t.Fatalf("an oomd kill of %s takes fyrd or the launcher", c)
		}
	}
	// Whole threads, never a job alone (the 3.7 MB leaves of 18 Nov).
	if got := strings.Join(names, ","); got != "fyr-agent-t_a.scope,fyr-agent-t_b.scope,fyr-deploy-1-1.scope" {
		t.Fatalf("candidates %s", got)
	}
	if k := killed(cands[0]); !slices.Equal(k, []int{1001, 1002, 1003, 1004}) {
		t.Fatalf("a kill of t_a ends %v", k)
	}
	// fyrd's unit is not under the slice at all.
	if rel, _ := filepath.Rel(filepath.Join(o.FS, slice), filepath.Join(o.FS, unit)); !strings.HasPrefix(rel, "..") {
		t.Fatalf("fyr.service under the slice: %s", rel)
	}
}

// The V0.80 layout, for contrast: oomd on fyr.service has server/ (fyrd) as
// a candidate, which is what it took on 18 Nov at 15:30:18.
func TestAgentsUnitUnitLayoutLetsOomdTakeServer(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	write(t, filepath.Join(tr.Thread("t_a"), "cgroup.procs"), "1001\n")
	write(t, filepath.Join(o.FS, unit, "server", "cgroup.procs"), strconv.Itoa(os.Getpid())+"\n")
	var names []string
	for _, c := range oomdCandidates(t, filepath.Join(o.FS, unit)) {
		names = append(names, filepath.Base(c))
	}
	if !slices.Contains(names, "server") {
		t.Fatalf("candidates %v", names)
	}
}
