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
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/launchenv"
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
	// Continue sends "continue" to a thread whose turn a forced switch cut
	// (V0.83), as the owner's message with commandID; false: it did not.
	Continue(threadID, commandID string) bool
}

// ErrNotDraining: Restart now came while no deploy waits for threads.
var ErrNotDraining = errors.New("No update is waiting for running threads")

// Deployer runs the fyr.deploy effect inside the daemon.
type Deployer struct {
	Data string
	DB   DB
	Host Host
	Quit func() // stops the daemon gracefully
	Run  Runner
	// Survive: the agent processes outlive the switch (the launcher's
	// runner holds them, V0.90), so a deploy does not drain.
	Survive   bool
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
	// Cgroup: where the build and the tests run (V0.80); "" is the daemon's.
	Cgroup string
	// Slot waits, with priority, for a slot for `make test` (V0.86) and
	// returns its release; nil runs the tests without one.
	Slot func(ctx context.Context) (release func(), err error)
	// OnShipped hears a deploy that succeeded (V0.82): after the new
	// daemon's probation, or when the client-only path switched. nil
	// hears nothing.
	OnShipped func(Shipped)
	// OnIdle hears that no deploy runs any more (V0.111): one failed, or
	// the new build's probation ended. nil hears nothing.
	OnIdle func()
	// Releases is where auto-update gets CI's signed builds (decision 0027);
	// nil: only local builds.
	Releases *Releases
	// Keep is how many builds besides current, previous and next survive a
	// prune (#147); 0 is 3.
	Keep int

	exit   int
	active int // deploys between start and return (V0.111)
	mu     sync.Mutex
	gen    int
	cancel context.CancelFunc
	force  chan struct{} // set while a deploy drains (V0.83)
}

// DeployNow is the owner's Restart now (V0.83): the deploy that drains
// stops waiting and switches at once. ErrNotDraining when none drains.
func (d *Deployer) DeployNow() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.force == nil {
		return ErrNotDraining
	}
	select {
	case d.force <- struct{}{}:
	default: // pressed twice: one is enough
	}
	return nil
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
	// Client marks the client-only path (V0.81), so the page shows nothing beyond its reload (V0.118 (3)).
	Client bool `json:"client,omitempty"`
	// start makes this update's record (#151) when its first phase comes.
	start func() Update
}

func (d *Deployer) report(p *Progress, phase, detail string, blocking []string) {
	p.Phase, p.Detail, p.Blocking, p.At = phase, detail, blocking, time.Now()
	if p.start != nil {
		d.recordPhase(*p, p.start)
	}
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
	// V0.82: what was built, so the new daemon can say which changes it
	// holds.
	Commit string `json:"commit,omitempty"`
	Remote string `json:"remote,omitempty"`
	Dir    string `json:"dir,omitempty"`
	// Cut are the threads whose turns a forced switch ended (V0.83).
	Cut []string `json:"cut,omitempty"`
	// Local: a build of a checkout, which pauses auto-update once it runs
	// (#159); Branch is the checkout's branch.
	Local  bool   `json:"local,omitempty"`
	Branch string `json:"branch,omitempty"`
}

// Shipped is a deploy that succeeded (V0.82): the build id, the commit it
// was built from (HEAD of the checkout), the checkout's origin remote as
// git printed it, and Contains, which reports a commit that is an ancestor
// of Commit in the checkout.
type Shipped struct {
	Build, Commit, Remote, Dir string
	Contains                   func(sha string) bool
}

// source reads the checkout's HEAD and origin remote before the build
// (V0.82); either is "" when git cannot say.
func source(ctx context.Context, run Runner, dir string) (commit, remote string) {
	if b, err := run(ctx, dir, "git", "-C", dir, "rev-parse", "HEAD"); err == nil {
		commit = strings.TrimSpace(string(b))
	}
	if b, err := run(ctx, dir, "git", "-C", dir, "remote", "get-url", "origin"); err == nil {
		remote = strings.TrimSpace(string(b))
	}
	if strings.ContainsAny(commit, " \n\t") {
		commit = ""
	}
	return commit, remote
}

