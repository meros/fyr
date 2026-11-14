package quota

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Fixture secrets: the tests assert they never reach a status, error or log.
const (
	fakeToken = "sk-ant-oat01-FIXTURE-TOKEN-DO-NOT-LEAK"
	fakeKey   = "sk-FIXTURE-KEY-DO-NOT-LEAK"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }
func newClock() *clock               { return &clock{time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)} }
func writeFile(t *testing.T, p, s string) {
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, []byte(s), 0o600)
}

func credentials(t *testing.T, dir string, expires time.Time) {
	writeFile(t, filepath.Join(dir, ".credentials.json"),
		fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"refreshToken":"r-FIXTURE","expiresAt":%d}}`, fakeToken, expires.UnixMilli()))
}

// noLeak fails when a secret shows in the statuses or the log.
func noLeak(t *testing.T, sts []Status, log *bytes.Buffer) {
	t.Helper()
	b, _ := json.Marshal(sts)
	for _, sec := range []string{fakeToken, fakeKey, "FIXTURE"} {
		if strings.Contains(string(b), sec) || strings.Contains(log.String(), sec) {
			t.Fatalf("secret %q leaked: %s | %s", sec, b, log)
		}
	}
}

func svc(c *clock, log *bytes.Buffer, ps ...Profile) *Service {
	s := New(ps, "/home/u")
	s.Now = c.now
	s.Getenv = func(string) string { return "" }
	s.Logf = func(f string, a ...any) { fmt.Fprintf(log, f+"\n", a...) }
	return s
}

func TestResolveDefaults(t *testing.T) {
	cases := []struct {
		p    Profile
		want Spec
	}{
		{Profile{ID: "a", Binary: "claude"}, Spec{Kind: KindAnthropic, ConfigDir: "/home/u/.claude", BaseURL: "https://api.anthropic.com"}},
		{Profile{ID: "b", Binary: "claude", Env: []string{"CLAUDE_CONFIG_DIR=/home/u/.claude-norban"}}, Spec{Kind: KindAnthropic, ConfigDir: "/home/u/.claude-norban", BaseURL: "https://api.anthropic.com"}},
		{Profile{ID: "c", Binary: "claude", Env: []string{"ANTHROPIC_BASE_URL=https://x"}}, Spec{Kind: KindNone}},
		{Profile{ID: "d", Binary: "claude-deepseek"}, Spec{Kind: KindNone}},
		{Profile{ID: "e", Binary: "claude-deepseek", Spec: &Spec{Kind: KindDeepSeek}}, Spec{Kind: KindDeepSeek, KeyEnv: "DEEPSEEK_API_KEY", BaseURL: "https://api.deepseek.com"}},
		{Profile{ID: "f", Spec: &Spec{Kind: KindOpenRouter, KeyFile: "~/keys/or", BaseURL: "http://h/v1/"}}, Spec{Kind: KindOpenRouter, KeyFile: "/home/u/keys/or", BaseURL: "http://h/v1"}},
		{Profile{ID: "g", Binary: "x", Spec: &Spec{Kind: KindAnthropic, ConfigDir: "~/.c2"}}, Spec{Kind: KindAnthropic, ConfigDir: "/home/u/.c2", BaseURL: "https://api.anthropic.com"}},
		{Profile{ID: "h", Spec: &Spec{Kind: "bogus"}}, Spec{Kind: KindNone}},
	}
	for _, c := range cases {
		if got := Resolve(c.p, "/home/u"); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.p.ID, got, c.want)
		}
	}
	b, _ := json.Marshal(&Spec{Kind: KindDeepSeek, KeyFile: "/run/secrets/k", KeyEnv: "X"})
	if string(b) != `{"kind":"deepseek"}` {
		t.Errorf("only the kind leaves the host, got %s", b)
	}
}

func TestAnthropicUsage(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/api/oauth/usage" || r.Header.Get("Authorization") != "Bearer "+fakeToken || r.Header.Get("anthropic-beta") != "oauth-2025-04-20" {
			w.WriteHeader(400)
			return
		}
		fmt.Fprint(w, `{"five_hour":{"utilization":34.0,"resets_at":"2026-11-14T13:00:00.123+00:00"},"seven_day":{"utilization":61,"resets_at":"2026-11-18T08:00:00Z"},"seven_day_opus":null}`)
	}))
	defer srv.Close()
	c, log := newClock(), &bytes.Buffer{}
	dir := t.TempDir()
	credentials(t, dir, c.t.Add(time.Hour))
	s := svc(c, log, Profile{ID: "claudeAgent", Spec: &Spec{Kind: KindAnthropic, ConfigDir: dir, BaseURL: srv.URL}})
	sts := s.Get(context.Background())
	if len(sts) != 1 || sts[0].Error != "" || len(sts[0].Windows) != 2 {
		t.Fatalf("got %+v", sts)
	}
	w := sts[0].Windows
	if w[0].Name != "5 h" || w[0].UsedPct != 34 || w[0].ResetsAt.Format(time.RFC3339) != "2026-11-14T13:00:00Z" || w[1].Name != "7 d" || w[1].UsedPct != 61 {
		t.Fatalf("windows %+v", w)
	}
	// Cached for five minutes.
	c.add(4 * time.Minute)
	s.Get(context.Background())
	if hits.Load() != 1 {
		t.Fatalf("read again inside the cache time: %d", hits.Load())
	}
	c.add(2 * time.Minute)
	s.Get(context.Background())
	if hits.Load() != 2 {
		t.Fatalf("not read after the cache time: %d", hits.Load())
	}
	noLeak(t, sts, log)
}

func TestAnthropicExpiredAndMissingLogin(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(401) }))
	defer srv.Close()
	c, log := newClock(), &bytes.Buffer{}
	expired, live, none := t.TempDir(), t.TempDir(), t.TempDir()
	credentials(t, expired, c.t.Add(-time.Minute))
	credentials(t, live, c.t.Add(time.Hour))
	s := svc(c, log,
		Profile{ID: "old", Spec: &Spec{Kind: KindAnthropic, ConfigDir: expired, BaseURL: srv.URL}},
		Profile{ID: "rejected", Spec: &Spec{Kind: KindAnthropic, ConfigDir: live, BaseURL: srv.URL}},
		Profile{ID: "none", Spec: &Spec{Kind: KindAnthropic, ConfigDir: none, BaseURL: srv.URL}})
	sts := s.Get(context.Background())
	if sts[0].Error != loginExpired || sts[1].Error != loginExpired || !strings.Contains(sts[2].Error, "No Claude login") {
		t.Fatalf("got %+v", sts)
	}
	if hits.Load() != 1 {
		t.Fatalf("an expired login must not be sent: %d requests", hits.Load())
	}
	if sts[0].Stale {
		t.Fatal("stale without data")
	}
	noLeak(t, sts, log)
}

func TestRateLimitBackoffKeepsStaleData(t *testing.T) {
	var mode atomic.Int32 // 0 ok, 1 429, 2 rate_limit_error body
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch mode.Load() {
		case 1:
			w.WriteHeader(429)
			fmt.Fprintf(w, `{"error":{"type":"rate_limit_error","message":"slow down %s"}}`, r.Header.Get("Authorization"))
		case 2:
			w.WriteHeader(400)
			fmt.Fprint(w, `{"type":"error","error":{"type":"rate_limit_error","message":"x"}}`)
		default:
			fmt.Fprint(w, `{"five_hour":{"utilization":10,"resets_at":null},"seven_day":{"utilization":20,"resets_at":null}}`)
		}
	}))
	defer srv.Close()
	c, log := newClock(), &bytes.Buffer{}
	dir := t.TempDir()
	credentials(t, dir, c.t.Add(24*time.Hour))
	s := svc(c, log, Profile{ID: "p", Spec: &Spec{Kind: KindAnthropic, ConfigDir: dir, BaseURL: srv.URL}})
	s.Get(context.Background())
	mode.Store(1)
	want := []time.Duration{5, 10, 20, 30, 30}
	c.add(TTL)
	for i, m := range want {
		if i == 2 {
			mode.Store(2)
		}
		before := hits.Load()
		st := s.Get(context.Background())[0]
		if hits.Load() != before+1 {
			t.Fatalf("step %d: no read when due", i)
		}
		if !st.Stale || len(st.Windows) != 2 || st.Windows[0].UsedPct != 10 || !strings.Contains(st.Error, "Rate limited") || st.RetryAt == nil {
			t.Fatalf("step %d: %+v", i, st)
		}
		if got := st.RetryAt.Sub(c.t); got != m*time.Minute {
			t.Fatalf("step %d: retry in %v, want %v min", i, got, m)
		}
		c.add(m*time.Minute - time.Second)
		s.Get(context.Background())
		if hits.Load() != before+1 {
			t.Fatalf("step %d: read during the wait", i)
		}
		c.add(time.Second)
	}
	// Success clears the wait and the note.
	mode.Store(0)
	c.add(time.Hour)
	st := s.Get(context.Background())[0]
	if st.Stale || st.Error != "" || st.RetryAt != nil {
		t.Fatalf("after success: %+v", st)
	}
	c.add(TTL)
	mode.Store(1)
	if st := s.Get(context.Background())[0]; st.RetryAt.Sub(c.t) != 5*time.Minute {
		t.Fatalf("backoff not reset: %+v", st)
	}
	noLeak(t, nil, log)
}

func TestDeepSeekBalance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/balance" || r.Header.Get("Authorization") != "Bearer "+fakeKey {
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"12.40","granted_balance":"0.00","topped_up_balance":"12.40"}]}`)
	}))
	defer srv.Close()
	c, log := newClock(), &bytes.Buffer{}
	keyFile := filepath.Join(t.TempDir(), "deepseek.key")
	writeFile(t, keyFile, fakeKey+"\n")
	s := svc(c, log,
		Profile{ID: "file", Spec: &Spec{Kind: KindDeepSeek, KeyFile: keyFile, BaseURL: srv.URL}},
		Profile{ID: "env", Env: []string{"MY_KEY=" + fakeKey}, Spec: &Spec{Kind: KindDeepSeek, KeyEnv: "MY_KEY", BaseURL: srv.URL}},
		Profile{ID: "daemonEnv", Spec: &Spec{Kind: KindDeepSeek, BaseURL: srv.URL}},
		Profile{ID: "nokey", Spec: &Spec{Kind: KindDeepSeek, KeyEnv: "NOPE", BaseURL: srv.URL}},
		Profile{ID: "wrong", Env: []string{"K=bad"}, Spec: &Spec{Kind: KindDeepSeek, KeyEnv: "K", BaseURL: srv.URL}})
	s.Getenv = func(k string) string {
		if k == "DEEPSEEK_API_KEY" {
			return fakeKey
		}
		return ""
	}
	sts := s.Get(context.Background())
	for _, st := range sts[:3] {
		if st.Error != "" || st.Balance == nil || *st.Balance.Amount != 12.4 || st.Balance.Currency != "CNY" {
			t.Fatalf("%s: %+v", st.Profile, st)
		}
	}
	if !strings.Contains(sts[3].Error, "No API key") || !strings.Contains(sts[4].Error, "rejected") {
		t.Fatalf("errors: %+v %+v", sts[3], sts[4])
	}
	noLeak(t, sts, log)
}

