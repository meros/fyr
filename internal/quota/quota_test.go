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
		{Profile{ID: "b", Binary: "claude", Env: []string{"CLAUDE_CONFIG_DIR=/home/u/.claude-acme"}}, Spec{Kind: KindAnthropic, ConfigDir: "/home/u/.claude-acme", BaseURL: "https://api.anthropic.com"}},
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
	s := svc(c, log, Profile{ID: "claudeAlt", Spec: &Spec{Kind: KindAnthropic, ConfigDir: dir, BaseURL: srv.URL}})
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
	s := svc(c, log, Profile{ID: "claudeAlt", Binary: "claude"}, Profile{ID: "ds", Spec: &Spec{Kind: KindDeepSeek}})
	reset := float64(c.t.Add(2 * time.Hour).Unix())
	s.Observe("claudeAlt", map[string]any{"status": "allowed", "unifiedWindows": map[string]any{
		"seven_day": map[string]any{"utilization": 0.61, "resetsAt": reset},
		"five_hour": map[string]any{"utilization": 0.342, "resetsAt": reset},
	}})
	st := s.Snapshot()[0]
	if len(st.Windows) != 2 || st.Windows[0].Name != "5 h" || st.Windows[0].UsedPct != 34.2 || st.Windows[1].UsedPct != 61 || st.UpdatedAt == nil || st.Windows[0].ResetsAt.Unix() != int64(reset) {
		t.Fatalf("%+v", st)
	}
	// An event with one window replaces only that one.
	s.Observe("claudeAlt", map[string]any{"status": "allowed_warning", "rateLimitType": "five_hour", "utilization": 0.9, "resetsAt": reset})
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

