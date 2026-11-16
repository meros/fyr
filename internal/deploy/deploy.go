package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/proc"
)

// Runner runs a command in dir and returns its combined output.
type Runner func(ctx context.Context, dir string, args ...string) ([]byte, error)

func execRunner(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := proc.Command(ctx, args[0], args[1:]...)
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
	// OnProgress hears each phase as it happens (SPEC 0.5 V0.39).
	OnProgress func(Progress)
	// Live is the build id of this daemon (a commit, maybe with -dirty-…).
	// A deploy of an older commit is refused (SPEC 0.5 V0.24).
	Live string
	// LiveDir holds the running fyrd and its server.sum (V0.81); empty is
	// the folder of this executable.
	LiveDir string
	// SwitchClient makes the front serve the client in dir and report build
	// as its buildId, so open pages reload (V0.81); nil serves no client.
	SwitchClient func(build, dir string) error

	exit   int
	mu     sync.Mutex
	gen    int
	cancel context.CancelFunc
}

// Progress is where a deploy is (SPEC 0.5 V0.39): build, test, drain (with
// the threads it waits for), switch, then done or failed.
type Progress struct {
	Phase     string    `json:"phase"`
	Detail    string    `json:"detail,omitempty"`
	Blocking  []string  `json:"blocking,omitempty"`
	Build     string    `json:"build,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	At        time.Time `json:"at"`
}

func (d *Deployer) report(p *Progress, phase, detail string, blocking []string) {
	p.Phase, p.Detail, p.Blocking, p.At = phase, detail, blocking, time.Now()
	if d.OnProgress != nil {
		d.OnProgress(*p)
	}
}

var errReplaced = errors.New("a newer deploy replaced this one; nothing was switched")

// olderThanLive reports why dir's HEAD must not replace the live build, or "".
func (d *Deployer) olderThanLive(ctx context.Context, run Runner, dir string) string {
	d.mu.Lock()
	live, _, _ := strings.Cut(d.Live, "-")
	d.mu.Unlock()
	if live == "" {
		return ""
	}
	b, err := run(ctx, dir, "git", "-C", dir, "rev-parse", "HEAD")
	head := strings.TrimSpace(string(b))
	if err != nil || head == "" || strings.HasPrefix(head, live) {
		return ""
	}
	if _, err := run(ctx, dir, "git", "-C", dir, "merge-base", "--is-ancestor", head, live); err == nil {
		short := head
		if len(short) > 12 {
			short = short[:12]
		}
		return fmt.Sprintf("commit %s is older than the live build %s; nothing was switched", short, live)
	}
	return ""
}

type pending struct {
	Thread string         `json:"thread"`
	Build  string         `json:"build"`
	Req    core.EffectReq `json:"req"`
}

func (d *Deployer) ExitCode() int { return d.exit }

func tail(b []byte, n int) string {
	return core.TruncTail(strings.TrimSpace(string(b)), n)
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
	// One deploy at a time: a newer one cancels one that has not switched.
	d.mu.Lock()
	if d.cancel != nil {
		d.cancel()
	}
	d.gen++
	gen := d.gen
	ctx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	d.mu.Unlock()
	defer cancel()
	replaced := func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.gen != gen
	}
	fail := func(err error) (string, error) {
		if replaced() {
			return "", errReplaced
		}
		return "", err
	}
	id := time.Now().Format("20060102-150405")
	prog := &Progress{Build: id, StartedAt: time.Now()}
	failed := func(err error) (string, error) {
		if err != nil && !errors.Is(err, core.ErrReportLater) {
			d.report(prog, "failed", err.Error(), nil)
		}
		return "", err
	}
	if why := d.olderThanLive(ctx, run, dir); why != "" {
		return failed(errors.New(why))
	}
	// V0.81: fyrd's sources as they are built; equal to the running
	// daemon's, only the web client changes.
	sum, _ := SourceSum(dir)
	clientOnly := sum != "" && sum == d.liveSum()
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
	d.report(prog, "build", "", nil)
	if b, err := run(ctx, dir, build...); err != nil {
		os.RemoveAll(out)
		return failed(firstErr(fail(fmt.Errorf("build failed: %v\n%s", err, tail(b, 3000)))))
	}
	d.report(prog, "test", "", nil)
	if b, err := run(ctx, dir, test...); err != nil {
		os.RemoveAll(out)
		return failed(firstErr(fail(fmt.Errorf("tests failed, nothing was switched: %v\n%s", err, tail(b, 3000)))))
	}
	if sum != "" {
		os.WriteFile(filepath.Join(out, sumFile), []byte(sum+"\n"), 0o644)
	}
	if clientOnly {
		return d.deployClient(ctx, run, prog, gen, dir, out, id)
	}
	if err := d.DB.Snapshot(filepath.Join(out, "pre.db")); err != nil {
		return failed(fmt.Errorf("database snapshot: %w", err))
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
		running := d.Host.Running()
		others := len(running)
		if others == 0 {
			break
		}
		if strings.Join(running, ",") != strings.Join(prog.Blocking, ",") || prog.Phase != "drain" {
			d.report(prog, "drain", "", append([]string(nil), running...))
		}
		if time.Now().After(deadline) {
			return failed(fmt.Errorf("%d threads (the requester included) were still running after %s; nothing was switched", others, max))
		}
		select {
		case <-ctx.Done():
			return failed(firstErr(fail(ctx.Err())))
		case <-time.After(poll):
		}
	}
	d.mu.Lock()
	if d.gen != gen {
		d.mu.Unlock()
		return "", errReplaced
	}
	d.cancel = nil // past this point the switch happens; nothing cancels it
	b, _ := json.Marshal(pending{Thread: threadID, Build: id, Req: req})
	if err := d.DB.SetKV("pending_deploy", string(b)); err != nil {
		d.mu.Unlock()
		return "", err
	}
	next := filepath.Join(d.Data, "next")
	os.Remove(next)
	if err := os.Symlink(filepath.Join("builds", id), next); err != nil {
		d.mu.Unlock()
		return "", err
	}
	d.exit = ExitDeploy
	d.mu.Unlock()
	d.report(prog, "switch", "", nil)
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
		d.report(&Progress{Build: f.Build, StartedAt: time.Now()}, "failed", msg, nil)
		go d.Host.Report(p.Thread, p.Req, true, false, msg)
		return
	}
	d.report(&Progress{Build: p.Build, StartedAt: time.Now()}, "done", "", nil)
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

func firstErr(_ string, err error) error { return err }