func TestOpenRouterKey(t *testing.T) {
	var limited atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/key" || r.Header.Get("Authorization") != "Bearer "+fakeKey {
			w.WriteHeader(401)
			return
		}
		if limited.Load() {
			fmt.Fprint(w, `{"data":{"label":"x","usage":7.5,"limit":20,"limit_remaining":12.5,"is_free_tier":false}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"label":"x","usage":3.25,"limit":null,"limit_remaining":null}}`)
	}))
	defer srv.Close()
	c, log := newClock(), &bytes.Buffer{}
	s := svc(c, log, Profile{ID: "or", Env: []string{"OPENROUTER_API_KEY=" + fakeKey}, Spec: &Spec{Kind: KindOpenRouter, BaseURL: srv.URL + "/api/v1"}})
	st := s.Get(context.Background())[0]
	if st.Balance == nil || st.Balance.Amount != nil || *st.Balance.Spent != 3.25 || st.Balance.Currency != "USD" {
		t.Fatalf("spent: %+v", st)
	}
	limited.Store(true)
	c.add(TTL)
	st = s.Get(context.Background())[0]
	if st.Balance == nil || *st.Balance.Amount != 12.5 {
		t.Fatalf("left: %+v", st)
	}
	noLeak(t, []Status{st}, log)
}

