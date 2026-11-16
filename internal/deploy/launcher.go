// Package deploy lets Fyr replace itself with a build of its own repo
// (SPEC 0.5 V0.13): the Deployer in the daemon builds, tests, drains and
// asks to switch; the Launcher outside it switches and rolls back.
package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// ExitDeploy is the daemon's exit code for "switch to next".
const ExitDeploy = 75

// MarkReady tells the launcher that the daemon serves requests. With addr
// (host:port of the loopback listener) the launcher also asks its
// /healthz before it trusts a new build (#148).
func MarkReady(addr ...string) {
	if p := os.Getenv("FYR_READY_FILE"); p != "" {
		s := fmt.Sprint(os.Getpid())
		if len(addr) > 0 && addr[0] != "" {
			s += " " + addr[0]
		}
		os.WriteFile(p, []byte(s), 0o600)
	}
}

// ProbationOK is the file the launcher writes, holding the build's link
// target, once a promoted build has run through its probation (#148). The
// daemon reports a deploy's success only then.
const ProbationOK = "probation-ok"

// Launcher runs <Data>/current/fyrd and switches builds. It holds the
// links current, previous and next, each pointing into <Data>/builds.
type Launcher struct {
	Data         string
	Args         []string // passed to fyrd
	Env          []string // added to the environment
	ReadyTimeout time.Duration
	Probation    time.Duration
	// Watch: a promoted build that crashes this long after its switch is
	// rolled back too, instead of crash-looping (#148). Default 10 min.
	Watch time.Duration
	// Seed is a known-good fyrd outside Data (the Nix store path from the
	// NixOS module, decision 0007). It runs when there is no current build,
	// when the file <Data>/pin exists, and after a rollback with no previous.
	Seed string

	mu        sync.Mutex
	child     *os.Process
	stopping  bool
	maxStarts int    // tests only
	lastExe   string // tests only
}

func (l *Launcher) path(name string) string { return filepath.Join(l.Data, name) }

func (l *Launcher) exists(name string) bool {
	_, err := os.Stat(l.path(name))
	return err == nil
}

// relink points name at the build that from points at, atomically.
func (l *Launcher) relink(name, from string) error {
	target, err := os.Readlink(l.path(from))
	if err != nil {
		return err
	}
	tmp := l.path(name + ".tmp")
	os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, l.path(name))
}

// Signal forwards a signal to the daemon and makes Run return after it exits.
func (l *Launcher) Signal(sig os.Signal) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stopping = true
	if l.child != nil {
		l.child.Signal(sig)
	}
}

type failure struct {
	Build  string    `json:"build"`
	Reason string    `json:"reason"`
	Log    string    `json:"log,omitempty"`
	At     time.Time `json:"at"`
}

