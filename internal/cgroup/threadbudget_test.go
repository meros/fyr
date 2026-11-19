// Decision 0032 slice 3: a thread's memory.high is a share of the agents'
// budget, and a slot raises it.
package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testBudget = uint64(1000) << 20

func budgetTree(t *testing.T, percent int) (*Tree, string, string) {
	t.Helper()
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	o.AgentsMemoryHigh = strconv.FormatUint(testBudget, 10)
	o.ThreadMemoryHighPercent = percent
	tr := Setup(o)
	if tr == nil {
		t.Fatal("no tree")
	}
	return tr, threadOf(tr, "t_a"), filepath.Join(tr.root, "agents")
}

func pages(n uint64) string {
	p := uint64(os.Getpagesize())
	return strconv.FormatUint(n/p*p, 10)
}

// high is a thread's limit; a plain directory without the file has none, as a new cgroup.
func high(t *testing.T, dir string) string {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "memory.high")); os.IsNotExist(err) {
		return "max"
	}
	return read(t, filepath.Join(dir, "memory.high"))
}

func TestB1AThreadGetsItsShare(t *testing.T) {
	tr, td, _ := budgetTree(t, 12)
	tr.ApplyThreadBudget(nil)
	if got, want := high(t, td), pages(testBudget*12/100); got != want {
		t.Fatalf("%s, want %s", got, want)
	}
}

func TestB2AKnownJobRaisesTheLimitAndTheReleaseLowersIt(t *testing.T) {
	tr, td, _ := budgetTree(t, 12)
	peak := uint64(200) << 20
	tr.ApplyThreadBudget(map[string]ThreadHold{"t_a": {Peak: peak, Known: true}})
	if got, want := high(t, td), pages(testBudget*12/100+peak*5/4); got != want {
		t.Fatalf("raised %s, want %s", got, want)
	}
	tr.ApplyThreadBudget(nil)
	if got, want := high(t, td), pages(testBudget*12/100); got != want {
		t.Fatalf("lowered %s, want %s", got, want)
	}
}

func TestB3AJobOfUnknownSizeHasNoLimit(t *testing.T) {
	tr, td, _ := budgetTree(t, 12)
	tr.ApplyThreadBudget(map[string]ThreadHold{"t_a": {}})
	if got := high(t, td); got != "max" {
		t.Fatal(got)
	}
}

func TestB4ARaiseToTheWholeBudgetIsNoLimit(t *testing.T) {
	tr, td, _ := budgetTree(t, 12)
	tr.ApplyThreadBudget(map[string]ThreadHold{"t_a": {Peak: testBudget, Known: true}})
	if got := high(t, td); got != "max" {
		t.Fatal(got)
	}
}

func TestB5NearlyFullSwapLiftsTheLimitsAndTheyComeBack(t *testing.T) {
	tr, td, agents := budgetTree(t, 12)
	write(t, filepath.Join(agents, "memory.swap.max"), "1000")
	write(t, filepath.Join(agents, "memory.swap.current"), "950")
	tr.ApplyThreadBudget(nil)
	if got := high(t, td); got != "max" {
		t.Fatalf("with full swap: %s", got)
	}
	write(t, filepath.Join(agents, "memory.swap.current"), "100")
	tr.ApplyThreadBudget(nil)
	if got, want := high(t, td), pages(testBudget*12/100); got != want {
		t.Fatalf("after: %s, want %s", got, want)
	}
}

func TestB6TurnedOffAThreadLosesItsLimit(t *testing.T) {
	tr, td, _ := budgetTree(t, 0)
	write(t, filepath.Join(td, "memory.high"), "4096")
	tr.ApplyThreadBudget(nil)
	if got := high(t, td); got != "max" {
		t.Fatal(got)
	}
}

func TestB7TheDeployAndTheProbeAreLeftAlone(t *testing.T) {
	tr, _, agents := budgetTree(t, 12)
	for _, n := range []string{DeployDir, "probe"} {
		d := filepath.Join(agents, n)
		os.MkdirAll(d, 0o755)
		tr.ApplyThreadBudget(nil)
		if _, err := os.Stat(filepath.Join(d, "memory.high")); !os.IsNotExist(err) {
			t.Fatalf("%s got a limit", n)
		}
	}
}

