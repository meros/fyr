// Decision 0032 slice 3 against the kernel: one thread's oversized work beside
// a small one, with and without the share. It needs a delegated service:
//
//	go test -c -o /tmp/cg.test ./internal/cgroup
//	systemd-run --user --wait --pipe -p Delegate=yes --setenv=FYR_REAL_CGROUP=1 \
//	  --setenv=FYR_PROOF_PYTHON=$(command -v python3) /tmp/cg.test -test.run TestRealThreadBudget -test.v -test.timeout 15m
package cgroup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

const budgetHog = `import os, sys
mb, touches = int(sys.argv[1]), int(sys.argv[2])
n, page = mb * 1024 * 1024, 4096
buf = bytearray(n)
for off in range(0, n, page):
    buf[off:off + 32] = os.urandom(32)
pages = n // page
x = os.getpid() | 1
progress = sys.argv[3] if len(sys.argv) > 3 else ""
for i in range(touches):
    x = (x * 6364136223846793005 + 1442695040888963407) & 0xFFFFFFFFFFFFFFFF
    buf[((x >> 33) % pages) * page + 40] = x & 255
    if progress and i % 100000 == 0:
        with open(progress, "w") as f:
            f.write(str(i))
`

// startHogs starts n jobs under one runner in td, as a turbo run does; each
// writes its progress to a file in dir when dir is set.
func startHogs(t *testing.T, td, script string, n, mb, touches int, dir string) chan error {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", `read go; n=$4; while [ "$n" -gt 0 ]; do "$0" -I "$1" "$2" "$3" ${5:+"$5/$n"} & n=$((n-1)); done; wait`,
		os.Getenv("FYR_PROOF_PYTHON"), script, strconv.Itoa(mb), strconv.Itoa(touches), strconv.Itoa(n), dir)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill() })
	if err := os.WriteFile(filepath.Join(td, "cgroup.procs"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(in, "go")
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	return done
}

// touched sums the progress the big jobs wrote.
func touched(dir string) int {
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	sum := 0
	for _, f := range files {
		b, _ := os.ReadFile(f)
		n, _ := strconv.Atoi(string(b))
		sum += n
	}
	return sum
}

func TestRealThreadBudget(t *testing.T) {
	if os.Getenv("FYR_REAL_CGROUP") == "" {
		t.Skip("FYR_REAL_CGROUP is not set")
	}
	const budget, share = uint64(1200) << 20, uint64(300) << 20
	const limit = 3 * time.Minute
	tr := Setup(Options{Budget: Budget{Enable: true, AgentsMemoryHigh: strconv.FormatUint(budget, 10)}, Logf: t.Logf})
	if tr == nil {
		t.Fatal("no tree")
	}
	script := filepath.Join(t.TempDir(), "hog.py")
	if err := os.WriteFile(script, []byte(budgetHog), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(name string, percent int) (time.Duration, uint64, string, float64) {
		tr.threadPercent = percent
		noisy, quiet := tr.Thread("t_noisy_"+name), tr.Thread("t_quiet_"+name)
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			for {
				select {
				case <-stop:
					return
				case <-time.After(time.Second):
					tr.ApplyThreadBudget(nil)
				}
			}
		}()
		tr.ApplyThreadBudget(nil)
		dir := t.TempDir()
		startHogs(t, noisy, script, 4, 500, 2_000_000_000, dir)
		time.Sleep(20 * time.Second)
		start := time.Now()
		before := touched(dir)
		done := startHogs(t, quiet, script, 1, 250, 30_000_000, "")
		took := limit
		select {
		case <-done:
			took = time.Since(start)
		case <-time.After(limit):
			t.Logf("%s: the small job was not done in %s", name, limit)
		}
		rate := float64(touched(dir)-before) / time.Since(start).Seconds()
		use := readBytes(filepath.Join(filepath.Dir(noisy), "memory.current"))
		high := read(t, filepath.Join(filepath.Dir(noisy), "memory.high"))
		os.WriteFile(filepath.Join(quiet, "cgroup.kill"), []byte("1"), 0o644)
		os.WriteFile(filepath.Join(noisy, "cgroup.kill"), []byte("1"), 0o644)
		time.Sleep(3 * time.Second)
		t.Logf("%s: small job %s; the big work holds %d MB in memory at %.0f touches/s, memory.high %s", name, took.Round(100*time.Millisecond), use>>20, rate, high)
		return took, use, high, rate
	}
	without, _, _, rateOff := run("off", 0)
	with, use, high, rateOn := run("on", 25)
	if rateOn > 0 {
		t.Logf("the share slows the big work %.1f times", rateOff/rateOn)
	}
	if high != strconv.FormatUint(share, 10) {
		t.Errorf("memory.high %s, want %d", high, share)
	}
	if use > share*11/10 {
		t.Errorf("the big job holds %d MB, over its share", use>>20)
	}
	// Measured, not required: two runs showed no clear gain for the small job
	// (decision 0032), so the owner tunes the share from the log.
	t.Logf("the small job took %s with the share, %s without", with.Round(100*time.Millisecond), without.Round(100*time.Millisecond))
	// A slot raises the running thread's limit, and its release lowers it.
	tr.threadPercent = 25
	td := threadOf(tr, "t_hold")
	peak := uint64(400) << 20
	tr.ApplyThreadBudget(map[string]ThreadHold{"t_hold": {Peak: peak, Known: true}})
	if got, want := read(t, filepath.Join(td, "memory.high")), strconv.FormatUint(share+peak*5/4, 10); got != want {
		t.Errorf("raised: %s, want %s", got, want)
	}
	tr.ApplyThreadBudget(nil)
	if got, want := read(t, filepath.Join(td, "memory.high")), strconv.FormatUint(share, 10); got != want {
		t.Errorf("lowered: %s, want %s", got, want)
	}
}