// Run starts the current build and keeps it running until it exits 0 or is
// signalled. Exit 75 promotes next; a promoted build that is not ready in
// ReadyTimeout or exits within Probation is replaced by previous.
func (l *Launcher) Run() error {
	if l.ReadyTimeout == 0 {
		l.ReadyTimeout = 20 * time.Second
	}
	if l.Probation == 0 {
		l.Probation = 60 * time.Second
	}
	if l.Watch == 0 {
		l.Watch = 10 * time.Minute
	}
	promoted := false
	// watched is the build switched to last and until when its crash rolls
	// back (#148).
	watched, watchUntil := "", time.Time{}
	starts, crashes := 0, []time.Time{}
	for {
		if l.maxStarts > 0 && starts >= l.maxStarts {
			return nil
		}
		exe, build := "", "seed"
		switch {
		case !l.exists("pin") && l.exists("current"):
			exe = filepath.Join(l.path("current"), "fyrd")
			build, _ = os.Readlink(l.path("current"))
		case l.Seed != "":
			exe = l.Seed
		default:
			return errors.New("no current build and no seed: " + l.path("current"))
		}
		l.lastExe = exe
		ready := l.path("ready")
		os.Remove(ready)
		logPath := l.path("daemon.log")
		logf, _ := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		cmd := exec.Command(exe, l.Args...)
		cmd.Env = append(append(os.Environ(), "FYR_LAUNCHER=1", "FYR_READY_FILE="+ready), l.Env...)
		cmd.Stdout, cmd.Stderr = logf, logf
		if err := cmd.Start(); err != nil {
			logf.Close()
			if promoted {
				l.rollback(build, "could not start: "+err.Error(), logPath)
				promoted = false
				continue
			}
			return err
		}
		starts++
		l.mu.Lock()
		l.child = cmd.Process
		l.mu.Unlock()
		started := time.Now()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		stopped := make(chan struct{}) // closed once this start's process exited

		var err error
		exited := false
		if promoted {
			deadline := time.After(l.ReadyTimeout)
		waitReady:
			for {
				select {
				case err = <-done:
					exited = true
					break waitReady
				case <-deadline:
					cmd.Process.Kill()
					<-done
					logf.Close()
					l.rollback(build, fmt.Sprintf("not ready after %s", l.ReadyTimeout), logPath)
					promoted = false
					goto next
				case <-time.After(20 * time.Millisecond):
					if _, e := os.Stat(ready); e == nil {
						break waitReady
					}
				}
			}
			// #148: a build that wrote its address must also answer
			// /healthz within what is left of the ready timeout.
			if addr := readyAddr(ready); !exited && addr != "" {
				left := l.ReadyTimeout - time.Since(started)
				if left < 2*time.Second {
					left = 2 * time.Second
				}
				hctx, cancel := context.WithTimeout(context.Background(), left)
				herr := probeHealth(hctx, addr)
				cancel()
				if herr != nil {
					cmd.Process.Kill()
					<-done
					logf.Close()
					l.rollback(build, "was not healthy: "+herr.Error(), logPath)
					promoted = false
					watched = ""
					goto next
				}
			}
			if !exited {
				watched, watchUntil = build, started.Add(l.Watch)
				os.Remove(l.path(ProbationOK))
				// The probation ends while the build runs: say so, so the
				// daemon reports the deploy's success (#148).
				go func(build string) {
					select {
					case <-time.After(l.Probation):
						os.WriteFile(l.path(ProbationOK), []byte(build), 0o600)
					case <-stopped:
					}
				}(build)
			}
		}
		if !exited {
			err = <-done
		}
		close(stopped)
		logf.Close()
		{
			code := exitCode(err)
			l.mu.Lock()
			stopping := l.stopping
			l.mu.Unlock()
			if stopping {
				return nil
			}
			if promoted && time.Since(started) < l.Probation && code != ExitDeploy {
				l.rollback(build, fmt.Sprintf("exited with code %d after %s", code, time.Since(started).Round(time.Second)), logPath)
				promoted = false
				continue
			}
			promoted = false
			// #148: a switched build that crashes within its watch window
			// goes back to previous instead of crash-looping into an exit.
			if code != 0 && code != ExitDeploy && build == watched && time.Now().Before(watchUntil) {
				watched = ""
				l.rollback(build, fmt.Sprintf("crashed with code %d %s after its switch", code, time.Since(started).Round(time.Second)), logPath)
				continue
			}
			if code == ExitDeploy {
				watched = ""
			}
			switch {
			case code == ExitDeploy && l.exists("next") && l.exists("pin"):
				log.Printf("fyr: pinned to the seed; not switching to next")
			case code == ExitDeploy && l.exists("next"):
				if l.exists("current") {
					l.relink("previous", "current")
				} else {
					os.Remove(l.path("previous")) // the seed is the floor
				}
				l.relink("current", "next")
				os.Remove(l.path("next"))
				promoted = true
				log.Printf("fyr: switched to %s", mustReadlink(l.path("current")))
			case code == ExitDeploy:
				log.Printf("fyr: exit 75 without a next build; restarting current")
			case code == 0:
				return nil
			default:
				crashes = append(crashes, time.Now())
				if len(crashes) > 5 && time.Since(crashes[len(crashes)-6]) < time.Minute {
					return fmt.Errorf("fyrd crashed 6 times within a minute (code %d); see %s", code, logPath)
				}
				time.Sleep(time.Second)
			}
		}
	next:
	}
}

func (l *Launcher) rollback(build, reason, logPath string) {
	log.Printf("fyr: %s %s; rolling back", build, reason)
	if l.exists("previous") {
		l.relink("current", "previous")
	} else {
		os.Remove(l.path("current")) // back to the seed
	}
	b, _ := json.Marshal(failure{Build: filepath.Base(build), Reason: reason, Log: tailFile(logPath, 2000), At: time.Now()})
	os.WriteFile(l.path("deploy-failed.json"), b, 0o600)
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ee.ExitCode()
	}
	return 1
}

func mustReadlink(p string) string { s, _ := os.Readlink(p); return s }

func tailFile(p string, n int) string {
	b, _ := os.ReadFile(p)
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(b)
}