// SPEC 0.5 V0.44: the usage response may carry fields that are not windows
// (numbers, flags, other objects); both windows are still read. Found live: the
// 5 h window was missing while the 7 d one showed.
func TestAnthropicUsageToleratesOtherFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"five_hour":{"utilization":12.5,"resets_at":"2026-11-14T20:00:00Z"},"seven_day":{"utilization":91,"resets_at":"2026-11-16T03:00:00Z"},"seven_day_opus":null,"extra_usage":{"is_enabled":false,"monthly_limit":null},"tier":"max_20x","limited":false,"version":2}`)
	}))
	defer srv.Close()
	c, log := newClock(), &bytes.Buffer{}
	dir := t.TempDir()
	credentials(t, dir, c.t.Add(time.Hour))
	s := svc(c, log, Profile{ID: "claudeAlt", Spec: &Spec{Kind: KindAnthropic, ConfigDir: dir, BaseURL: srv.URL}})
	st := s.Get(context.Background())[0]
	if st.Error != "" || len(st.Windows) != 2 || st.Windows[0].Name != "5 h" || st.Windows[0].UsedPct != 12.5 || st.Windows[1].UsedPct != 91 {
		t.Fatalf("%+v", st)
	}
}

// SPEC 0.5 V0.44: a stream event updates windows but never hides the API's error.
func TestStreamEventKeepsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	c, log := newClock(), &bytes.Buffer{}
	dir := t.TempDir()
	credentials(t, dir, c.t.Add(time.Hour))
	s := svc(c, log, Profile{ID: "claudeAlt", Spec: &Spec{Kind: KindAnthropic, ConfigDir: dir, BaseURL: srv.URL}})
	if st := s.Get(context.Background())[0]; st.Error == "" {
		t.Fatalf("setup: no error %+v", st)
	}
	s.Observe("claudeAlt", map[string]any{"rateLimitType": "seven_day", "utilization": 0.91, "resetsAt": float64(c.t.Add(48 * time.Hour).Unix())})
	st := s.Snapshot()[0]
	if st.Error == "" {
		t.Fatalf("the stream event hid the API error: %+v", st)
	}
	if len(st.Windows) != 1 || st.Windows[0].UsedPct != 91 {
		t.Fatalf("windows %+v", st.Windows)
	}
}

// SPEC 0.5 V0.50: quota is keyed by the auth identity: two profiles on one
// login share one entry; SetProfiles keeps an identity's data.
func TestQuotaSharedIdentity(t *testing.T) {
	home := t.TempDir()
	s := New([]Profile{{ID: "a", Binary: "claude"}, {ID: "b", Binary: "claude", Spec: &Spec{Kind: KindAnthropic, ConfigDir: "~/.claude"}},
		{ID: "n", Binary: "claude", Spec: &Spec{Kind: KindAnthropic, ConfigDir: "~/.claude-n"}}}, home)
	if len(s.targets) != 2 {
		t.Fatalf("targets %d", len(s.targets))
	}
	s.Observe("a", map[string]any{"rateLimitType": "five_hour", "utilization": 0.5})
	st := s.Snapshot()
	if len(st) != 3 || st[1].Profile != "b" || len(st[1].Windows) != 1 || len(st[2].Windows) != 0 {
		t.Fatalf("%+v", st)
	}
	s.SetProfiles([]Profile{{ID: "b2", Binary: "claude"}})
	if st := s.Snapshot(); len(st) != 1 || st[0].Profile != "b2" || len(st[0].Windows) != 1 {
		t.Fatalf("kept: %+v", st)
	}
}

// SPEC 0.5 V0.50: a token account reads its usage with its own token; with
// no token it shows no numbers (never the folder's login).
func TestAnthropicTokenAccountWithoutToken(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o700)
	os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"), []byte(`{"claudeAiOauth":{"accessToken":"folder-token"}}`), 0o600)
	s := New([]Profile{{ID: "n", Spec: &Spec{Kind: KindAnthropic, Token: true}, Identity: "token:n",
		Key: func() (string, error) { return "", os.ErrNotExist }}}, home)
	t0 := s.targets[0]
	r := s.fetchAnthropic(context.Background(), t0)
	if r.err == nil || len(r.windows) != 0 {
		t.Fatalf("token account without a token got usage: %+v", r)
	}
}

// SPEC 0.5 V0.122 (1): a token account (a `claude setup-token` token, scope
// user:inference only) is never sent to the usage endpoint, which needs
// user:profile. Its windows come from its sessions' rate_limit_event, marked
// as such; before the first event it shows no error and no numbers.
func TestTokenAccountIsNeverPolled(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(429) }))
	defer srv.Close()
	c, log := newClock(), &bytes.Buffer{}
	s := svc(c, log, Profile{ID: "n", Identity: "token:n", Spec: &Spec{Kind: KindAnthropic, Token: true, BaseURL: srv.URL},
		Key: func() (string, error) { return fakeToken, nil }})
	for i := 0; i < 5; i++ {
		st := s.Get(context.Background())[0]
		if st.Error != "" || st.Stale || st.RetryAt != nil || len(st.Windows) != 0 || st.Source != SourceSession {
			t.Fatalf("step %d: %+v", i, st)
		}
		c.add(time.Hour)
	}
	if hits.Load() != 0 {
		t.Fatalf("a token account asked the usage endpoint %d times", hits.Load())
	}
	s.Observe("n", map[string]any{"status": "allowed", "rateLimitType": "seven_day", "unifiedWindows": map[string]any{
		"seven_day": map[string]any{"utilization": 0.67, "resetsAt": float64(c.t.Add(48 * time.Hour).Unix())}}})
	c.add(12 * time.Minute)
	st := s.Get(context.Background())[0]
	if len(st.Windows) != 1 || st.Windows[0].UsedPct != 67 || st.UpdatedAt == nil || c.t.Sub(*st.UpdatedAt) != 12*time.Minute || st.Error != "" || st.Source != SourceSession {
		t.Fatalf("after an event: %+v", st)
	}
	if hits.Load() != 0 {
		t.Fatalf("polled after an event: %d", hits.Load())
	}
	noLeak(t, []Status{st}, log)
}

// SPEC 0.5 V0.122 (1): a token account without a token still says so, with no request.
func TestTokenAccountWithoutTokenSaysSo(t *testing.T) {
	c, log := newClock(), &bytes.Buffer{}
	s := svc(c, log, Profile{ID: "n", Identity: "token:n", Spec: &Spec{Kind: KindAnthropic, Token: true, BaseURL: "http://127.0.0.1:1"},
		Key: func() (string, error) { return "", os.ErrNotExist }})
	if st := s.Get(context.Background())[0]; !strings.Contains(st.Error, "No token yet") || strings.Contains(st.Error, "expired") {
		t.Fatalf("%+v", st)
	}
}

// SPEC 0.5 V0.122 (2): a refused read is logged with its status and the
// body's error type and message, never the token, even when the reply echoes it.
func TestRefusalIsLoggedWithoutSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprintf(w, `{"type":"error","error":{"type":"permission_error","message":"OAuth token lacks scope user:profile (%s)"}}`, r.Header.Get("Authorization"))
	}))
	defer srv.Close()
	c, log := newClock(), &bytes.Buffer{}
	dir := t.TempDir()
	credentials(t, dir, c.t.Add(time.Hour))
	s := svc(c, log, Profile{ID: "p", Spec: &Spec{Kind: KindAnthropic, ConfigDir: dir, BaseURL: srv.URL}})
	sts := s.Get(context.Background())
	if !strings.Contains(log.String(), "HTTP 403") || !strings.Contains(log.String(), "permission_error") || !strings.Contains(log.String(), "lacks scope user:profile") {
		t.Fatalf("log: %s", log)
	}
	if strings.Contains(sts[0].Error, "permission_error") {
		t.Fatalf("the provider's words reached the owner's status: %+v", sts[0])
	}
	noLeak(t, sts, log)
}

// SPEC 0.5 V0.122 (3): the wait after a rate limit and the last data survive a
// restart, so many deploys never turn into frequent requests.
func TestBackoffSurvivesRestart(t *testing.T) {
	var hits atomic.Int32
	var limited atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if limited.Load() {
			w.WriteHeader(429)
			return
		}
		fmt.Fprint(w, `{"five_hour":{"utilization":10,"resets_at":null},"seven_day":{"utilization":20,"resets_at":null}}`)
	}))
	defer srv.Close()
	c, log := newClock(), &bytes.Buffer{}
	dir := t.TempDir()
	credentials(t, dir, c.t.Add(30*24*time.Hour))
	state := filepath.Join(t.TempDir(), "quota-state.json")
	p := Profile{ID: "p", Spec: &Spec{Kind: KindAnthropic, ConfigDir: dir, BaseURL: srv.URL}}
	start := func() *Service {
		s := svc(c, log)
		s.StatePath = state
		s.SetProfiles([]Profile{p})
		return s
	}
	s := start()
	s.Get(context.Background())
	limited.Store(true)
	c.add(TTL)
	s.Get(context.Background()) // 5 min wait
	c.add(5 * time.Minute)
	s.Get(context.Background()) // 10 min wait
	if hits.Load() != 3 {
		t.Fatalf("setup: %d reads", hits.Load())
	}
	// A deploy every minute for ten minutes: no read until the wait ends.
	for i := 0; i < 9; i++ {
		c.add(time.Minute)
		st := start().Get(context.Background())[0]
		if hits.Load() != 3 {
			t.Fatalf("restart %d read during the wait", i)
		}
		if !st.Stale || len(st.Windows) != 2 || st.RetryAt == nil {
			t.Fatalf("restart %d lost the data: %+v", i, st)
		}
	}
	c.add(time.Minute)
	if st := start().Get(context.Background())[0]; hits.Load() != 4 || st.RetryAt.Sub(c.t) != 20*time.Minute {
		t.Fatalf("after the wait: %d reads, %+v", hits.Load(), st)
	}
	b, _ := os.ReadFile(state)
	noLeak(t, nil, bytes.NewBuffer(append(log.Bytes(), b...)))
}

// V0.quota-reset-wake: ResetOf resolves a provider window's reset time, picks
// the binding window when omitted, fires OnUpdate outside the lock, and
// refuses stale reads, balance providers, and token accounts or windows
// without a future reset time.
func TestResetOfResolvesAndRefuses(t *testing.T) {
	var body atomic.Value
	var status atomic.Int32
	status.Store(200)
	body.Store(`{"five_hour":{"utilization":34,"resets_at":"2026-11-14T15:00:00Z"},"seven_day":{"utilization":61,"resets_at":"2026-11-18T08:00:00Z"}}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(status.Load()))
		fmt.Fprint(w, body.Load().(string))
	}))
	defer srv.Close()

	c, log := newClock(), &bytes.Buffer{}
	dir := t.TempDir()
	credentials(t, dir, c.t.Add(24*time.Hour))
	s := svc(c, log,
		Profile{ID: "claude", Spec: &Spec{Kind: KindAnthropic, ConfigDir: dir, BaseURL: srv.URL}},
		Profile{ID: "tok", Identity: "token:tok", Spec: &Spec{Kind: KindAnthropic, Token: true, BaseURL: srv.URL},
			Key: func() (string, error) { return fakeToken, nil }},
		Profile{ID: "ds", Env: []string{"DEEPSEEK_API_KEY=" + fakeKey}, Spec: &Spec{Kind: KindDeepSeek, BaseURL: srv.URL}},
	)
	var updates int
	s.OnUpdate = func(sts []Status) {
		// Calling Snapshot inside OnUpdate proves s.mu is not held.
		if len(s.Snapshot()) != len(sts) {
			t.Errorf("snapshot length mismatch")
		}
		updates++
	}

	// Omitted window picks soonest future reset (5 h) when none is exhausted.
	win, at, err := s.ResetOf(context.Background(), "claude", "")
	if err != nil || win != "5 h" || at.Format(time.RFC3339) != "2026-11-14T15:00:00Z" {
		t.Fatalf("default window: %q %v %v", win, at, err)
	}
	if updates == 0 {
		t.Fatal("OnUpdate did not run on fresh read")
	}

	// Aliases resolve cleanly.
	if win, at, err := s.ResetOf(context.Background(), "claude", "seven_day"); err != nil || win != "7 d" || at.Format(time.RFC3339) != "2026-11-18T08:00:00Z" {
		t.Fatalf("seven_day alias: %q %v %v", win, at, err)
	}
	if win, _, err := s.ResetOf(context.Background(), "claude", "5h"); err != nil || win != "5 h" {
		t.Fatalf("5h alias: %q %v", win, err)
	}

	// Exhausted 7 d window wins when window is omitted.
	body.Store(`{"five_hour":{"utilization":20,"resets_at":"2026-11-14T15:00:00Z"},"seven_day":{"utilization":100,"resets_at":"2026-11-18T08:00:00Z"}}`)
	c.add(TTL)
	if win, at, err := s.ResetOf(context.Background(), "claude", ""); err != nil || win != "7 d" || at.Format(time.RFC3339) != "2026-11-18T08:00:00Z" {
		t.Fatalf("exhausted 7 d: %q %v %v", win, at, err)
	}

	// Stale reading is refused with the reason.
	status.Store(429)
	body.Store(`{"error":{"type":"rate_limit_error","message":"slow down"}}`)
	c.add(TTL)
	if _, _, err := s.ResetOf(context.Background(), "claude", "5 h"); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale read not refused: %v", err)
	}

	// Token account before any session is refused.
	if _, _, err := s.ResetOf(context.Background(), "tok", ""); err == nil || !strings.Contains(err.Error(), "token account") {
		t.Fatalf("token account without session not refused: %v", err)
	}
	// Token account with a session event that carries no resetsAt is refused.
	prevUpdates := updates
	s.Observe("tok", map[string]any{"rateLimitType": "five_hour", "utilization": 0.9})
	if updates <= prevUpdates {
		t.Fatal("OnUpdate did not run on Observe")
	}
	if _, _, err := s.ResetOf(context.Background(), "tok", "5 h"); err == nil || !strings.Contains(err.Error(), "no reset time") {
		t.Fatalf("token account without resetsAt not refused: %v", err)
	}
	// Token account whose session resetsAt is in the past is refused.
	s.Observe("tok", map[string]any{"rateLimitType": "five_hour", "utilization": 0.9, "resetsAt": float64(c.t.Add(-time.Minute).Unix())})
	if _, _, err := s.ResetOf(context.Background(), "tok", "5 h"); err == nil || !strings.Contains(err.Error(), "no future reset time") {
		t.Fatalf("past resetsAt not refused: %v", err)
	}
	// Balance provider and unknown provider are refused.
	if _, _, err := s.ResetOf(context.Background(), "ds", ""); err == nil || !strings.Contains(err.Error(), "balance") {
		t.Fatalf("balance provider not refused: %v", err)
	}
	if _, _, err := s.ResetOf(context.Background(), "unknown", ""); err == nil || !strings.Contains(err.Error(), "no quota tracking") {
		t.Fatalf("unknown provider not refused: %v", err)
	}
	noLeak(t, s.Snapshot(), log)
}
