package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

type kv struct {
	mu sync.Mutex
	m  map[string]string
}

func (k *kv) GetKV(key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.m[key], nil
}
func (k *kv) SetKV(key, v string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if v == "" {
		delete(k.m, key)
	} else {
		k.m[key] = v
	}
	return nil
}
func (k *kv) Snapshot(path string) error { return os.WriteFile(path, []byte("db"), 0o600) }

type host struct {
	mu        sync.Mutex
	running   []string
	reports   []string
	continued []string // V0.83: thread and command id of each "continue"
}

func (h *host) Continue(th, cmdID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.continued = append(h.continued, th+" "+cmdID)
	return true
}

func (h *host) Running() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.running...)
}
func (h *host) Report(th string, req core.EffectReq, allowed, ok bool, text string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reports = append(h.reports, th+" "+strings.Fields(map[bool]string{true: "ok", false: "fail"}[ok])[0]+" "+text)
}

func newDeployer(t *testing.T, failStep string) (*Deployer, *host, *[]string) {
	t.Setenv("FYR_LAUNCHER", "1")
	steps := &[]string{}
	h := &host{}
	d := &Deployer{Data: t.TempDir(), DB: &kv{m: map[string]string{}}, Host: h, Quit: func() {},
		Poll: 10 * time.Millisecond, Probation: time.Millisecond,
		Run: func(_ context.Context, dir string, args ...string) ([]byte, error) {
			*steps = append(*steps, strings.Join(args, " "))
			if strings.Contains(strings.Join(args, " "), failStep) && failStep != "" {
				return []byte("FAIL TestX"), errors.New("exit 2")
			}
			return []byte("ok"), nil
		}}
	return d, h, steps
}

var req = core.EffectReq{Action: "fyr.deploy", Args: map[string]any{"dir": "/src/fyr"}}

