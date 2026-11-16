package deploy

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
)

// #147: builds/ held 81 builds and 7.3 GB after one day of deploys (66
// database snapshots of ~182 MB). After every good switch Fyr keeps the
// builds that current, previous and next point at, plus the newest Keep
// others, and database snapshots only in current and previous.

// Prune deletes old builds and snapshots. It never touches a linked build.
func (d *Deployer) Prune() {
	keep := d.Keep
	if keep == 0 {
		keep = 3
	}
	n, freed := prune(d.Data, keep)
	if n > 0 || freed > 0 {
		log.Printf("prune: removed %d builds, %d MB freed", n, freed>>20)
	}
}

func prune(data string, keep int) (removed int, freed int64) {
	builds := filepath.Join(data, "builds")
	linked := map[string]bool{}
	snap := map[string]bool{}
	for _, l := range []string{"current", "previous", "next"} {
		if t, err := os.Readlink(filepath.Join(data, l)); err == nil {
			linked[filepath.Base(t)] = true
			if l != "next" {
				snap[filepath.Base(t)] = true
			}
		}
	}
	ents, err := os.ReadDir(builds)
	if err != nil {
		return 0, 0
	}
	type b struct {
		name string
		mod  int64
	}
	var others []b
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), ".dl-") { // a download that never finished
			freed += size(filepath.Join(builds, e.Name()))
			os.RemoveAll(filepath.Join(builds, e.Name()))
			continue
		}
		if linked[e.Name()] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		others = append(others, b{e.Name(), info.ModTime().UnixNano()})
	}
	sort.Slice(others, func(i, j int) bool { return others[i].mod > others[j].mod })
	for i, o := range others {
		p := filepath.Join(builds, o.name)
		if i >= keep {
			freed += size(p)
			if os.RemoveAll(p) == nil {
				removed++
			}
			continue
		}
		if !snap[o.name] {
			if st, err := os.Stat(filepath.Join(p, "pre.db")); err == nil {
				freed += st.Size()
				os.Remove(filepath.Join(p, "pre.db"))
			}
		}
	}
	return removed, freed
}

func size(p string) (n int64) {
	filepath.Walk(p, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			n += info.Size()
		}
		return nil
	})
	return n
}

// MinFree is the free space a build or download needs on the data disk:
// 2 GB, or three times the database when that is more (a snapshot, a
// smoke copy and room to grow).
func minFree(data string) uint64 {
	need := uint64(2 << 30)
	if st, err := os.Stat(filepath.Join(data, "fyr.db")); err == nil && uint64(st.Size())*3 > need {
		need = uint64(st.Size()) * 3
	}
	return need
}

// diskRoom refuses to start a build when the data disk is nearly full.
func diskRoom(data string) error {
	var st syscall.Statfs_t
	if err := syscall.Statfs(data, &st); err != nil {
		return nil // cannot say: do not block the update
	}
	free := uint64(st.Bavail) * uint64(st.Bsize)
	if need := minFree(data); free < need {
		return fmt.Errorf("not enough disk space: %d MB free on the data disk, %d MB needed; nothing was built", free>>20, need>>20)
	}
	return nil
}

var (
	goFail   = regexp.MustCompile(`(?m)^\s*--- FAIL: (\S+)`)
	goPkg    = regexp.MustCompile(`(?m)^FAIL[ \t]+(\S+)`)
	makeErr  = regexp.MustCompile(`(?m)^make(?:\[\d+\])?: \*\*\* \[[^\]]*?:\s*(\S+)\]`)
	npmFail  = regexp.MustCompile(`(?m)^npm (?:ERR!|error) (?:Lifecycle script|command failed|code) .*`)
	goBuildE = regexp.MustCompile(`(?m)^(\S+\.go:\d+:\d+: .+)$`)
)

// Failing names what failed in a build or test output (#145): the Go tests
// and their packages, else a compile error, else the make target or npm
// script, else "see the log".
func Failing(out []byte) string {
	s := string(out)
	var parts []string
	seen := map[string]bool{}
	for _, m := range goFail.FindAllStringSubmatch(s, 5) {
		if !seen[m[1]] {
			seen[m[1]] = true
			parts = append(parts, m[1])
		}
	}
	for _, m := range goPkg.FindAllStringSubmatch(s, 5) {
		if m[1] != "" && !seen[m[1]] {
			seen[m[1]] = true
			parts = append(parts, "in "+m[1])
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, ", ")
	}
	if m := goBuildE.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	if m := npmFail.FindString(s); m != "" {
		return strings.TrimSpace(m)
	}
	if m := makeErr.FindStringSubmatch(s); m != nil {
		return "make target " + m[1]
	}
	return "see the log"
}