// budgetLog is a tree whose clock and log lines the test holds.
func budgetLog(t *testing.T, percent int) (*Tree, string, *[]string, *time.Time) {
	t.Helper()
	tr, td, _ := budgetTree(t, percent)
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

func TestB8AChangeOfLimitIsLoggedOnceWithItsReason(t *testing.T) {
	tr, _, lines, _ := budgetLog(t, 12)
	tr.ApplyThreadBudget(nil)
	tr.ApplyThreadBudget(nil)
	tr.ApplyThreadBudget(map[string]ThreadHold{"t_a": {Peak: 100 << 20, Known: true}})
	got := withWord(*lines, "budget limit thread=t_a")
	if len(got) != 2 || !strings.Contains(got[0], "why=share") || !strings.Contains(got[1], "why=slot") || !strings.Contains(got[1], "job_peak=100MB") {
		t.Fatalf("%q", got)
	}
}

func TestB9AThreadOverItsLimitIsLoggedOnceAMinuteWithTheSignals(t *testing.T) {
	tr, td, lines, now := budgetLog(t, 12)
	write(t, filepath.Join(td, "memory.current"), strconv.FormatUint(300<<20, 10))
	write(t, filepath.Join(td, "memory.stat"), "inactive_file 0\npgmajfault 100\n")
	write(t, filepath.Join(td, "memory.events"), "low 0\nhigh 5\nmax 0\n")
	write(t, filepath.Join(td, "memory.pressure"), "some avg10=40.00 avg60=0 avg300=0 total=1\nfull avg10=20.00 avg60=0 avg300=0 total=1\n")
	tr.ApplyThreadBudget(nil)
	tr.ApplyThreadBudget(nil)
	if got := withWord(*lines, "budget over"); len(got) != 1 {
		t.Fatalf("%q", got)
	}
	*now = now.Add(time.Minute)
	write(t, filepath.Join(td, "memory.stat"), "inactive_file 0\npgmajfault "+strconv.Itoa(100+60*256)+"\n")
	write(t, filepath.Join(td, "memory.events"), "low 0\nhigh 9\nmax 0\n")
	tr.ApplyThreadBudget(nil)
	got := withWord(*lines, "budget over")
	if len(got) != 2 {
		t.Fatalf("%q", got)
	}
	for _, w := range []string{"mode=on", "working=300MB", "limit=120MB", "high_events=+4", "psi_some=40", "psi_full=20", "slot=none"} {
		if !strings.Contains(got[1], w) {
			t.Fatalf("no %s in %q", w, got[1])
		}
	}
	if os.Getpagesize() == 4096 && !strings.Contains(got[1], "read_back=1MB/s") {
		t.Fatalf("%q", got[1])
	}
	if s := withWord(*lines, "budget summary"); len(s) != 2 || !strings.Contains(s[1], "over=1") {
		t.Fatalf("%q", s)
	}
}

func TestB10FileCacheDoesNotCountAsOver(t *testing.T) {
	tr, td, lines, _ := budgetLog(t, 12)
	write(t, filepath.Join(td, "memory.current"), strconv.FormatUint(300<<20, 10))
	write(t, filepath.Join(td, "memory.stat"), "inactive_file "+strconv.FormatUint(250<<20, 10)+"\n")
	tr.ApplyThreadBudget(nil)
	if got := withWord(*lines, "budget over"); len(got) != 0 {
		t.Fatalf("%q", got)
	}
}

func TestB11WhileOffItLogsWhatTheShareWouldMeetAndLimitsNothing(t *testing.T) {
	tr, td, lines, _ := budgetLog(t, 0)
	write(t, filepath.Join(td, "memory.current"), strconv.FormatUint(300<<20, 10))
	tr.ApplyThreadBudget(nil)
	got := withWord(*lines, "budget over")
	if len(got) != 1 || !strings.Contains(got[0], "mode=shadow") || !strings.Contains(got[0], "limit=120MB") {
		t.Fatalf("%q", got)
	}
	if h := high(t, td); h != "max" {
		t.Fatalf("limited while off: %s", h)
	}
	if l := withWord(*lines, "budget limit"); len(l) != 0 {
		t.Fatalf("%q", l)
	}
}

func TestB12ASlotWithoutALimitIsNotCalledOver(t *testing.T) {
	tr, td, lines, _ := budgetLog(t, 12)
	write(t, filepath.Join(td, "memory.current"), strconv.FormatUint(900<<20, 10))
	tr.ApplyThreadBudget(map[string]ThreadHold{"t_a": {}})
	if got := withWord(*lines, "budget over"); len(got) != 0 {
		t.Fatalf("%q", got)
	}
}