// SPEC 0.5 V0.13: red tests stop the deploy before any switch.
func TestDeployStopsOnRedTests(t *testing.T) {
	d, _, _ := newDeployer(t, "test")
	_, err := d.Deploy(context.Background(), "t1", req, "/src/fyr")
	if err == nil || !strings.Contains(err.Error(), "FAIL TestX") {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Lstat(filepath.Join(d.Data, "next")); err == nil {
		t.Fatal("next was linked after red tests")
	}
	if d.ExitCode() != 0 {
		t.Fatal("daemon would exit after red tests")
	}
}

// SPEC 0.5 V0.13: build, test, snapshot, drain, link next, exit 75.
func TestDeploySwitches(t *testing.T) {
	d, h, steps := newDeployer(t, "")
	h.running = []string{"t1", "other"}
	go func() { time.Sleep(60 * time.Millisecond); h.mu.Lock(); h.running = []string{"t1"}; h.mu.Unlock() }()
	// The requester's own turn must end too before the switch.
	go func() { time.Sleep(120 * time.Millisecond); h.mu.Lock(); h.running = nil; h.mu.Unlock() }()
	start := time.Now()
	_, err := d.Deploy(context.Background(), "t1", req, "/src/fyr")
	if !errors.Is(err, core.ErrReportLater) {
		t.Fatalf("err %v", err)
	}
	if time.Since(start) < 110*time.Millisecond {
		t.Fatal("did not wait for every running thread, the requester included")
	}
	var made []string // V0.82: git reads HEAD and the remote first
	for _, s := range *steps {
		if !strings.HasPrefix(s, "git ") {
			made = append(made, s)
		}
	}
	if len(made) != 2 || !strings.HasPrefix(made[0], "make -C /src/fyr build") || made[1] != "make -C /src/fyr test" {
		t.Fatalf("steps %v", *steps)
	}
	next, err := os.Readlink(filepath.Join(d.Data, "next"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d.Data, next, "pre.db")); err != nil {
		t.Fatal("no database snapshot before the switch")
	}
	if d.ExitCode() != ExitDeploy {
		t.Fatalf("exit code %d", d.ExitCode())
	}
	if p, _ := d.DB.GetKV("pending_deploy"); !strings.Contains(p, `"thread":"t1"`) {
		t.Fatalf("pending %q", p)
	}
}

// SPEC 0.5 V0.13: the new daemon reports success; after a rollback the old
// one reports the failure.
func TestReportPending(t *testing.T) {
	d, h, _ := newDeployer(t, "")
	d.DB.SetKV("pending_deploy", `{"thread":"t1","build":"b2","req":{"action":"fyr.deploy","args":{"dir":"/src/fyr"}}}`)
	d.ReportPending()
	time.Sleep(50 * time.Millisecond)
	if len(h.reports) != 1 || !strings.HasPrefix(h.reports[0], "t1 ok") {
		t.Fatalf("%v", h.reports)
	}
	d2, h2, _ := newDeployer(t, "")
	d2.DB.SetKV("pending_deploy", `{"thread":"t1","build":"b2","req":{"action":"fyr.deploy"}}`)
	os.WriteFile(filepath.Join(d2.Data, "deploy-failed.json"), []byte(`{"build":"b2","reason":"exited with code 1 after 0s","log":"panic: x"}`), 0o600)
	d2.ReportPending()
	time.Sleep(50 * time.Millisecond)
	if len(h2.reports) != 1 || !strings.HasPrefix(h2.reports[0], "t1 fail") || !strings.Contains(h2.reports[0], "rolled back") {
		t.Fatalf("%v", h2.reports)
	}
	if _, err := os.Stat(filepath.Join(d2.Data, "deploy-failed.json")); err == nil {
		t.Fatal("failure report not consumed")
	}
}

func TestDeployNeedsLauncher(t *testing.T) {
	d, _, _ := newDeployer(t, "")
	t.Setenv("FYR_LAUNCHER", "")
	if _, err := d.Deploy(context.Background(), "t1", req, "/src/fyr"); err == nil || !strings.Contains(err.Error(), "launcher") {
		t.Fatalf("err %v", err)
	}
}

// Decision 0007: a checkout with a flake builds inside its own dev shell.
func TestDeployUsesNixDevelopWithFlake(t *testing.T) {
	d, _, steps := newDeployer(t, "")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "flake.nix"), []byte("{}"), 0o644)
	d.Deploy(context.Background(), "t1", req, dir)
	made := slices.DeleteFunc(slices.Clone(*steps), func(s string) bool { return strings.HasPrefix(s, "git ") }) // V0.82
	if len(made) < 2 || !strings.HasPrefix(made[0], "nix develop "+dir+" --command make -C "+dir+" build") {
		t.Fatalf("steps %v", *steps)
	}
}

// gitRunner answers git like a repository where HEAD is head and old is an
// ancestor of new; everything else succeeds.
func gitRunner(head string, ancestors map[string]bool, steps *[]string) Runner {
	return func(_ context.Context, dir string, args ...string) ([]byte, error) {
		*steps = append(*steps, strings.Join(args, " "))
		if args[0] == "git" && strings.Contains(strings.Join(args, " "), " rev-parse ") {
			return []byte(head + "\n"), nil
		}
		if args[0] == "git" && strings.Contains(strings.Join(args, " "), " merge-base ") {
			n := len(args)
			if ancestors[args[n-2]+">"+args[n-1]] {
				return nil, nil
			}
			return nil, errors.New("exit 1")
		}
		return []byte("ok"), nil
	}
}

// SPEC 0.5 V0.24: a deploy never switches to a commit older than the live
// build. (Found live: an approved deploy of an older commit replaced a newer
// build that had been switched in by hand.)
func TestDeployRefusesOlderCommit(t *testing.T) {
	d, _, _ := newDeployer(t, "")
	steps := &[]string{}
	d.Live = "2feec023d240"
	d.Run = gitRunner("2f67e4d59d58aaaa", map[string]bool{"2f67e4d59d58aaaa>2feec023d240": true}, steps)
	_, err := d.Deploy(context.Background(), "t1", req, "/src/fyr")
	if err == nil || !strings.Contains(err.Error(), "older than the live build") {
		t.Fatalf("err %v", err)
	}
	for _, s := range *steps {
		if strings.Contains(s, "make") {
			t.Fatalf("built an older commit: %v", *steps)
		}
	}
	// The same commit again, or a newer one, is allowed.
	d.Run = gitRunner("2feec023d240bbbb", nil, steps)
	if _, err := d.Deploy(context.Background(), "t1", req, "/src/fyr"); !errors.Is(err, core.ErrReportLater) {
		t.Fatalf("same commit refused: %v", err)
	}
}

