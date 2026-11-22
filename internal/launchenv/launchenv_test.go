// #167: a fyrd uses the launcher's variables only when they name its own
// install and its parent is that install's launcher, and no process it
// starts inherits them.
package launchenv

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/meros/fyr/internal/runner"
)

func TestTakeRemovesTheVars(t *testing.T) {
	t.Setenv(Runner, "/live/runner.sock")
	t.Setenv(Launcher, "1")
	t.Setenv(ReadyFile, "/live/ready")
	t.Setenv(LauncherPid, "42")
	e := Take()
	if e.Runner != "/live/runner.sock" || !e.Launcher || e.ReadyFile != "/live/ready" || e.LauncherPid != 42 {
		t.Fatalf("Take read %+v", e)
	}
	for _, n := range Names {
		if v, ok := os.LookupEnv(n); ok {
			t.Errorf("%s=%s is still in the environment", n, v)
		}
	}
}

func TestScrub(t *testing.T) {
	in := []string{"PATH=/bin", "FYR_RUNNER=/live/runner.sock", "FYR_LAUNCHER=1", "FYR_READY_FILE=/live/ready", "FYR_LAUNCHER_PID=42", "FYR_FAKE_CLAUDE=1"}
	got := Scrub(in)
	if !slices.Equal(got, []string{"PATH=/bin", "FYR_FAKE_CLAUDE=1"}) {
		t.Fatalf("Scrub: %v", got)
	}
}

// V0.agent-env: Without drops just the names it is given, keeping the rest.
func TestWithoutDropsOnlyTheNamesGiven(t *testing.T) {
	in := []string{"PATH=/bin", "ANTHROPIC_BASE_URL=https://somewhere.example", "ANTHROPIC_MODEL=m", "HOME=/home/me"}
	got := Without(in, []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL"})
	if !slices.Equal(got, []string{"PATH=/bin", "HOME=/home/me"}) {
		t.Fatalf("Without: %v", got)
	}
	if got := Without(in, nil); len(got) != len(in) {
		t.Fatalf("Without(nil): %v", got)
	}
}

func live(data string, launcher int) Env {
	return Env{Runner: runner.SocketPath(data), ReadyFile: filepath.Join(data, "ready"), Launcher: true, LauncherPid: launcher}
}

func TestCheckKeepsItsOwnInstall(t *testing.T) {
	data := t.TempDir()
	peer := func(string) (int, error) { return 100, nil }
	// A new launcher (it names its pid) and an old one (it does not).
	for _, pid := range []int{100, 0} {
		e, dropped := live(data, pid).Check(data, 100, peer)
		if e.Runner == "" || e.ReadyFile == "" || !e.Launcher || len(dropped) > 0 {
			t.Fatalf("launcher pid %d: own install dropped: %+v %v", pid, e, dropped)
		}
	}
}

// The incident of 16 Nov: an agent's shell runs `fyrd serve -data b` with
// the live install's variables.
func TestCheckDropsAnotherInstall(t *testing.T) {
	liveData, data := t.TempDir(), t.TempDir()
	peer := func(string) (int, error) { return 100, nil } // the live launcher
	e, dropped := live(liveData, 100).Check(data, 4242, peer)
	if e != (Env{}) || len(dropped) == 0 {
		t.Fatalf("another install's variables kept: %+v", e)
	}
	// Same data folder (fyrd serve with the default -data), but the parent
	// is an agent's shell, not the launcher: old launcher and new.
	for _, pid := range []int{100, 0} {
		if e, _ := live(liveData, pid).Check(liveData, 4242, peer); e != (Env{}) {
			t.Fatalf("launcher pid %d: kept with a parent that is not the launcher: %+v", pid, e)
		}
	}
	// A runner socket that names this data folder but is not held by the
	// parent.
	other := func(string) (int, error) { return 7, nil }
	if e, _ := live(data, 0).Check(data, 100, other); e.Runner != "" || e.Launcher {
		t.Fatalf("a runner held by pid 7 was kept for parent 100: %+v", e)
	}
}

func TestCheckWithoutPeerCredentials(t *testing.T) {
	data := t.TempDir()
	none := func(string) (int, error) { return 0, errors.ErrUnsupported }
	if e, _ := live(data, 100).Check(data, 100, none); e.Runner == "" || !e.Launcher {
		t.Fatalf("own install dropped where the OS has no peer pid: %+v", e)
	}
	if e, _ := live(t.TempDir(), 100).Check(data, 100, none); e.Runner != "" || e.ReadyFile != "" {
		t.Fatalf("another data folder's runner kept: %+v", e)
	}
}

func TestInitForce(t *testing.T) {
	t.Setenv(Runner, "/somewhere/runner.sock")
	e, _ := Init(t.TempDir(), "/explicit/runner.sock")
	if e.Runner != "/explicit/runner.sock" {
		t.Fatalf("an explicit runner was not used: %+v", e)
	}
}

func TestClaudeEnv(t *testing.T) {
	t.Setenv(Runner, "/live.sock")
	t.Setenv("ANTHROPIC_API_KEY", "k")
	t.Setenv("HOME_KEEP", "1")
	env := ClaudeEnv("X=1")
	has := func(kv string) bool {
		for _, e := range env {
			if e == kv {
				return true
			}
		}
		return false
	}
	for _, e := range env {
		if strings.HasPrefix(e, Runner+"=") || strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
			t.Fatalf("leaked %s", e)
		}
	}
	if !has("HOME_KEEP=1") || !has("CLAUDE_CODE_ENTRYPOINT=sdk-go") || env[len(env)-1] != "X=1" {
		t.Fatalf("env %v", env)
	}
}
