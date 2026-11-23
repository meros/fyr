package deploy

import (
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/meros/fyr/internal/launchenv"
)

// The test binary doubles as a fake daemon: FYR_FAKE_DAEMON=<mode>.
//
//	ok     becomes ready, then exits 0 when the file "stop" appears
//	deploy becomes ready, then exits 75 (switch to next)
//	crash  exits 1 at once
//	slow   never becomes ready
func TestMain(m *testing.M) {
	if mode := os.Getenv("FYR_FAKE_DAEMON"); mode != "" {
		fakeDaemon(mode)
		return
	}
	os.Exit(m.Run())
}

func fakeDaemon(mode string) {
	if mode != "runner" && mode != "stray" {
		launchenv.Init(filepath.Dir(os.Getenv(launchenv.ReadyFile)), "")
	}
	switch mode {
	case "runner":
		liveDaemon()
	case "stray":
		strayDaemon()
		os.Exit(0)
	case "crash":
		os.Exit(1)
	case "slow":
		time.Sleep(time.Hour)
	case "hup":
		// as fyrd (V0.180): SIGHUP exits 75, so the launcher restarts it; each start is a line.
		f, _ := os.OpenFile(os.Getenv("FYR_STARTS_FILE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		f.WriteString(strconv.Itoa(os.Getpid()) + "\n")
		f.Close()
		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)
		MarkReady()
		for i := 0; i < 1000; i++ {
			select {
			case <-hup:
				os.Exit(ExitDeploy)
			case <-time.After(10 * time.Millisecond):
			}
			if _, err := os.Stat(os.Getenv("FYR_STOP_FILE")); err == nil {
				os.Exit(0)
			}
		}
		os.Exit(0)
	case "healthy", "sick", "latecrash":
		// #148: serve /healthz (200, or 503 when sick) and say where.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			os.Exit(3)
		}
		check := func() error { return nil }
		if mode == "sick" {
			check = func() error { return errors.New("the database does not answer") }
		}
		go http.Serve(ln, Health(http.NotFoundHandler(), check))
		MarkReady(ln.Addr().String())
		if mode == "latecrash" {
			time.Sleep(1500 * time.Millisecond) // past the 500 ms probation
			os.Exit(1)
		}
		for i := 0; i < 1000; i++ {
			if _, err := os.Stat(os.Getenv("FYR_STOP_FILE")); err == nil {
				os.Exit(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
		os.Exit(0)
	}
	MarkReady()
	if mode == "deploy" {
		time.Sleep(50 * time.Millisecond)
		os.Exit(ExitDeploy)
	}
	for i := 0; i < 400; i++ {
		if _, err := os.Stat(os.Getenv("FYR_STOP_FILE")); err == nil {
			os.Exit(0)
		}
		time.Sleep(10 * time.Millisecond)
	}
	os.Exit(0)
}

// build makes a build dir whose "fyrd" runs this test binary in a mode.
func build(t *testing.T, data, id, mode string) string {
	dir := filepath.Join(data, "builds", id)
	os.MkdirAll(dir, 0o755)
	script := "#!/bin/sh\nFYR_FAKE_DAEMON=" + mode + " exec " + strconv.Quote(os.Args[0]) + " \"$@\"\n"
	os.WriteFile(filepath.Join(dir, "fyrd"), []byte(script), 0o755)
	return dir
}

func link(t *testing.T, data, name, id string) {
	os.Remove(filepath.Join(data, name))
	if err := os.Symlink(filepath.Join("builds", id), filepath.Join(data, name)); err != nil {
		t.Fatal(err)
	}
}

func target(data, name string) string {
	p, _ := os.Readlink(filepath.Join(data, name))
	return filepath.Base(p)
}

func launcher(data string) *Launcher {
	// ReadyTimeout is loose: on a busy machine (a full `make test` on macOS) a fake daemon takes seconds to start.
	return &Launcher{Data: data, ReadyTimeout: 10 * time.Second, Probation: 500 * time.Millisecond,
		Env: []string{"FYR_STOP_FILE=" + filepath.Join(data, "stop")}}
}

// SPEC 0.5 V0.13: exit 75 promotes next to current and keeps previous.
func TestLauncherPromotesNext(t *testing.T) {
	data := t.TempDir()
	build(t, data, "a", "deploy")
	build(t, data, "b", "ok")
	link(t, data, "current", "a")
	link(t, data, "next", "b")
	l := launcher(data)
	l.maxStarts = 5 // a rolled-back b would restart a (exit 75) forever
	// Stop b once it is current and past its probation, however slow the starts.
	go func() {
		for i := 0; i < 1000 && target(data, "current") != "b"; i++ {
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(l.Probation + 300*time.Millisecond)
		os.WriteFile(filepath.Join(data, "stop"), nil, 0o644)
	}()
	if err := l.Run(); err != nil {
		t.Fatal(err)
	}
	if target(data, "current") != "b" || target(data, "previous") != "a" {
		t.Fatalf("current=%s previous=%s", target(data, "current"), target(data, "previous"))
	}
	if _, err := os.Lstat(filepath.Join(data, "next")); err == nil {
		t.Fatal("next link left behind")
	}
}

// SPEC 0.5 V0.13: a next that crashes in probation is rolled back.
func TestLauncherRollsBackCrash(t *testing.T) {
	data := t.TempDir()
	build(t, data, "a", "deploy")
	build(t, data, "b", "crash")
	link(t, data, "current", "a")
	link(t, data, "next", "b")
	l := launcher(data)
	l.maxStarts = 2 // a, then b (crash) → back to a, which deploys again with no next → stops
	// b's start on a busy machine (a fresh test binary on macOS) can outlast 500 ms; its crash must land in probation.
	l.Probation = 5 * time.Second
	l.Run()
	if target(data, "current") != "a" {
		t.Fatalf("current=%s after a crashed deploy", target(data, "current"))
	}
	b, err := os.ReadFile(filepath.Join(data, "deploy-failed.json"))
	if err != nil || len(b) == 0 {
		t.Fatalf("no rollback report: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, "builds", "a", "fyrd")); err != nil {
		t.Fatal("previous build was deleted")
	}
}

// SPEC 0.5 V0.13: a next that never becomes ready is rolled back.
func TestLauncherRollsBackNotReady(t *testing.T) {
	data := t.TempDir()
	build(t, data, "a", "deploy")
	build(t, data, "b", "slow")
	link(t, data, "current", "a")
	link(t, data, "next", "b")
	l := launcher(data)
	l.ReadyTimeout = 300 * time.Millisecond
	l.maxStarts = 2
	l.Run()
	if target(data, "current") != "a" {
		t.Fatalf("current=%s", target(data, "current"))
	}
}

// seedLauncher has a store-like seed build outside Data.
func seedLauncher(t *testing.T, data, seedMode string) *Launcher {
	seedRoot := t.TempDir()
	build(t, seedRoot, "seed", seedMode)
	l := launcher(data)
	l.Seed = filepath.Join(seedRoot, "builds", "seed", "fyrd")
	return l
}

// SPEC 12.6, decision 0007: without a current build the launcher runs the seed.
func TestLauncherRunsSeedWithoutCurrent(t *testing.T) {
	data := t.TempDir()
	l := seedLauncher(t, data, "ok")
	go func() { time.Sleep(300 * time.Millisecond); os.WriteFile(filepath.Join(data, "stop"), nil, 0o644) }()
	if err := l.Run(); err != nil {
		t.Fatal(err)
	}
	if l.lastExe != l.Seed {
		t.Fatalf("ran %q", l.lastExe)
	}
}

// Decision 0007: the pin file forces the seed even when current exists.
func TestLauncherPinForcesSeed(t *testing.T) {
	data := t.TempDir()
	build(t, data, "a", "crash")
	link(t, data, "current", "a")
	os.WriteFile(filepath.Join(data, "pin"), nil, 0o644)
	l := seedLauncher(t, data, "ok")
	go func() { time.Sleep(300 * time.Millisecond); os.WriteFile(filepath.Join(data, "stop"), nil, 0o644) }()
	if err := l.Run(); err != nil {
		t.Fatal(err)
	}
	if l.lastExe != l.Seed {
		t.Fatalf("ran %q", l.lastExe)
	}
}

// Decision 0007: a failed promote with no previous build falls back to the seed.
func TestLauncherRollsBackToSeed(t *testing.T) {
	data := t.TempDir()
	build(t, data, "b", "crash")
	link(t, data, "next", "b")
	l := seedLauncher(t, data, "deploy")
	l.maxStarts = 2 // seed (deploys) → b (crashes) → rollback
	// b's start on a busy runner can outlast the 500 ms default; its crash must land in probation (V0.flaky-rollback-seed).
	l.Probation = 5 * time.Second
	l.Run()
	if _, err := os.Lstat(filepath.Join(data, "current")); err == nil {
		t.Fatalf("current still points at %s after a crash with no previous", target(data, "current"))
	}
	if _, err := os.Stat(filepath.Join(data, "deploy-failed.json")); err != nil {
		t.Fatal("no rollback report")
	}
}

// #148: a next that is ready but answers /healthz with 503 is rolled back,
// and the report says why.
func TestLauncherRollsBackUnhealthy(t *testing.T) {
	data := t.TempDir()
	build(t, data, "a", "deploy")
	build(t, data, "b", "sick")
	link(t, data, "current", "a")
	link(t, data, "next", "b")
	l := launcher(data)
	// The health probe retries a 503 until the deadline, so this run lasts about the deadline. 3 s was too short
	// under load (load average 31: no answer came before it, so the report said "context deadline exceeded"); 8 s is
	// generous and stays under the fake's 10 s life. The report keeps the last answer (probeHealth), so the
	// connection errors after the fake exits cannot hide the 503's cause.
	l.ReadyTimeout = 8 * time.Second
	l.maxStarts = 2
	l.Run()
	if target(data, "current") != "a" {
		t.Fatalf("current=%s after an unhealthy deploy", target(data, "current"))
	}
	b, _ := os.ReadFile(filepath.Join(data, "deploy-failed.json"))
	if !strings.Contains(string(b), "not healthy") || !strings.Contains(string(b), "database") {
		t.Fatalf("rollback report %s", b)
	}
}

// #148: a next that passes its probation and then crashes within the watch
// window goes back to previous instead of crash-looping.
func TestLauncherRollsBackLateCrash(t *testing.T) {
	data := t.TempDir()
	build(t, data, "a", "deploy")
	build(t, data, "b", "latecrash")
	link(t, data, "current", "a")
	link(t, data, "next", "b")
	l := launcher(data)
	l.maxStarts = 2 // a (deploys) → b (crashes late) → rollback, stop
	l.Run()
	if target(data, "current") != "a" {
		t.Fatalf("current=%s after a late crash", target(data, "current"))
	}
	b, _ := os.ReadFile(filepath.Join(data, "deploy-failed.json"))
	if !strings.Contains(string(b), "crashed with code 1") {
		t.Fatalf("rollback report %s", b)
	}
	if target(data, "previous") == "b" {
		t.Fatal("previous points at the failed build")
	}
}

// #148: a healthy next gets probation-ok with its build once its probation
// ended.
func TestLauncherWritesProbationOK(t *testing.T) {
	data := t.TempDir()
	build(t, data, "a", "deploy")
	build(t, data, "b", "healthy")
	link(t, data, "current", "a")
	link(t, data, "next", "b")
	l := launcher(data)
	l.maxStarts = 5
	go func() {
		for i := 0; i < 1000; i++ {
			if b, err := os.ReadFile(filepath.Join(data, ProbationOK)); err == nil && filepath.Base(string(b)) == "b" {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		os.WriteFile(filepath.Join(data, "stop"), nil, 0o644)
	}()
	if err := l.Run(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(data, ProbationOK))
	if err != nil || filepath.Base(string(b)) != "b" || target(data, "current") != "b" {
		t.Fatalf("probation-ok %q (%v), current=%s", b, err, target(data, "current"))
	}
}

// V0.survive-update: a launcher restarted during a switch's probation starts
// the new current without a switch; it still writes probation-ok for it once
// it has run its probation, so the deploy's report is not left waiting.
func TestLauncherRestartedInProbationWritesProbationOK(t *testing.T) {
	data := t.TempDir()
	build(t, data, "a", "ok")
	build(t, data, "b", "healthy")
	link(t, data, "previous", "a")
	link(t, data, "current", "b")
	os.WriteFile(filepath.Join(data, ProbationOK), []byte("builds/a"), 0o600) // the deploy before
	l := launcher(data)
	l.maxStarts = 1
	go func() {
		for i := 0; i < 1000; i++ {
			if b, err := os.ReadFile(filepath.Join(data, ProbationOK)); err == nil && filepath.Base(string(b)) == "b" {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		os.WriteFile(filepath.Join(data, "stop"), nil, 0o644)
	}()
	if err := l.Run(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(data, ProbationOK))
	if err != nil || filepath.Base(string(b)) != "b" || target(data, "current") != "b" {
		t.Fatalf("probation-ok %q (%v), current=%s", b, err, target(data, "current"))
	}
}

// SPEC V0.180 (#264): SIGHUP to the launcher is a reload: fyrd gets it, exits 75, and
// the launcher starts it again (the runner, and with it the agents, stays) instead
// of stopping as it does on SIGTERM.
func TestLauncherReloadsOnSIGHUP(t *testing.T) {
	data := t.TempDir()
	build(t, data, "a", "hup")
	link(t, data, "current", "a")
	l := launcher(data)
	starts := filepath.Join(data, "starts")
	l.Env = append(l.Env, "FYR_STARTS_FILE="+starts)
	l.maxStarts = 3
	lines := func() int {
		b, _ := os.ReadFile(starts)
		return strings.Count(string(b), "\n")
	}
	done := make(chan error, 1)
	go func() { done <- l.Run() }()
	wait := func(n int) {
		for i := 0; i < 1000 && lines() < n; i++ {
			time.Sleep(10 * time.Millisecond)
		}
		if lines() < n {
			t.Fatalf("%d starts, want %d", lines(), n)
		}
	}
	wait(1)
	time.Sleep(100 * time.Millisecond) // past signal.Notify in the fake
	l.Reload(syscall.SIGHUP)
	wait(2)
	select {
	case err := <-done:
		t.Fatalf("Run returned after a reload: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	os.WriteFile(filepath.Join(data, "stop"), nil, 0o644)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if target(data, "current") != "a" {
		t.Errorf("current=%s", target(data, "current"))
	}
}
