package deploy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// V0.persist-runtime-state: GitHub's "wait until" for the release API
// outlives a restart: the new daemon sends no request before the reset.
func TestRateLimitHoldSurvivesRestart(t *testing.T) {
	_, k, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Unix(1_800_000_000, 0)
	g := newFakeGitHub(t, relSHA, k)
	limited := true
	g.handle = func(w http.ResponseWriter, q *http.Request) bool {
		if !limited {
			return false
		}
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(now.Add(20*time.Minute).Unix(), 10))
		w.WriteHeader(403)
		fmt.Fprint(w, `{"message":"API rate limit exceeded"}`)
		return true
	}
	db := &kv{m: map[string]string{}}
	r := g.releases()
	r.KV, r.Now = db, func() time.Time { return now }
	r.compare(context.Background(), oldSHA, relSHA)
	limited = false

	r2 := g.releases() // the restarted daemon
	r2.KV, r2.Now = db, func() time.Time { return now.Add(time.Minute) }
	var e errRateLimited
	if _, err := r2.compare(context.Background(), oldSHA, relSHA); !errors.As(err, &e) || len(g.requests()) != 1 {
		t.Fatalf("after a restart: %v, %d requests", err, len(g.requests()))
	}
	r3 := g.releases()
	r3.KV, r3.Now = db, func() time.Time { return now.Add(21 * time.Minute) }
	if _, err := r3.compare(context.Background(), oldSHA, relSHA); err != nil || len(g.requests()) != 2 {
		t.Fatalf("after the reset: %v, %d requests", err, len(g.requests()))
	}
	if v, _ := db.GetKV(apiHoldKey); v != "" {
		t.Fatalf("a passed hold is still stored: %q", v)
	}
}

// #156: the success report waits only for the launcher's probation-ok, with
// no fallback timer; a daemon that stops first leaves pending_deploy for the
// next start, which reports it.
func TestReportPendingWaitsForProbationOKAcrossRestart(t *testing.T) {
	d, h, _ := newDeployer(t, "")
	d.DB.SetKV("pending_deploy", `{"thread":"t1","build":"b2","req":{"action":"fyr.deploy"}}`)
	ctx, stop := context.WithCancel(context.Background())
	d.ReportPending(ctx)
	time.Sleep(100 * time.Millisecond)
	h.mu.Lock()
	early := len(h.reports)
	h.mu.Unlock()
	if early != 0 {
		t.Fatalf("reported before probation-ok: %v", h.reports)
	}
	stop() // fyrd restarts during the probation
	time.Sleep(50 * time.Millisecond)
	if raw, _ := d.DB.GetKV("pending_deploy"); !strings.Contains(raw, "b2") {
		t.Fatalf("pending_deploy lost on stop: %q", raw)
	}

	d2, h2, _ := newDeployer(t, "") // the next daemon: same data dir and store
	d2.Data, d2.DB = d.Data, d.DB
	markProbationOK(t, d2, "b2")
	d2.ReportPending(t.Context())
	if r := h2.reportsAfter(1); len(r) != 1 || !strings.HasPrefix(r[0], "t1 ok") {
		t.Fatalf("after restart: %v", r)
	}
}

// V0.survive-update: a launcher from before probation-ok (#165), which says no
// pid (#172), never writes it, so the report comes after OldLauncher instead
// of never; auto-update would otherwise wait on pending_deploy for good.
func TestReportPendingUnderAnOldLauncher(t *testing.T) {
	d, h, _ := newDeployer(t, "")
	d.OldLauncher = 50 * time.Millisecond
	d.DB.SetKV("pending_deploy", `{"thread":"t1","build":"b2","req":{"action":"fyr.deploy"}}`)
	d.ReportPending(t.Context())
	if r := h.reportsAfter(1); len(r) != 1 || !strings.HasPrefix(r[0], "t1 ok") {
		t.Fatalf("under an old launcher: %v", r)
	}
	if raw, _ := d.DB.GetKV("pending_deploy"); raw != "" {
		t.Fatalf("pending_deploy kept: %q", raw)
	}
}