// ship tells OnShipped about a build of commit, from the checkout dir or,
// for a release (dir ""), from GitHub's compare.
func (d *Deployer) ship(run Runner, build, commit, remote, dir string) {
	if commit != "" {
		d.DB.SetKV("live_commit", commit)
	}
	if d.OnShipped == nil || commit == "" || (dir == "" && d.Releases == nil) {
		return
	}
	d.OnShipped(Shipped{Build: build, Commit: commit, Remote: remote, Dir: dir, Contains: func(sha string) bool {
		if sha == "" || strings.HasPrefix(sha, "-") { // never an option to git
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if dir == "" {
			return d.Releases.Contains(ctx, commit, sha)
		}
		_, err := run(ctx, dir, "git", "-C", dir, "merge-base", "--is-ancestor", sha, commit)
		return err == nil
	}})
}

func hasNix() bool { _, err := exec.LookPath("nix"); return err == nil }

func (d *Deployer) runner() Runner {
	if d.Run == nil {
		return execRunner
	}
	return d.Run
}

func (d *Deployer) ExitCode() int { return d.exit }

func tail(b []byte, n int) string {
	return core.TruncTail(strings.TrimSpace(string(b)), n)
}

// Deploy builds and tests dir, snapshots the database, waits until no other
// thread runs, links next and stops the daemon with ExitDeploy. It returns
// core.ErrReportLater on success: the new daemon reports the result.
func (d *Deployer) Deploy(ctx context.Context, threadID string, req core.EffectReq, dir string) (string, error) {
	if !launchenv.Current().Launcher {
		return "", errors.New("Fyr is not running under the fyr launcher, so it cannot switch builds. Start it with `fyr` (see README)")
	}
	run := d.runner()
	// One deploy at a time: a newer one cancels one that has not switched.
	d.mu.Lock()
	if d.cancel != nil {
		d.cancel()
	}
	d.active++
	defer d.ended()
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
	// #151: the record of this attempt: why it ran and what it builds.
	trigger, _ := req.Args["trigger"].(string)
	if trigger == "" {
		trigger = "agent"
	}
	recCommit, _ := req.Args["commit"].(string)
	prog.start = func() Update {
		return Update{Build: prog.Build, Commit: recCommit, Trigger: trigger, Local: dir != "", Thread: threadID, Result: UpdateRunning, StartedAt: prog.StartedAt}
	}
	failed := func(err error) (string, error) {
		if err != nil && !errors.Is(err, core.ErrReportLater) {
			d.report(prog, "failed", err.Error(), nil)
		}
		return "", err
	}
	// Decision 0027: an ask without a checkout installs a release.
	if dir == "" {
		sha, _ := req.Args["commit"].(string)
		return d.install(ctx, run, prog, gen, threadID, req, sha, fail, failed)
	}
	if why := d.olderThanLive(ctx, run, dir); why != "" {
		return failed(errors.New(why))
	}
	commit, remote := source(ctx, run, dir) // V0.82: what is built
	recCommit = commit
	branch := gitBranch(ctx, run, dir)
	if mk := prog.start; mk != nil {
		prog.start = func() Update { u := mk(); u.Branch = branch; return u }
	}
	// V0.111: an auto-approved deploy builds only the commit the gate passed.
	if want, _ := req.Args["commit"].(string); want != "" && commit != want {
		return failed(fmt.Errorf("The checkout moved from %s to %s after the approval; nothing was built. Ask again.", short7(want), short7(commit)))
	}
	// V0.81: fyrd's sources as they are built; equal to the running
	// daemon's, only the web client changes.
	sum, _ := SourceSum(dir)
	clientOnly := sum != "" && sum == d.liveSum()
	out := filepath.Join(d.Data, "builds", id)
	if err := diskRoom(d.Data); err != nil {
		return failed(err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return "", err
	}
	// #145: the whole build and test output stays with the build; a failed
	// build keeps its logs and loses its binaries.
	dropBins := func() {
		os.Remove(filepath.Join(out, "fyrd"))
		os.Remove(filepath.Join(out, "fyr"))
	}
	// Decision 0007: build with the checkout's own pinned toolchain when it
	// has a flake, so the system profile is never touched. A host without
	// Nix (a Mac) builds with the toolchain on its PATH.
	build := []string{"make", "-C", dir, "build", "OUT=" + out}
	test := []string{"make", "-C", dir, "test"}
	if _, err := os.Stat(filepath.Join(dir, "flake.nix")); err == nil && hasNix() {
		build = append([]string{"nix", "develop", dir, "--command"}, build...)
		test = append([]string{"nix", "develop", dir, "--command"}, test...)
	}
	// V0.80: the build and the tests run in agents/deploy/, under the agents' budget.
	inCg, closeCg := proc.WithCgroup(ctx, d.Cgroup)
	defer closeCg()
	d.report(prog, "build", "", nil)
	b, err := run(inCg, dir, build...)
	buildLog := filepath.Join(out, "build.log")
	os.WriteFile(buildLog, b, 0o644)
	if err != nil {
		dropBins()
		return failed(firstErr(fail(fmt.Errorf("build failed (%s), nothing was switched; log: %s\n%s", Failing(b), buildLog, tail(b, 3000)))))
	}
	// #159: a local build runs the smoke test; the full suite only on ask.
	if full, _ := req.Args["tests"].(bool); full {
		// V0.86: the tests hold a slot for heavy jobs, ahead of every agent.
		release := func() {}
		if d.Slot != nil {
			d.report(prog, "test", "waiting for room to run the tests", nil)
			r, err := d.Slot(ctx)
			if err != nil {
				dropBins()
				return failed(firstErr(fail(fmt.Errorf("the tests were not admitted, nothing was switched: %v", err))))
			}
			release = r
		}
		d.report(prog, "test", "", nil)
		tb, terr := run(inCg, dir, test...)
		release()
		testLog := filepath.Join(out, "test.log")
		os.WriteFile(testLog, tb, 0o644)
		if terr != nil {
			dropBins()
			return failed(firstErr(fail(fmt.Errorf("tests failed (%s), nothing was switched; log: %s\n%s", Failing(tb), testLog, tail(tb, 3000)))))
		}
	} else {
		d.report(prog, "smoke", "", nil)
		if err := d.smoke(inCg, run, out, ""); err != nil {
			dropBins()
			return failed(firstErr(fail(fmt.Errorf("the smoke test failed, nothing was switched: %w", err))))
		}
	}
	if sum != "" {
		os.WriteFile(filepath.Join(out, sumFile), []byte(sum+"\n"), 0o644)
	}
	if clientOnly {
		res, err := d.deployClient(ctx, run, prog, gen, dir, out, id)
		if err == nil {
			d.ship(run, id, commit, remote, dir)
			d.pause(Paused{Build: id, Commit: commit, Branch: branch, Dir: dir, At: time.Now()})
			d.Prune()
		}
		return res, err
	}
	req.Args = withLocal(req.Args, branch)
	return d.switchTo(ctx, prog, gen, threadID, req, id, commit, remote, dir, out)
}

// withLocal marks a deploy ask's args as a local build of branch (#159).
func withLocal(args map[string]any, branch string) map[string]any {
	out := map[string]any{}
	for k, v := range args {
		out[k] = v
	}
	out["local"] = true
	if branch != "" {
		out["branch"] = branch
	}
	return out
}

func gitBranch(ctx context.Context, run Runner, dir string) string {
	b, err := run(ctx, dir, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	s := strings.TrimSpace(string(b))
	if err != nil || strings.ContainsAny(s, " \n") {
		return ""
	}
	return s
}

// switchTo is the end of every deploy, local or release: snapshot the
// database, drain when the agents do not survive, record pending_deploy,
// link next and stop with ExitDeploy for the launcher to switch.
func (d *Deployer) switchTo(ctx context.Context, prog *Progress, gen int, threadID string, req core.EffectReq, id, commit, remote, dir, out string) (string, error) {
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
	failed := func(err error) (string, error) {
		if err != nil && !errors.Is(err, core.ErrReportLater) {
			d.report(prog, "failed", err.Error(), nil)
		}
		return "", err
	}
	local, _ := req.Args["local"].(bool)
	branch, _ := req.Args["branch"].(string)
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
	// V0.83: while this deploy drains, the owner's Restart now ends the wait.
	force := make(chan struct{}, 1)
	d.mu.Lock()
	d.force = force
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		if d.force == force {
			d.force = nil
		}
		d.mu.Unlock()
	}()
	var cut []string
drain:
	for {
		// Every run must end, the requester's too: the switch stops the
		// process, and a live turn would die with it (SPEC 0.5 V0.13).
		running := d.Host.Running()
		others := len(running)
		if others == 0 || d.Survive {
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
		case <-force:
			// The turns running now end with the process; each gets
			// "continue" from the new daemon.
			cut = d.Host.Running()
			break drain
		case <-time.After(poll):
		}
	}
	d.mu.Lock()
	if d.gen != gen {
		d.mu.Unlock()
		return "", errReplaced
	}
	d.cancel = nil // past this point the switch happens; nothing cancels it
	d.force = nil
	b, _ := json.Marshal(pending{Thread: threadID, Build: id, Req: req, Commit: commit, Remote: remote, Dir: dir, Cut: cut, Local: local, Branch: branch})
	if err := d.DB.SetKV("pending_deploy", string(b)); err != nil {
		d.mu.Unlock()
		return "", err
	}
	// #152: next is replaced atomically, never missing for a moment.
	if err := setLink(filepath.Join(d.Data, "next"), filepath.Join("builds", id)); err != nil {
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
	// V0.83: the turns a forced switch cut resume, on this build or after a
	// rollback alike. The command id is the build's, so a second start of
	// this daemon sends nothing twice.
	for _, th := range p.Cut {
		d.Host.Continue(th, "deploy-"+p.Build+"-continue-"+th)
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
		if !p.Local && p.Commit != "" {
			d.markBad(p.Commit, f.Reason) // #154: not tried again until a newer release
		}
		d.finish(f.Build, UpdateRolledBack, f.Reason)
		go func() {
			d.tell(p.Thread, p.Req, false, msg)
			d.idle()
		}()
		return
	}
	d.report(&Progress{Build: p.Build, StartedAt: time.Now()}, "done", "", nil)
	wait := d.Probation
	if wait == 0 {
		wait = 65 * time.Second
	}
	go func() {
		// #148: the launcher says when the probation ended (probation-ok);
		// a launcher from before #148 never does, so after wait plus a
		// margin the deploy counts as good as before.
		okPath := filepath.Join(d.Data, ProbationOK)
		step := min(time.Second, wait/10+time.Millisecond)
		for end := time.Now().Add(wait + wait/2); time.Now().Before(end); time.Sleep(step) {
			if b, err := os.ReadFile(okPath); err == nil && filepath.Base(strings.TrimSpace(string(b))) == p.Build {
				break
			}
		}
		d.DB.SetKV("pending_deploy", "")
		d.ship(d.runner(), p.Build, p.Commit, p.Remote, p.Dir)
		msg := "Fyr now runs release " + p.Build + "."
		if p.Local {
			// #159: a local build holds auto-update off until a release
			// contains it or the owner goes back to the release track.
			d.pause(Paused{Build: p.Build, Commit: p.Commit, Branch: p.Branch, Dir: p.Dir, At: time.Now()})
			msg = "Fyr now runs local build " + p.Build
			if p.Branch != "" {
				msg += " of " + p.Branch
			}
			msg += "; auto-update is paused until a release contains it, or until the owner presses Back to release."
		} else {
			d.Unpause()
		}
		d.finish(p.Build, UpdateLive, "")
		d.Prune()
		d.tell(p.Thread, p.Req, true, msg)
		d.idle()
	}()
}

// tell reports a deploy's result to the thread that asked; an update Fyr
// started by itself has no thread and only logs.
func (d *Deployer) tell(thread string, req core.EffectReq, ok bool, msg string) {
	if thread == "" {
		log.Printf("update: %s", msg)
		return
	}
	d.Host.Report(thread, req, true, ok, msg)
}

// markBad keeps a release that rolled back here from being tried again
// until deployable moves past it (#154).
func (d *Deployer) markBad(commit, why string) {
	b, _ := json.Marshal(map[string]string{"commit": commit, "why": why, "at": time.Now().UTC().Format(time.RFC3339)})
	d.DB.SetKV("update_bad", string(b))
}

// Bad is the release commit that last rolled back here, and why.
func (d *Deployer) Bad() (commit, why string) {
	raw, _ := d.DB.GetKV("update_bad")
	var m map[string]string
	json.Unmarshal([]byte(raw), &m)
	return m["commit"], m["why"]
}

func firstErr(_ string, err error) error { return err }

// Busy: a deploy builds, tests or drains here, or a switched build waits
// out its probation (pending_deploy), so no other deploy starts by itself
// (V0.111).
func (d *Deployer) Busy() bool {
	d.mu.Lock()
	active := d.active > 0
	d.mu.Unlock()
	if active {
		return true
	}
	p, _ := d.DB.GetKV("pending_deploy")
	return p != ""
}

// ended counts a deploy out; the last one to fail tells OnIdle. A deploy
// that switched leaves pending_deploy and the daemon stops, so Busy holds.
func (d *Deployer) ended() {
	d.mu.Lock()
	d.active--
	if d.active == 0 && d.cancel != nil {
		d.cancel = nil
	}
	last := d.active == 0
	d.mu.Unlock()
	if last {
		d.idle()
	}
}

func (d *Deployer) idle() {
	if d.OnIdle != nil && !d.Busy() {
		d.OnIdle()
	}
}

func short7(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
