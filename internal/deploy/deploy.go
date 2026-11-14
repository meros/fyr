package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/meros/fyr/internal/core"
)

// Runner runs a command in dir and returns its combined output.
type Runner func(ctx context.Context, dir string, args ...string) ([]byte, error)

func execRunner(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	return cmd.CombinedOutput()
}

// DB is what the Deployer needs from the store.
type DB interface {
	GetKV(k string) (string, error)
	SetKV(k, v string) error
	Snapshot(path string) error
}

// Host is what the Deployer needs from the engine.
type Host interface {
	Running() []string
	Report(threadID string, req core.EffectReq, allowed, ok bool, text string)
}

// Deployer runs the fyr.deploy effect inside the daemon.
type Deployer struct {
	Data      string
	DB        DB
	Host      Host
	Quit      func() // stops the daemon gracefully
	Run       Runner
	Poll      time.Duration // drain poll interval
	Drain     time.Duration // max drain wait
	Probation time.Duration // wait before reporting success

	exit int
}

type pending struct {
	Thread string         `json:"thread"`
	Build  string         `json:"build"`
	Req    core.EffectReq `json:"req"`
}

func (d *Deployer) ExitCode() int { return d.exit }

func tail(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		s = "…" + s[len(s)-n:]
	}
	return s
}

// Deploy builds and tests dir, snapshots the database, waits until no other
// thread runs, links next and stops the daemon with ExitDeploy. It returns
// core.ErrReportLater on success: the new daemon reports the result.
func (d *Deployer) Deploy(ctx context.Context, threadID string, req core.EffectReq, dir string) (string, error) {
	if os.Getenv("FYR_LAUNCHER") != "1" {
		return "", errors.New("Fyr is not running under the fyr launcher, so it cannot switch builds. Start it with `fyr` (see README)")
	}
	run := d.Run
	if run == nil {
		run = execRunner
	}
	id := time.Now().Format("20060102-150405")
	out := filepath.Join(d.Data, "builds", id)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return "", err
	}
	// Decision 0007: build with the checkout's own pinned toolchain when it
	// has a flake, so the system profile is never touched.
	build := []string{"make", "-C", dir, "build", "OUT=" + out}
	test := []string{"make", "-C", dir, "test"}
	if _, err := os.Stat(filepath.Join(dir, "flake.nix")); err == nil {
		build = append([]string{"nix", "develop", dir, "--command"}, build...)
		test = append([]string{"nix", "develop", dir, "--command"}, test...)
	}
	if b, err := run(ctx, dir, build...); err != nil {
		os.RemoveAll(out)
		return "", fmt.Errorf("build failed: %v\n%s", err, tail(b, 3000))
	}
	if b, err := run(ctx, dir, test...); err != nil {
		os.RemoveAll(out)
		return "", fmt.Errorf("tests failed, nothing was switched: %v\n%s", err, tail(b, 3000))
	}
	if err := d.DB.Snapshot(filepath.Join(out, "pre.db")); err != nil {
		return "", fmt.Errorf("database snapshot: %w", err)
	}
	poll, max := d.Poll, d.Drain
	if poll == 0 {
		poll = 2 * time.Second
	}
	if max == 0 {
		max = 30 * time.Minute
	}
	deadline := time.Now().Add(max)
	for {
		// Every run must end, the requester's too: the switch stops the
		// process, and a live turn would die with it (SPEC 0.5 V0.13).
		others := len(d.Host.Running())
		if others == 0 {
			break
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("%d other threads were still running after %s; nothing was switched", others, max)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(poll):
		}
	}
	b, _ := json.Marshal(pending{Thread: threadID, Build: id, Req: req})
	if err := d.DB.SetKV("pending_deploy", string(b)); err != nil {
		return "", err
	}
	next := filepath.Join(d.Data, "next")
	os.Remove(next)
	if err := os.Symlink(filepath.Join("builds", id), next); err != nil {
		return "", err
	}
	d.exit = ExitDeploy
	if d.Quit != nil {
		d.Quit()
	}
	return "", core.ErrReportLater
}

// ReportPending delivers the result of a deploy that a previous daemon
// started: a rollback report from the launcher, or success once this build
// has survived its probation.
func (d *Deployer) ReportPending() {
	raw, _ := d.DB.GetKV("pending_deploy")
	if raw == "" {
		return
	}
	var p pending
	if json.Unmarshal([]byte(raw), &p) != nil {
		d.DB.SetKV("pending_deploy", "")
		return
	}
	failedPath := filepath.Join(d.Data, "deploy-failed.json")
	if b, err := os.ReadFile(failedPath); err == nil {
		var f failure
		json.Unmarshal(b, &f)
		os.Remove(failedPath)
		d.DB.SetKV("pending_deploy", "")
		msg := fmt.Sprintf("Build %s %s, so Fyr rolled back to the previous build.", f.Build, f.Reason)
		if f.Log != "" {
			msg += "\nLog tail:\n" + tail([]byte(f.Log), 1500)
		}
		go d.Host.Report(p.Thread, p.Req, true, false, msg)
		return
	}
	wait := d.Probation
	if wait == 0 {
		wait = 65 * time.Second
	}
	go func() {
		time.Sleep(wait)
		d.DB.SetKV("pending_deploy", "")
		d.Host.Report(p.Thread, p.Req, true, true, "Fyr now runs build "+p.Build+".")
	}()
}
