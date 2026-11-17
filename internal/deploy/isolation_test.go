package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/meros/fyr/internal/launchenv"
	"github.com/meros/fyr/internal/runner"
)

// liveDaemon is the fake fyrd of the live install (mode "runner"): it
// takes the launcher's variables as fyrd does, starts one agent process in
// the runner and holds its link. It writes:
//
//	env   the environment it was started with: what an agent's shell had
//	      before #167
//	agent the agent process's pid
//	lost  why its link to the agent process ended, if it did
func liveDaemon() {
	dir := os.Getenv("FYR_TEST_DIR")
	os.WriteFile(filepath.Join(dir, "env"), []byte(strings.Join(os.Environ(), "\n")), 0o600)
	e, _ := launchenv.Init(os.Getenv("FYR_TEST_DATA"), "")
	if e.Runner == "" {
		os.WriteFile(filepath.Join(dir, "lost"), []byte("no runner"), 0o600)
		os.Exit(4)
	}
	rc := &runner.Client{Path: e.Runner}
	sleep, _ := exec.LookPath("sleep")
	conn, err := rc.Spawn(runner.Spawn{ID: "live", Key: "t-live", Binary: sleep, Args: []string{"60"}, Env: launchenv.Scrub(os.Environ())})
	if err != nil {
		os.WriteFile(filepath.Join(dir, "lost"), []byte("spawn: "+err.Error()), 0o600)
		os.Exit(4)
	}
	os.WriteFile(filepath.Join(dir, "agent"), []byte(strconv.Itoa(conn.Pid())), 0o600)
	go func() {
		_, _, err := conn.ReadLine()
		os.WriteFile(filepath.Join(dir, "lost"), []byte(err.Error()+": "+errText(conn.Wait())), 0o600)
	}()
	MarkReady("127.0.0.1:1")
	for i := 0; i < 1000; i++ {
		if _, err := os.Stat(os.Getenv("FYR_STOP_FILE")); err == nil {
			conn.Kill()
			conn.Forget()
			os.Exit(0)
		}
		time.Sleep(10 * time.Millisecond)
	}
	os.Exit(0)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// strayDaemon is what an agent's `fyrd serve -data <scratch>` did on 16
// Nov (mode "stray"): it takes the variables it inherited, says it is
// ready, and adopts the runner's processes. None of their threads is in
// its database, so Adopt ends every one (agent.Manager.attach: "no
// thread").
func strayDaemon() {
	e, _ := launchenv.Init(os.Getenv("FYR_TEST_DATA"), "")
	MarkReady("127.0.0.1:2")
	if e.Runner == "" {
		return
	}
	rc := &runner.Client{Path: e.Runner}
	procs, _ := rc.List()
	for _, p := range procs {
		if conn, err := rc.Attach(p.ID); err == nil {
			conn.Kill()
			conn.Forget()
		}
	}
}

// #167: an agent-like child with the live fyrd's whole environment (what
// every agent process inherited before the fix) runs a fyrd of its own.
// The live runner keeps its other processes, the live fyrd keeps its link,
// and the live ready file still names the live fyrd.
func TestStrayFyrdLeavesTheLiveRunnerAlone(t *testing.T) {
	data, dir := t.TempDir(), t.TempDir()
	build(t, data, "a", "runner")
	link(t, data, "current", "a")
	l := launcher(data)
	l.Env = append(l.Env, "FYR_TEST_DIR="+dir, "FYR_TEST_DATA="+data)
	srv, err := l.ServeRunner(t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close(time.Second)
	done := make(chan error, 1)
	go func() { done <- l.Run() }()
	defer func() {
		os.WriteFile(filepath.Join(data, "stop"), nil, 0o644)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("the launcher did not stop")
		}
	}()

	agentFile, ready := filepath.Join(dir, "agent"), filepath.Join(data, "ready")
	for i := 0; i < 1000; i++ {
		if b, _ := os.ReadFile(filepath.Join(dir, "lost")); len(b) > 0 {
			t.Fatalf("the live fyrd has no agent process: %s", b)
		}
		_, e1 := os.Stat(agentFile)
		_, e2 := os.Stat(ready)
		if e1 == nil && e2 == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	b, err := os.ReadFile(agentFile)
	if err != nil {
		t.Fatal("the live fyrd did not start its agent process")
	}
	agent, _ := strconv.Atoi(string(b))
	liveReady, _ := os.ReadFile(ready)
	inherited, _ := os.ReadFile(filepath.Join(dir, "env"))

	// The agent-like child: not the launcher's child, the live environment,
	// a scratch data folder of its own.
	stray := exec.Command(os.Args[0])
	stray.Env = append(strings.Split(string(inherited), "\n"), "FYR_FAKE_DAEMON=stray", "FYR_TEST_DATA="+t.TempDir())
	if out, err := stray.CombinedOutput(); err != nil {
		t.Fatalf("stray: %v\n%s", err, out)
	}
	time.Sleep(300 * time.Millisecond)

	if b, _ := os.ReadFile(filepath.Join(dir, "lost")); len(b) > 0 {
		t.Errorf("the live fyrd lost its link: %s", b)
	}
	if err := syscall.Kill(agent, 0); err != nil {
		t.Errorf("the live agent process %d is gone: %v", agent, err)
	}
	if now, _ := os.ReadFile(ready); string(now) != string(liveReady) {
		t.Errorf("the live ready file changed from %q to %q", liveReady, now)
	}
}

// #167: the launcher trusts a ready file only from the fyrd it started.
func TestLauncherIgnoresAnotherProcesssReadyFile(t *testing.T) {
	data := t.TempDir()
	build(t, data, "a", "deploy")
	build(t, data, "b", "slow")
	link(t, data, "current", "a")
	link(t, data, "next", "b")
	l := launcher(data)
	l.ReadyTimeout = time.Second
	l.maxStarts = 3
	// While b starts and never gets ready, another process writes ready.
	go func() {
		for i := 0; i < 500 && target(data, "current") != "b"; i++ {
			time.Sleep(10 * time.Millisecond)
		}
		for i := 0; i < 50; i++ {
			os.WriteFile(filepath.Join(data, "ready"), []byte("1"), 0o600)
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(2 * time.Second)
		l.Signal(syscall.SIGTERM) // a trusted b would run for an hour
	}()
	l.Run()
	if target(data, "current") != "a" {
		t.Fatalf("a build that never got ready was trusted on another process's ready file: current=%s", target(data, "current"))
	}
}
