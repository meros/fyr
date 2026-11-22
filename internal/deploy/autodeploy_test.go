package deploy

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// SPEC V0.111 (5): an auto-approved deploy builds only the commit the gate
// passed; a checkout that moved after the approval builds nothing.
func TestDeployRefusesAMovedCommit(t *testing.T) {
	d, _, steps := newDeployer(t, "")
	r := core.EffectReq{Action: "fyr.deploy", Args: map[string]any{"dir": "/src/fyr", "autoDeploy": true, "commit": "a1b2c3d4e5f6"}}
	_, err := d.Deploy(context.Background(), "t1", r, "/src/fyr")
	if err == nil || !strings.Contains(err.Error(), "The checkout moved from a1b2c3d to ok after the approval; nothing was built") {
		t.Fatalf("err %v", err)
	}
	for _, s := range *steps {
		if strings.HasPrefix(s, "make") {
			t.Fatalf("built a moved checkout: %v", *steps)
		}
	}
	// The same commit builds.
	r.Args["commit"] = "ok"
	if _, err := d.Deploy(context.Background(), "t1", r, "/src/fyr"); !errors.Is(err, core.ErrReportLater) {
		t.Fatalf("err %v", err)
	}
}

// SPEC V0.111 (4): Busy while a deploy builds and until the new build's
// probation ends; OnIdle hears the end of both.
func TestDeployBusy(t *testing.T) {
	d, _, _ := newDeployer(t, "test")
	release := make(chan struct{})
	inner := d.Run
	d.Run = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		if args[0] == "make" {
			<-release
		}
		return inner(ctx, dir, args...)
	}
	idle := make(chan struct{}, 4)
	d.OnIdle = func() { idle <- struct{}{} }
	if d.Busy() {
		t.Fatal("busy before any deploy")
	}
	done := make(chan error)
	go func() { _, err := d.Deploy(context.Background(), "t1", req, "/src/fyr"); done <- err }()
	deadline := time.Now().Add(2 * time.Second)
	for !d.Busy() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !d.Busy() {
		t.Fatal("not busy while building")
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("red tests passed")
	}
	if d.Busy() {
		t.Fatal("busy after a failed deploy")
	}
	select {
	case <-idle:
	case <-time.After(time.Second):
		t.Fatal("OnIdle did not hear the failed deploy")
	}
	// A switch waits out its probation in the next daemon.
	d.DB.SetKV("pending_deploy", `{"thread":"t1","build":"b2","req":{"action":"fyr.deploy"}}`)
	if !d.Busy() {
		t.Fatal("not busy during the probation")
	}
	markProbationOK(t, d, "b2")
	d.ReportPending(t.Context())
	select {
	case <-idle:
	case <-time.After(time.Second):
		t.Fatal("OnIdle did not hear the end of the probation")
	}
	if d.Busy() {
		t.Fatal("busy after the probation")
	}
}

// SPEC V0.111 (2): the gate passes a clean checkout whose HEAD is on
// origin's default branch, and says why it refuses anything else.
func TestGate(t *testing.T) {
	const head, main = "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222"
	cases := []struct {
		name, status, remote string
		ancestor             bool
		lsErr                bool
		why                  string
	}{
		{name: "head is main", remote: head},
		{name: "head is behind main", remote: main, ancestor: true},
		{name: "dirty", status: " M internal/x.go\n", remote: head, why: "the checkout has changes that are not committed"},
		{name: "untracked", status: "?? new.go\n", remote: head, why: "the checkout has changes that are not committed"},
		{name: "not on main", remote: main, why: "commit 1111111 is not on origin's default branch"},
		{name: "no remote", lsErr: true, why: "Fyr could not read origin's default branch"},
	}
	for _, c := range cases {
		run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
			switch strings.Join(args[3:], " ") {
			case "rev-parse HEAD":
				return []byte(head + "\n"), nil
			case "status --porcelain":
				return []byte(c.status), nil
			case "ls-remote origin HEAD":
				if c.lsErr {
					return []byte("fatal: no remote"), errors.New("exit 128")
				}
				return []byte(c.remote + "\tHEAD\n"), nil
			case "merge-base --is-ancestor " + head + " " + main:
				if c.ancestor {
					return nil, nil
				}
				return nil, errors.New("exit 1")
			}
			return nil, errors.New("unexpected " + strings.Join(args, " "))
		}
		commit, why := Gate(context.Background(), run, "/src/fyr")
		if !strings.Contains(why, c.why) || (c.why == "" && why != "") {
			t.Errorf("%s: why %q, want %q", c.name, why, c.why)
		}
		if c.why == "" && commit != head {
			t.Errorf("%s: commit %q", c.name, commit)
		}
	}
}
