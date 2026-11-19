// Decision 0032 slice 3 as V0.share-shadow left it: the thread share is only
// measured (the guard's `budget over` and `budget summary` lines against a
// shadow share of shadowPercent), and no thread's memory.high is written.
package cgroup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testBudget = uint64(1000) << 20

// shareConfig is a config.json "cgroups" that still names the removed share.
const shareConfig = `{"enable":true,"agentsMemoryHigh":"1048576000","threadMemoryHighPercent":12}`

func budgetOptions(t *testing.T) (Options, *logs) {
	t.Helper()
	o, l := fakeFS(t, "0::/"+unit+"\n")
	var b Budget
	if err := json.Unmarshal([]byte(shareConfig), &b); err != nil {
		t.Fatal(err)
	}
	o.Budget = b
	return o, l
}

func budgetTree(t *testing.T) (*Tree, string, string) {
	t.Helper()
	o, _ := budgetOptions(t)
	tr := Setup(o)
	if tr == nil {
		t.Fatal("no tree")
	}
	return tr, threadOf(tr, "t_a"), filepath.Join(tr.root, "agents")
}

// budgetLog is a tree whose clock and log lines the test holds.
func budgetLog(t *testing.T) (*Tree, string, *[]string, *time.Time) {
	t.Helper()
	tr, td, _ := budgetTree(t)
	lines, now := &[]string{}, &time.Time{}
	*now = time.Unix(1_800_000_000, 0)
	tr.logf = func(f string, a ...any) { *lines = append(*lines, fmt.Sprintf(f, a...)) }
	tr.budgetNow = func() time.Time { return *now }
	return tr, td, lines, now
}

func withWord(lines []string, word string) []string {
	var out []string
	for _, l := range lines {
		if strings.Contains(l, word) {
			out = append(out, l)
		}
	}
	return out
}

// V0.share-shadow: a config that still names threadMemoryHighPercent starts
// fyrd, with one line that it is ignored.
func TestShareShadowTheRemovedSettingIsIgnoredWithOneLine(t *testing.T) {
	o, l := budgetOptions(t)
	if Setup(o) == nil {
		t.Fatalf("no tree: %q", l.lines)
	}
	if got := withWord(l.lines, "threadMemoryHighPercent"); len(got) != 1 || !strings.Contains(got[0], "ignored") {
		t.Fatalf("%q", got)
	}
}

// V0.share-shadow: the shadow lines still come, and the thread keeps no limit
// of its own, with a slot held or not.
func TestShareShadowLogsTheShareAndWritesNoMemoryHigh(t *testing.T) {
	tr, td, lines, now := budgetLog(t)
	write(t, filepath.Join(td, "memory.current"), strconv.FormatUint(300<<20, 10))
	write(t, filepath.Join(td, "memory.stat"), "inactive_file 0\npgmajfault 100\n")
	write(t, filepath.Join(td, "memory.events"), "low 0\nhigh 5\nmax 0\n")
	write(t, filepath.Join(td, "memory.pressure"), "some avg10=40.00 avg60=0 avg300=0 total=1\nfull avg10=20.00 avg60=0 avg300=0 total=1\n")
	tr.ShadowThreadBudget(nil)
	tr.ShadowThreadBudget(nil)
	if got := withWord(*lines, "budget over"); len(got) != 1 {
		t.Fatalf("%q", got)
	}
	*now = now.Add(time.Minute)
	write(t, filepath.Join(td, "memory.stat"), "inactive_file 0\npgmajfault "+strconv.Itoa(100+60*256)+"\n")
	write(t, filepath.Join(td, "memory.events"), "low 0\nhigh 9\nmax 0\n")
	tr.ShadowThreadBudget(nil)
	got := withWord(*lines, "budget over")
	if len(got) != 2 {
		t.Fatalf("%q", got)
	}
	for _, w := range []string{"thread=t_a", "mode=shadow", "working=300MB", "limit=120MB", "high_events=+4", "psi_some=40", "psi_full=20"} {
		if !strings.Contains(got[1], w) {
			t.Fatalf("no %s in %q", w, got[1])
		}
	}
	if os.Getpagesize() == 4096 && !strings.Contains(got[1], "read_back=1MB/s") {
		t.Fatalf("%q", got[1])
	}
	s := withWord(*lines, "budget summary")
	if len(s) != 2 || !strings.Contains(s[1], "mode=shadow") || !strings.Contains(s[1], "share=120MB") || !strings.Contains(s[1], "over=1") {
		t.Fatalf("%q", s)
	}
	*now = now.Add(time.Minute)
	tr.ShadowThreadBudget(map[string]bool{"t_a": true})
	if _, err := os.Stat(filepath.Join(td, "memory.high")); !os.IsNotExist(err) {
		t.Fatalf("a thread got a memory.high: %v", err)
	}
	if l := withWord(*lines, "budget limit"); len(l) != 0 {
		t.Fatalf("%q", l)
	}
}

// V0.share-shadow: in the scope layout a thread's memory.high would go through
// systemctl set-property; no such call is made.
func TestShareShadowSetsNoMemoryHighOnAScope(t *testing.T) {
	o, _, calls := scopeFS(t, false)
	write(t, filepath.Join(o.FS, slice, "memory.high"), strconv.FormatUint(testBudget, 10))
	tr := Setup(o)
	tr.Launch("t_a")
	scope := filepath.Join(o.FS, slice, "fyr-agent-t_a.scope")
	write(t, filepath.Join(scope, "cgroup.procs"), "10\n")
	write(t, filepath.Join(scope, "memory.current"), strconv.FormatUint(300<<20, 10))
	tr.ShadowThreadBudget(nil)
	tr.ShadowThreadBudget(map[string]bool{"t_a": true})
	for _, c := range *calls {
		if strings.Contains(c, "MemoryHigh") {
			t.Fatalf("call %q", c)
		}
	}
}

func TestShareShadowFileCacheDoesNotCountAsOver(t *testing.T) {
	tr, td, lines, _ := budgetLog(t)
	write(t, filepath.Join(td, "memory.current"), strconv.FormatUint(300<<20, 10))
	write(t, filepath.Join(td, "memory.stat"), "inactive_file "+strconv.FormatUint(250<<20, 10)+"\n")
	tr.ShadowThreadBudget(nil)
	if got := withWord(*lines, "budget over"); len(got) != 0 {
		t.Fatalf("%q", got)
	}
}

// A thread holding a slot runs a job fyr admitted: it is not called over.
func TestShareShadowASlotIsNotCalledOver(t *testing.T) {
	tr, td, lines, _ := budgetLog(t)
	write(t, filepath.Join(td, "memory.current"), strconv.FormatUint(900<<20, 10))
	tr.ShadowThreadBudget(map[string]bool{"t_a": true})
	if got := withWord(*lines, "budget over"); len(got) != 0 {
		t.Fatalf("%q", got)
	}
}

func TestShareShadowTheDeployAndTheProbeAreLeftAlone(t *testing.T) {
	tr, _, lines, _ := budgetLog(t)
	agents := filepath.Join(tr.root, "agents")
	for _, n := range []string{DeployDir, "probe"} {
		d := filepath.Join(agents, n)
		os.MkdirAll(d, 0o755)
		write(t, filepath.Join(d, "memory.current"), strconv.FormatUint(900<<20, 10))
	}
	tr.ShadowThreadBudget(nil)
	if got := withWord(*lines, "budget over"); len(got) != 0 {
		t.Fatalf("%q", got)
	}
}
