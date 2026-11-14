package deploy

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
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
	switch mode {
	case "crash":
		os.Exit(1)
	case "slow":
		time.Sleep(time.Hour)
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
	return &Launcher{Data: data, ReadyTimeout: 2 * time.Second, Probation: 500 * time.Millisecond,
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
	go func() { time.Sleep(1500 * time.Millisecond); os.WriteFile(filepath.Join(data, "stop"), nil, 0o644) }()
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
	l.Run()
	if _, err := os.Lstat(filepath.Join(data, "current")); err == nil {
		t.Fatalf("current still points at %s after a crash with no previous", target(data, "current"))
	}
	if _, err := os.Stat(filepath.Join(data, "deploy-failed.json")); err != nil {
		t.Fatal("no rollback report")
	}
}
