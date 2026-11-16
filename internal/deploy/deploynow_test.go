package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// SPEC 0.5 V0.83: the owner's Restart now ends the drain at once: the
// deploy switches with the threads still running, and stores the threads
// whose turns it cut. Outside a drain there is nothing to force.
func TestDeployNowStopsTheDrain(t *testing.T) {
	d, h, _ := newDeployer(t, "")
	d.Drain = time.Hour
	if err := d.DeployNow(); !errors.Is(err, ErrNotDraining) {
		t.Fatalf("outside a drain: %v", err)
	}
	h.running = []string{"t1", "busy"}
	drained := make(chan struct{}, 1)
	d.OnProgress = func(p Progress) {
		if p.Phase == "drain" {
			select {
			case drained <- struct{}{}:
			default:
			}
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := d.Deploy(context.Background(), "t1", req, "/src/fyr")
		done <- err
	}()
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("the deploy never drained")
	}
	h.mu.Lock()
	h.running = []string{"t1", "busy", "late"} // the list at the press counts
	h.mu.Unlock()
	if err := d.DeployNow(); err != nil {
		t.Fatalf("force: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, core.ErrReportLater) {
			t.Fatalf("err %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the drain did not stop on Restart now")
	}
	if d.ExitCode() != ExitDeploy {
		t.Fatalf("exit code %d", d.ExitCode())
	}
	p, _ := d.DB.GetKV("pending_deploy")
	if !strings.Contains(p, `"cut":["t1","busy","late"]`) {
		t.Fatalf("pending %q", p)
	}
	if err := d.DeployNow(); !errors.Is(err, ErrNotDraining) {
		t.Fatalf("after the switch: %v", err)
	}
}

// SPEC 0.5 V0.83: a drain that ends by itself cuts nothing.
func TestDrainedDeployCutsNothing(t *testing.T) {
	d, h, _ := newDeployer(t, "")
	h.running = []string{"busy"}
	go func() { time.Sleep(40 * time.Millisecond); h.mu.Lock(); h.running = nil; h.mu.Unlock() }()
	if _, err := d.Deploy(context.Background(), "t1", req, "/src/fyr"); !errors.Is(err, core.ErrReportLater) {
		t.Fatalf("err %v", err)
	}
	if p, _ := d.DB.GetKV("pending_deploy"); strings.Contains(p, `"cut"`) {
		t.Fatalf("pending %q", p)
	}
}

// SPEC 0.5 V0.83: after the restart each cut thread gets "continue" once the
// new daemon is up, also after a rollback; the command id is the build's, so
// a second start does not send it twice.
func TestReportPendingContinuesCutThreads(t *testing.T) {
	d, h, _ := newDeployer(t, "")
	d.DB.SetKV("pending_deploy", `{"thread":"t1","build":"b2","req":{"action":"fyr.deploy"},"cut":["t1","busy"]}`)
	d.ReportPending()
	h.mu.Lock()
	got := strings.Join(h.continued, ",")
	h.mu.Unlock()
	if got != "t1 deploy-b2-continue-t1,busy deploy-b2-continue-busy" {
		t.Fatalf("continued %q", got)
	}

	f, fh, _ := newDeployer(t, "")
	f.DB.SetKV("pending_deploy", `{"thread":"t1","build":"b3","req":{"action":"fyr.deploy"},"cut":["busy"]}`)
	os.WriteFile(filepath.Join(f.Data, "deploy-failed.json"), []byte(`{"build":"b3","reason":"exited with code 1 after 0s"}`), 0o600)
	f.ReportPending()
	fh.mu.Lock()
	rolled := strings.Join(fh.continued, ",")
	fh.mu.Unlock()
	if rolled != "busy deploy-b3-continue-busy" {
		t.Fatalf("after a rollback: %q", rolled)
	}

	n, nh, _ := newDeployer(t, "")
	n.DB.SetKV("pending_deploy", `{"thread":"t1","build":"b4","req":{"action":"fyr.deploy"}}`)
	n.ReportPending()
	nh.mu.Lock()
	defer nh.mu.Unlock()
	if len(nh.continued) != 0 {
		t.Fatalf("a drained deploy sent %v", nh.continued)
	}
}