// SPEC 0.5 V0.24: a newer deploy cancels one that has not switched yet.
func TestNewerDeployCancelsPending(t *testing.T) {
	d, h, _ := newDeployer(t, "")
	h.running = []string{"busy"}
	first := make(chan error, 1)
	go func() {
		_, err := d.Deploy(context.Background(), "t1", req, "/src/fyr")
		first <- err
	}()
	time.Sleep(50 * time.Millisecond) // the first one now drains
	second := make(chan error, 1)
	go func() {
		_, err := d.Deploy(context.Background(), "t2", req, "/src/fyr")
		second <- err
	}()
	select {
	case err := <-first:
		if err == nil || !strings.Contains(err.Error(), "newer deploy") {
			t.Fatalf("first: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("the first deploy was not cancelled")
	}
	h.mu.Lock()
	h.running = nil
	h.mu.Unlock()
	if err := <-second; !errors.Is(err, core.ErrReportLater) {
		t.Fatalf("second: %v", err)
	}
	if p, _ := d.DB.GetKV("pending_deploy"); !strings.Contains(p, `"thread":"t2"`) {
		t.Fatalf("pending %q", p)
	}
}

// SPEC 0.5 V0.39: a deploy reports each phase as it happens, with the threads
// it waits for; a failure reports why.
func TestDeployReportsProgress(t *testing.T) {
	d, h, _ := newDeployer(t, "")
	var mu sync.Mutex
	var phases []string
	var waited []string
	d.OnProgress = func(p Progress) {
		mu.Lock()
		defer mu.Unlock()
		if len(phases) == 0 || phases[len(phases)-1] != p.Phase {
			phases = append(phases, p.Phase)
		}
		if p.Phase == "drain" {
			waited = append(waited, strings.Join(p.Blocking, ","))
		}
	}
	h.running = []string{"t1", "other"}
	go func() { time.Sleep(60 * time.Millisecond); h.mu.Lock(); h.running = []string{"t1"}; h.mu.Unlock() }()
	go func() { time.Sleep(120 * time.Millisecond); h.mu.Lock(); h.running = nil; h.mu.Unlock() }()
	if _, err := d.Deploy(context.Background(), "t1", req, "/src/fyr"); !errors.Is(err, core.ErrReportLater) {
		t.Fatalf("err %v", err)
	}
	mu.Lock()
	got := strings.Join(phases, " ")
	w := strings.Join(waited, " | ")
	mu.Unlock()
	if got != "build test drain switch" {
		t.Fatalf("phases %q", got)
	}
	if !strings.Contains(w, "t1,other") || !strings.Contains(w, "t1") {
		t.Fatalf("blocking threads not reported: %q", w)
	}

	f, _, _ := newDeployer(t, "test")
	var last Progress
	f.OnProgress = func(p Progress) { last = p }
	f.Deploy(context.Background(), "t1", req, "/src/fyr")
	if last.Phase != "failed" || !strings.Contains(last.Detail, "FAIL TestX") {
		t.Fatalf("failure not reported: %+v", last)
	}
}

// SPEC 0.5 V0.39: the new daemon says the update is done at once; after a
// rollback the old one says it failed and why.
func TestReportPendingProgress(t *testing.T) {
	d, _, _ := newDeployer(t, "")
	var got []Progress
	d.OnProgress = func(p Progress) { got = append(got, p) }
	d.DB.SetKV("pending_deploy", `{"thread":"t1","build":"b2","req":{"action":"fyr.deploy"}}`)
	d.ReportPending()
	if len(got) == 0 || got[0].Phase != "done" || got[0].Build != "b2" {
		t.Fatalf("done not reported: %+v", got)
	}
	f, _, _ := newDeployer(t, "")
	got = nil
	f.OnProgress = func(p Progress) { got = append(got, p) }
	f.DB.SetKV("pending_deploy", `{"thread":"t1","build":"b3","req":{"action":"fyr.deploy"}}`)
	os.WriteFile(filepath.Join(f.Data, "deploy-failed.json"), []byte(`{"build":"b3","reason":"exited with code 1 after 0s","log":"panic: x"}`), 0o600)
	f.ReportPending()
	if len(got) == 0 || got[0].Phase != "failed" || !strings.Contains(got[0].Detail, "rolled back") {
		t.Fatalf("rollback not reported: %+v", got)
	}
}

// clientOnlySetup is a checkout whose fyrd sources equal the running
// daemon's (its server.sum), with a built client in web/dist, and a current
// build to replace.
func clientOnlySetup(t *testing.T, d *Deployer) (dir string, switched *[]string) {
	t.Helper()
	dir = t.TempDir()
	for p, s := range map[string]string{
		"go.mod":                 "module x\n",
		"internal/a/a.go":        "package a\n",
		"internal/a/a_test.go":   "package a\n",
		"web/embed.go":           "package web\n",
		"web/src/app.tsx":        "v1\n",
		"web/dist/index.html":    "<html>v1</html>\n",
		"web/dist/assets/app.js": "v1\n",
		"docs/SPEC.md":           "spec\n",
	} {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644)
	}
	sum, err := SourceSum(dir)
	if err != nil {
		t.Fatal(err)
	}
	d.LiveDir = filepath.Join(d.Data, "builds", "old")
	os.MkdirAll(d.LiveDir, 0o755)
	os.WriteFile(filepath.Join(d.LiveDir, "server.sum"), []byte(sum+"\n"), 0o644)
	os.Symlink(filepath.Join("builds", "old"), filepath.Join(d.Data, "current"))
	switched = &[]string{}
	d.SwitchClient = func(build, web string) error {
		*switched = append(*switched, build+" "+web)
		return nil
	}
	steps := d.Run
	d.Run = func(ctx context.Context, wd string, args ...string) ([]byte, error) {
		if len(args) == 2 && strings.HasSuffix(args[0], "/fyrd") && args[1] == "version" {
			return []byte("abc123def456\n"), nil
		}
		return steps(ctx, wd, args...)
	}
	return dir, switched
}

// SPEC 0.5 V0.81: with an equal fyrd, a deploy builds and tests, then
// switches the client only: no drain, no restart, open pages reload.
func TestDeployClientOnly(t *testing.T) {
	d, h, steps := newDeployer(t, "")
	dir, switched := clientOnlySetup(t, d)
	var phases []string
	d.OnProgress = func(p Progress) {
		if len(phases) == 0 || phases[len(phases)-1] != p.Phase {
			phases = append(phases, p.Phase)
		}
	}
	// Threads keep running: the client-only path does not wait for them.
	h.running = []string{"t1", "busy"}
	os.WriteFile(filepath.Join(dir, "web", "src", "app.tsx"), []byte("v2\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "internal", "a", "a_test.go"), []byte("package a // more tests\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "docs", "SPEC.md"), []byte("spec 2\n"), 0o644)
	done := make(chan struct{})
	var out string
	var err error
	go func() { out, err = d.Deploy(context.Background(), "t1", req, dir); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the client-only path waited for running threads")
	}
	if err != nil || !strings.Contains(out, "nothing restarted") {
		t.Fatalf("out %q err %v", out, err)
	}
	all := strings.Join(*steps, "\n")
	if !strings.Contains(all, " build") || !strings.Contains(all, " test") {
		t.Fatalf("build and test must still run: %v", *steps)
	}
	if d.ExitCode() != 0 {
		t.Fatal("the daemon would restart")
	}
	if _, err := os.Lstat(filepath.Join(d.Data, "next")); err == nil {
		t.Fatal("next was linked")
	}
	if p, _ := d.DB.GetKV("pending_deploy"); p != "" {
		t.Fatalf("pending %q", p)
	}
	cur, _ := os.Readlink(filepath.Join(d.Data, "current"))
	prev, _ := os.Readlink(filepath.Join(d.Data, "previous"))
	if prev != filepath.Join("builds", "old") || !strings.HasPrefix(cur, "builds"+string(filepath.Separator)) || cur == prev {
		t.Fatalf("current %q previous %q", cur, prev)
	}
	web := filepath.Join(d.Data, cur, "web")
	if len(*switched) != 1 || (*switched)[0] != "abc123def456 "+web {
		t.Fatalf("switched %v, want build abc123def456 at %s", *switched, web)
	}
	if b, _ := os.ReadFile(filepath.Join(web, "assets", "app.js")); string(b) != "v1\n" {
		t.Fatalf("client not copied: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(d.Data, cur, "server.sum")); len(strings.TrimSpace(string(b))) != 64 {
		t.Fatalf("server.sum %q", b)
	}
	if got := strings.Join(phases, " "); got != "build test client done" {
		t.Fatalf("phases %q", got)
	}
	if d.Live != "abc123def456" {
		t.Fatalf("live %q", d.Live)
	}
}

// SPEC 0.5 V0.81: a change to fyrd's sources takes the full path (V0.13).
func TestDeployFullPathWhenFyrdChanged(t *testing.T) {
	for name, file := range map[string]string{"go": "internal/a/a.go", "go.mod": "go.mod", "embed": "web/embed.go", "new": "internal/b/b.go"} {
		t.Run(name, func(t *testing.T) {
			d, _, _ := newDeployer(t, "")
			dir, switched := clientOnlySetup(t, d)
			os.MkdirAll(filepath.Join(dir, filepath.Dir(file)), 0o755)
			os.WriteFile(filepath.Join(dir, file), []byte("package a // changed\n"), 0o644)
			if _, err := d.Deploy(context.Background(), "t1", req, dir); !errors.Is(err, core.ErrReportLater) {
				t.Fatalf("err %v", err)
			}
			if d.ExitCode() != ExitDeploy || len(*switched) != 0 {
				t.Fatalf("exit %d switched %v", d.ExitCode(), *switched)
			}
			if _, err := os.Readlink(filepath.Join(d.Data, "next")); err != nil {
				t.Fatal("next not linked")
			}
		})
	}
	// A daemon without server.sum (the seed, an older build) never takes
	// the client-only path.
	d, _, _ := newDeployer(t, "")
	dir, switched := clientOnlySetup(t, d)
	os.Remove(filepath.Join(d.LiveDir, "server.sum"))
	if _, err := d.Deploy(context.Background(), "t1", req, dir); !errors.Is(err, core.ErrReportLater) || len(*switched) != 0 {
		t.Fatalf("err %v switched %v", err, *switched)
	}
}

// SPEC 0.5 V0.81: a failed client switch keeps the old client and links.
func TestDeployClientOnlyFailureKeepsOld(t *testing.T) {
	d, _, _ := newDeployer(t, "")
	dir, _ := clientOnlySetup(t, d)
	d.SwitchClient = func(string, string) error { return errors.New("no") }
	var last Progress
	d.OnProgress = func(p Progress) { last = p }
	if _, err := d.Deploy(context.Background(), "t1", req, dir); err == nil || errors.Is(err, core.ErrReportLater) {
		t.Fatalf("err %v", err)
	}
	if cur, _ := os.Readlink(filepath.Join(d.Data, "current")); cur != filepath.Join("builds", "old") {
		t.Fatalf("current %q", cur)
	}
	if _, err := os.Lstat(filepath.Join(d.Data, "previous")); err == nil {
		t.Fatal("previous left pointing at the old build after a failed switch")
	}
	if last.Phase != "failed" || d.ExitCode() != 0 {
		t.Fatalf("last %+v exit %d", last, d.ExitCode())
	}
}
