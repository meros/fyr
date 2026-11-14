package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	mu      sync.Mutex
	running []string
	reports []string
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
	go func() { time.Sleep(100 * time.Millisecond); h.mu.Lock(); h.running = []string{"t1"}; h.mu.Unlock() }()
	start := time.Now()
	_, err := d.Deploy(context.Background(), "t1", req, "/src/fyr")
	if !errors.Is(err, core.ErrReportLater) {
		t.Fatalf("err %v", err)
	}
	if time.Since(start) < 90*time.Millisecond {
		t.Fatal("did not wait for the other running thread")
	}
	if len(*steps) != 2 || !strings.HasPrefix((*steps)[0], "make -C /src/fyr build") || (*steps)[1] != "make -C /src/fyr test" {
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
	if len(*steps) < 2 || !strings.HasPrefix((*steps)[0], "nix develop "+dir+" --command make -C "+dir+" build") {
		t.Fatalf("steps %v", *steps)
	}
}