func TestTransientFailureRetriesSoon(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(502)
			return
		}
		fmt.Fprint(w, `{"balance_infos":[{"currency":"USD","total_balance":"1"}]}`)
	}))
	defer srv.Close()
	c, log := newClock(), &bytes.Buffer{}
	s := svc(c, log, Profile{ID: "d", Env: []string{"DEEPSEEK_API_KEY=" + fakeKey}, Spec: &Spec{Kind: KindDeepSeek, BaseURL: srv.URL}})
	if st := s.Get(context.Background())[0]; !strings.Contains(st.Error, "502") || st.Stale {
		t.Fatalf("%+v", st)
	}
	fail.Store(false)
	c.add(retryShort)
	if st := s.Get(context.Background())[0]; st.Error != "" || st.Balance == nil {
		t.Fatalf("%+v", st)
	}
	noLeak(t, nil, log)
}

func TestStreamEventUpdatesWindows(t *testing.T) {
	c, log := newClock(), &bytes.Buffer{}
	s := svc(c, log, Profile{ID: "claudeAgent", Binary: "claude"}, Profile{ID: "ds", Spec: &Spec{Kind: KindDeepSeek}})
	reset := float64(c.t.Add(2 * time.Hour).Unix())
	s.Observe("claudeAgent", map[string]any{"status": "allowed", "unifiedWindows": map[string]any{
		"seven_day": map[string]any{"utilization": 0.61, "resetsAt": reset},
		"five_hour": map[string]any{"utilization": 0.342, "resetsAt": reset},
	}})
	st := s.Snapshot()[0]
	if len(st.Windows) != 2 || st.Windows[0].Name != "5 h" || st.Windows[0].UsedPct != 34.2 || st.Windows[1].UsedPct != 61 || st.UpdatedAt == nil || st.Windows[0].ResetsAt.Unix() != int64(reset) {
		t.Fatalf("%+v", st)
	}
	// An event with one window replaces only that one.
	s.Observe("claudeAgent", map[string]any{"status": "allowed_warning", "rateLimitType": "five_hour", "utilization": 0.9, "resetsAt": reset})
	st = s.Snapshot()[0]
	if st.Windows[0].UsedPct != 90 || st.Windows[1].UsedPct != 61 {
		t.Fatalf("%+v", st.Windows)
	}
	// Other kinds and unknown profiles ignore it.
	s.Observe("ds", map[string]any{"rateLimitType": "five_hour", "utilization": 0.5})
	s.Observe("nobody", map[string]any{"rateLimitType": "five_hour", "utilization": 0.5})
	if s.Snapshot()[1].Windows != nil {
		t.Fatal("deepseek took windows")
	}
}

func TestNoneProfilesAreLeftOut(t *testing.T) {
	s := New([]Profile{{ID: "x", Binary: "wrapper"}, {ID: "y", Binary: "claude"}}, "/h")
	if sts := s.Snapshot(); len(sts) != 1 || sts[0].Profile != "y" {
		t.Fatalf("%+v", sts)
	}
}

func TestGetReturnsWhenContextEnds(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-block }))
	defer srv.Close()
	defer close(block)
	c, log := newClock(), &bytes.Buffer{}
	s := svc(c, log, Profile{ID: "d", Env: []string{"DEEPSEEK_API_KEY=k"}, Spec: &Spec{Kind: KindDeepSeek, BaseURL: srv.URL}})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if sts := s.Get(ctx); len(sts) != 1 || time.Since(start) > 2*time.Second {
		t.Fatalf("Get waited %v", time.Since(start))
	}
}
