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

// shipRunner answers git like a checkout at head with an origin remote, in
// which the commits of in are ancestors of head; the rest goes to next.
func shipRunner(head string, in map[string]bool, next Runner) Runner {
	return func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		j := strings.Join(args, " ")
		switch {
		case strings.HasSuffix(j, "rev-parse HEAD"):
			return []byte(head + "\n"), nil
		case strings.HasSuffix(j, "remote get-url origin"):
			return []byte("git@github.com:meros/fyr.git\n"), nil
		case strings.Contains(j, "merge-base --is-ancestor"):
			if args[len(args)-1] == head && in[args[len(args)-2]] {
				return nil, nil
			}
			return nil, errors.New("exit 1")
		}
		return next(ctx, dir, args...)
	}
}

// SPEC 0.5 V0.82 (2): the deployer records the commit it built and the
// remote with the pending deploy; the new daemon's success report hands
// them on with a test of which merge commits the build holds.
func TestV082DeployRecordsCommitAndShips(t *testing.T) {
	d, h, _ := newDeployer(t, "")
	d.Run = shipRunner("c0ffee", map[string]bool{"m71": true}, d.Run)
	if _, err := d.Deploy(context.Background(), "t1", req, "/src/fyr"); !errors.Is(err, core.ErrReportLater) {
		t.Fatalf("err %v", err)
	}
	raw, _ := d.DB.GetKV("pending_deploy")
	if !strings.Contains(raw, `"commit":"c0ffee"`) || !strings.Contains(raw, `"remote":"git@github.com:meros/fyr.git"`) || !strings.Contains(raw, `"dir":"/src/fyr"`) {
		t.Fatalf("pending %s", raw)
	}
	// The new daemon: the same store, a fresh deployer.
	d2, h2, _ := newDeployer(t, "")
	d2.DB = d.DB
	d2.Run = shipRunner("c0ffee", map[string]bool{"m71": true}, d2.Run)
	got := make(chan Shipped, 1)
	d2.OnShipped = func(s Shipped) { got <- s }
	markProbationOK(t, d2, "")
	d2.ReportPending(t.Context())
	var s Shipped
	select {
	case s = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("success was not shipped")
	}
	if s.Commit != "c0ffee" || s.Remote != "git@github.com:meros/fyr.git" || s.Build == "" || !s.Contains("m71") || s.Contains("m99") {
		t.Fatalf("shipped %+v", s)
	}
	// The thread hears of it after the ship (V0.deploy-report-flake): wait
	// for the report itself, not a fixed sleep.
	r2 := h2.reportsAfter(1)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(r2) != 1 || !strings.HasPrefix(r2[0], "t1 ok") || len(h.reports) != 0 {
		t.Fatalf("reports %v %v", h.reports, r2)
	}
}

// SPEC 0.5 V0.82 (2): a rollback ships nothing.
func TestV082RollbackShipsNothing(t *testing.T) {
	d, _, _ := newDeployer(t, "")
	d.DB.SetKV("pending_deploy", `{"thread":"t1","build":"b2","commit":"c0ffee","remote":"x","dir":"/src/fyr","req":{"action":"fyr.deploy"}}`)
	writeFailed(t, d)
	shipped := false
	d.OnShipped = func(Shipped) { shipped = true }
	d.ReportPending(t.Context())
	time.Sleep(30 * time.Millisecond)
	if shipped {
		t.Fatal("a rolled-back build was shipped")
	}
}

func writeFailed(t *testing.T, d *Deployer) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(d.Data, "deploy-failed.json"), []byte(`{"build":"b2","reason":"exited with code 1 after 0s"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// SPEC 0.5 V0.82 (2): the client-only path ships when it succeeds.
func TestV082ClientOnlyShips(t *testing.T) {
	d, _, _ := newDeployer(t, "")
	dir, _ := clientOnlySetup(t, d)
	d.Run = shipRunner("beef", map[string]bool{"m73": true}, d.Run)
	var s *Shipped
	d.OnShipped = func(x Shipped) { s = &x }
	if _, err := d.Deploy(context.Background(), "t1", req, dir); err != nil {
		t.Fatal(err)
	}
	if s == nil || s.Commit != "beef" || s.Build == "" || !s.Contains("m73") {
		t.Fatalf("shipped %+v", s)
	}
}
