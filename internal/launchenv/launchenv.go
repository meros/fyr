// Package launchenv is how a fyrd knows that the launcher of its own
// install started it (#167). The launcher hands its fyrd the variables
// below; they name the live install: its runner's socket and its ready
// file. Every process fyrd started inherited them, so an agent's
// `fyrd serve -data scratch` or test took the live runner's processes
// over and ended them, and wrote the live ready file.
//
// Now fyrd takes them once at start (Init): it removes them from its own
// environment, so no process it starts inherits them, and keeps each only
// when it names fyrd's own data folder and fyrd's parent is the launcher
// that holds that runner. The launcher's runner also serves only the fyrd
// it started (runner.Server.Allow).
package launchenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/runner"
)

const (
	// Runner is the runner's socket (V0.90).
	Runner = "FYR_RUNNER"
	// Launcher is "1" under the launcher: a deploy can switch builds.
	Launcher = "FYR_LAUNCHER"
	// ReadyFile is where fyrd says it serves (V0.13, #148).
	ReadyFile = "FYR_READY_FILE"
	// LauncherPid is the launcher's pid (#167). A launcher from before
	// #167 does not set it.
	LauncherPid = "FYR_LAUNCHER_PID"
)

// Names are the variables that name the live install.
var Names = []string{Runner, Launcher, ReadyFile, LauncherPid}

// Env is what the launcher said.
type Env struct {
	Runner, ReadyFile string
	Launcher          bool
	LauncherPid       int
}

// Take reads the variables and removes them from this process's
// environment.
func Take() Env {
	e := Env{Runner: os.Getenv(Runner), ReadyFile: os.Getenv(ReadyFile), Launcher: os.Getenv(Launcher) == "1"}
	e.LauncherPid, _ = strconv.Atoi(os.Getenv(LauncherPid))
	for _, n := range Names {
		os.Unsetenv(n)
	}
	return e
}

// Scrub returns env without the variables.
func Scrub(env []string) []string {
	return Without(env, Names)
}

// ClaudeEnv is the environment of a claude process: this process's own,
// without the launcher's variables (#167) and without a credential or
// endpoint variable (V0.agent-env: the adapter sets those it needs, in
// extra), CLAUDE_CODE_ENTRYPOINT=sdk-go, then extra.
func ClaudeEnv(extra ...string) []string {
	drop := append(append([]string{}, Names...), cli.CredentialVars...)
	return append(append(Without(os.Environ(), drop), "CLAUDE_CODE_ENTRYPOINT=sdk-go"), extra...)
}

// Without returns env without the named variables.
func Without(env []string, names []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !isName(name, names) {
			out = append(out, kv)
		}
	}
	return out
}

func isName(n string, names []string) bool {
	for _, m := range names {
		if n == m {
			return true
		}
	}
	return false
}

// Check keeps what belongs to the install with data folder data and a
// parent (ppid) that is its launcher; dropped says what it left out and
// why. peer is the pid that listens on a socket (runner.PeerPid).
//
//   - The parent is the launcher: it is LauncherPid; from a launcher before
//     #167, the parent holds the runner's socket. Otherwise nothing is kept.
//   - Runner: the socket is this data folder's, held by the parent.
//   - ReadyFile: <data>/ready.
//
// Where the OS gives no peer pid, the data folder alone decides.
func (e Env) Check(data string, ppid int, peer func(string) (int, error)) (Env, []string) {
	if e == (Env{}) {
		return e, nil
	}
	var dropped []string
	drop := func(what, why string) { dropped = append(dropped, what+": "+why) }
	held := func(sock string) (bool, string) {
		pid, err := peer(sock)
		switch {
		case errors.Is(err, errors.ErrUnsupported):
			return true, ""
		case err != nil:
			return false, err.Error()
		case pid != ppid:
			return false, fmt.Sprintf("pid %d holds it, not this fyrd's parent %d", pid, ppid)
		}
		return true, ""
	}
	// Is the parent the launcher?
	switch {
	case e.LauncherPid != 0 && e.LauncherPid != ppid:
		drop("all", fmt.Sprintf("the launcher is pid %d, this fyrd's parent is %d", e.LauncherPid, ppid))
		return Env{}, dropped
	case e.LauncherPid == 0 && e.Runner != "":
		if ok, why := held(e.Runner); !ok {
			drop("all", "the runner "+e.Runner+" is not this fyrd's launcher's: "+why)
			return Env{}, dropped
		}
	}
	out := e
	if e.Runner != "" {
		if want := runner.SocketPath(data); abs(e.Runner) != abs(want) {
			out.Runner = ""
			drop(Runner, e.Runner+" is not this data folder's runner "+want)
		} else if ok, why := held(e.Runner); !ok {
			out.Runner = ""
			drop(Runner, why)
		}
	}
	if e.ReadyFile != "" {
		if want := filepath.Join(data, "ready"); abs(e.ReadyFile) != abs(want) {
			out.ReadyFile = ""
			drop(ReadyFile, e.ReadyFile+" is not this data folder's "+want)
		}
	}
	if out.Runner == "" && e.Runner != "" || out.ReadyFile == "" && e.ReadyFile != "" {
		// Another install's folder: this fyrd cannot switch it either.
		out.Launcher = false
	}
	return out, dropped
}

func abs(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return filepath.Clean(p)
}

var (
	mu      sync.Mutex
	current Env
)

// Set makes e what Current returns.
func Set(e Env) {
	mu.Lock()
	current = e
	mu.Unlock()
}

// Current is what Init kept.
func Current() Env {
	mu.Lock()
	defer mu.Unlock()
	return current
}

// Init is Take, Check against data and this process's parent, and Set.
// force, when not empty, is a runner socket to use without the checks
// (fyrd -runner, told explicitly).
func Init(data, force string) (Env, []string) {
	e, dropped := Take().Check(data, os.Getppid(), runner.PeerPid)
	if force != "" {
		e.Runner = force
	}
	Set(e)
	return e, dropped
}
