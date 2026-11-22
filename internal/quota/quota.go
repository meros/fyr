// Package quota reads how much of each profile's provider allowance is left
// (SPEC 0.5 V0.44): Claude subscription windows through the CLI's stored
// OAuth login, DeepSeek and OpenRouter balances through an API key. It reads
// the logins on the host at runtime; a token or key never leaves this
// package (not in a log line, an error or a Status).
package quota

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/meros/fyr/internal/cli/claudecli"
	"github.com/meros/fyr/internal/fsutil"
	"github.com/meros/fyr/internal/hostfs"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	KindAnthropic  = "anthropic-oauth"
	KindDeepSeek   = "deepseek"
	KindOpenRouter = "openrouter"
	KindNone       = "none"
)

// Spec says where a profile's quota comes from. Only Kind is sent to
// clients (MarshalJSON); the rest stays on the host.
type Spec struct {
	Kind string `json:"kind"`
	// ConfigDir (anthropic-oauth): the CLI's config folder with
	// .credentials.json; default the profile's CLAUDE_CONFIG_DIR, else ~/.claude.
	ConfigDir string `json:"configDir,omitempty"`
	// KeyEnv, KeyFile (deepseek, openrouter): where the API key is. KeyFile
	// wins; KeyEnv is looked up in the profile's env, then the daemon's.
	KeyEnv  string `json:"keyEnv,omitempty"`
	KeyFile string `json:"keyFile,omitempty"`
	// Token (anthropic-oauth, V0.50): the account logs in with a token from
	// Fyr's secret store; usage is read with that token, never the folder's login.
	Token bool `json:"token,omitempty"`
	// BaseURL overrides the provider's API root.
	BaseURL string `json:"baseURL,omitempty"`
}

// MarshalJSON sends only the kind: paths and variable names stay on the host.
func (s Spec) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind string `json:"kind"`
	}{s.Kind})
}

// Profile is what Resolve needs from a Fyr profile.
type Profile struct {
	ID     string
	Binary string
	Env    []string // KEY=VALUE, as the profile starts its agent
	Spec   *Spec
	// Identity names the account behind the profile (decision 0015): two
	// profiles with one identity share one quota entry and one provider
	// read. Empty means Identity(spec).
	Identity string
	// Key reads the API key from Fyr's secret store (V0.50); it wins over
	// keyFile and keyEnv. Optional.
	Key func() (string, error)
}

// Identity is the default quota identity of a resolved spec: the login
// folder for anthropic-oauth, else the kind, key source and endpoint.
func Identity(s Spec) string {
	if s.Kind == KindAnthropic {
		return "login:" + filepath.Clean(s.ConfigDir)
	}
	src := "file:" + s.KeyFile
	if s.KeyFile == "" {
		src = "env:" + s.KeyEnv
	}
	return s.Kind + ":" + src + "@" + s.BaseURL
}

var defaultBase = map[string]string{
	KindAnthropic:  "https://api.anthropic.com",
	KindDeepSeek:   "https://api.deepseek.com",
	KindOpenRouter: "https://openrouter.ai/api/v1",
}

var defaultKeyEnv = map[string]string{KindDeepSeek: "DEEPSEEK_API_KEY", KindOpenRouter: "OPENROUTER_API_KEY"}

// Resolve fills a profile's spec with defaults. Without a spec, a plain
// `claude` profile that does not point at another endpoint is
// anthropic-oauth; anything else is none.
func Resolve(p Profile, home string) Spec {
	var s Spec
	if p.Spec != nil && p.Spec.Kind != "" {
		s = *p.Spec
	} else if (p.Binary == "" || p.Binary == "claude") && envOf(p.Env, "ANTHROPIC_BASE_URL") == "" {
		s.Kind = KindAnthropic
	} else {
		s.Kind = KindNone
	}
	switch s.Kind {
	case KindAnthropic:
		if s.ConfigDir == "" {
			s.ConfigDir = envOf(p.Env, "CLAUDE_CONFIG_DIR")
		}
		if s.ConfigDir == "" {
			s.ConfigDir = "~/.claude"
		}
		s.ConfigDir = hostfs.ExpandHome(s.ConfigDir, home)
	case KindDeepSeek, KindOpenRouter:
		if s.KeyEnv == "" && s.KeyFile == "" {
			s.KeyEnv = defaultKeyEnv[s.Kind]
		}
		if s.KeyFile != "" {
			s.KeyFile = hostfs.ExpandHome(s.KeyFile, home)
		}
	case KindNone:
	default:
		s = Spec{Kind: KindNone}
	}
	if s.BaseURL == "" {
		s.BaseURL = defaultBase[s.Kind]
	}
	s.BaseURL = strings.TrimRight(s.BaseURL, "/")
	return s
}

func envOf(env []string, k string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], k+"="); ok {
			return v
		}
	}
	return ""
}

// Window is one Claude usage window.
type Window struct {
	Name     string     `json:"name"`
	UsedPct  float64    `json:"usedPct"`
	ResetsAt *time.Time `json:"resetsAt,omitempty"`
}

// Balance is a prepaid balance: Amount left, or Spent by a key without a limit.
type Balance struct {
	Amount   *float64 `json:"amount,omitempty"`
	Spent    *float64 `json:"spent,omitempty"`
	Currency string   `json:"currency"`
}

// Status is one profile's entry in GET /api/quota.
type Status struct {
	Profile   string     `json:"profile"`
	Kind      string     `json:"kind"`
	Windows   []Window   `json:"windows,omitempty"`
	Balance   *Balance   `json:"balance,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	Stale     bool       `json:"stale,omitempty"`
	Error     string     `json:"error,omitempty"`
	RetryAt   *time.Time `json:"retryAt,omitempty"`
	// Source is SourceSession when the windows come only from the sessions'
	// rate_limit_event (a token account, V0.122); UpdatedAt is then the
	// last event's time.
	Source string `json:"source,omitempty"`
}

// SourceSession: the windows come from the last session, never from a read.
const SourceSession = "session"

const (
	TTL        = 5 * time.Minute
	retryShort = time.Minute // a network failure or a 5xx
	fetchLimit = 10 * time.Second
)

// Backoff is the wait after each consecutive rate limit.
var Backoff = []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 30 * time.Minute}

type target struct {
	id   string // the identity
	spec Spec
	env  []string
	key  func() (string, error)
}

type entry struct {
	st       Status
	next     time.Time // no provider read before this
	limited  int       // consecutive rate limits
	fetching bool
}

// Service caches the quota of every profile that is not none.
type Service struct {
	Client *http.Client
	Now    func() time.Time
	Getenv func(string) string
	Logf   func(format string, args ...any)
	// StatePath keeps each identity's data and rate-limit wait across a
	// restart (V0.122); empty keeps them in memory only. Set it before the
	// first SetProfiles that should load it.
	StatePath string
	// OnUpdate runs outside s.mu whenever a fresh read or stream event updates
	// quota windows (V0.quota-reset-wake).
	OnUpdate func([]Status)

	loaded  bool
	home    string
	targets []target          // one per identity
	order   []string          // profile ids, in profile order
	ident   map[string]string // profile id → identity
	mu      sync.Mutex
	ent     map[string]*entry // by identity
}

// New resolves the profiles' specs; profiles of kind none are left out.
func New(profiles []Profile, home string) *Service {
	s := &Service{ent: map[string]*entry{}, home: home}
	s.SetProfiles(profiles)
	return s
}

// SetProfiles replaces the profiles (providers changed, V0.50); the data of
// an identity that stays is kept.
func (s *Service) SetProfiles(profiles []Profile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded && s.StatePath != "" {
		s.loaded = true
		s.load()
	}
	ent := map[string]*entry{}
	s.targets, s.order, s.ident = nil, nil, map[string]string{}
	for _, p := range profiles {
		sp := Resolve(p, s.home)
		if sp.Kind == KindNone {
			continue
		}
		id := p.Identity
		if id == "" {
			id = Identity(sp)
		}
		s.order = append(s.order, p.ID)
		s.ident[p.ID] = id
		if ent[id] != nil {
			continue
		}
		s.targets = append(s.targets, target{id: id, spec: sp, env: p.Env, key: p.Key})
		if e := s.ent[id]; e != nil && e.st.Kind == sp.Kind {
			ent[id] = e
		} else {
			ent[id] = &entry{st: Status{Kind: sp.Kind}}
		}
		if sp.Token {
			ent[id].st.Source = SourceSession
		} else {
			ent[id].st.Source = ""
		}
	}
	s.ent = ent
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// Get reads the providers whose data is due (older than TTL and outside a
// rate-limit wait), waits for them at most until ctx ends, and returns every
// profile's status.
func (s *Service) Get(ctx context.Context) []Status {
	now := s.now()
	done := make(chan struct{})
	var wg sync.WaitGroup
	var tokens []target
	s.mu.Lock()
	for _, t := range s.targets {
		e := s.ent[t.id]
		if t.spec.Token {
			// V0.122: a setup-token token carries only user:inference and the
			// usage endpoint needs user:profile, so it is never asked; the
			// sessions' rate_limit_event fills the windows.
			tokens = append(tokens, t)
			continue
		}
		if e.fetching || now.Before(e.next) {
			continue
		}
		e.fetching = true
		wg.Add(1)
		go func(t target) {
			defer wg.Done()
			fctx, cancel := context.WithTimeout(context.Background(), fetchLimit)
			defer cancel()
			r := s.fetch(fctx, t)
			s.apply(t, r)
		}(t)
	}
	s.mu.Unlock()
	// The key is read outside s.mu: the registry behind it calls SetProfiles.
	for _, t := range tokens {
		has := hasToken(t)
		s.mu.Lock()
		if e := s.ent[t.id]; e != nil {
			e.st.Error, e.st.RetryAt, e.st.Stale = "", nil, false
			if !has {
				e.st.Error = noToken
			}
		}
		s.mu.Unlock()
	}
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
	return s.Snapshot()
}

// Snapshot returns the cached statuses without reading anything.
func (s *Service) Snapshot() []Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Status, 0, len(s.order))
	for _, pid := range s.order {
		st := s.ent[s.ident[pid]].st
		st.Profile = pid
		st.Windows = append([]Window(nil), st.Windows...)
		out = append(out, st)
	}
	return out
}

type errClass int

const (
	errOther errClass = iota
	errTransient
	errLimited
)

// fail is an error for the owner's eyes; its text never holds a secret.
type fail struct {
	class errClass
	msg   string
	// detail is for the log only: the status and the reply's error type and
	// message, with any secret taken out.
	detail string
}

func (f *fail) Error() string { return f.msg }

type result struct {
	windows []Window
	balance *Balance
	err     *fail
}

func (s *Service) apply(t target, r result) {
	now := s.now()
	s.mu.Lock()
	e := s.ent[t.id]
	if e == nil {
		s.mu.Unlock()
		return // the providers changed while this read ran
	}
	e.fetching = false
	if r.err == nil {
		e.limited = 0
		e.next = now.Add(TTL)
		e.st = Status{Kind: t.spec.Kind, Windows: r.windows, Balance: r.balance, UpdatedAt: &now}
		s.save()
		cb := s.OnUpdate
		s.mu.Unlock()
		if cb != nil {
			cb(s.Snapshot())
		}
		return
	}
	defer s.mu.Unlock()
	defer s.save()
	if r.err.detail != "" {
		s.logf("quota %s: %s (%s)", t.id, r.err.msg, r.err.detail)
	} else {
		s.logf("quota %s: %s", t.id, r.err.msg)
	}
	e.st.Error, e.st.RetryAt = r.err.msg, nil
	e.st.Stale = e.st.UpdatedAt != nil
	switch r.err.class {
	case errLimited:
		d := Backoff[min(e.limited, len(Backoff)-1)]
		e.limited++
		e.next = now.Add(d)
		at := e.next
		e.st.RetryAt = &at
	case errTransient:
		e.next = now.Add(retryShort)
	default:
		e.next = now.Add(TTL)
	}
}

// Observe takes a `rate_limit_event`'s rate_limit_info from a running
// session of the profile (a free update between provider reads).
func (s *Service) Observe(profile string, info map[string]any) {
	ws := streamWindows(info)
	if len(ws) == 0 {
		return
	}
	now := s.now()
	s.mu.Lock()
	e, ok := s.ent[s.ident[profile]]
	if !ok || e.st.Kind != KindAnthropic {
		s.mu.Unlock()
		return
	}
	for _, w := range ws {
		found := false
		for i := range e.st.Windows {
			if e.st.Windows[i].Name == w.Name {
				e.st.Windows[i], found = w, true
			}
		}
		if !found {
			e.st.Windows = append(e.st.Windows, w)
		}
	}
	sortWindows(e.st.Windows)
	e.st.UpdatedAt = &now
	// The stream carries only the window that binds; it never hides an API error.
	e.st.Stale = false
	s.save()
	cb := s.OnUpdate
	s.mu.Unlock()
	if cb != nil {
		cb(s.Snapshot())
	}
}

// NormalizeWindow maps common window spellings ("5h", "five_hour", "7d",
// "seven_day") to Fyr's canonical names ("5 h", "7 d").
func NormalizeWindow(name string) string {
	s := strings.TrimSpace(name)
	switch strings.ToLower(strings.ReplaceAll(s, " ", "")) {
	case "5h", "five_hour", "5hours", "5hour":
		return "5 h"
	case "7d", "seven_day", "7days", "7day":
		return "7 d"
	}
	return s
}

// ResetOf resolves the reset time of a profile's quota window for a quota wake
// (V0.quota-reset-wake). It refreshes due OAuth profiles via Get (never polling
// token accounts or rate-limited profiles) and refuses stale readings, token
// accounts without a known future reset time, balance providers, and windows
// with no reset time, never inventing a time.
func (s *Service) ResetOf(ctx context.Context, profile, window string) (string, time.Time, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, fetchLimit)
		defer cancel()
	}
	sts := s.Get(ctx)
	var st *Status
	for i := range sts {
		if sts[i].Profile == profile {
			st = &sts[i]
			break
		}
	}
	if st == nil {
		return "", time.Time{}, fmt.Errorf("provider %q has no quota tracking", profile)
	}
	if st.Balance != nil || (st.Kind != KindAnthropic && len(st.Windows) == 0) {
		return "", time.Time{}, fmt.Errorf("provider %q uses a balance, not reset windows", profile)
	}
	if st.Stale {
		if st.Error != "" {
			return "", time.Time{}, fmt.Errorf("quota for %q is stale (%s); wait for a fresh reading", profile, st.Error)
		}
		return "", time.Time{}, fmt.Errorf("quota for %q is stale; wait for a fresh reading", profile)
	}
	if len(st.Windows) == 0 {
		if st.Source == SourceSession {
			return "", time.Time{}, fmt.Errorf("provider %q is a token account read only from running sessions; no reset time is known", profile)
		}
		if st.Error != "" {
			return "", time.Time{}, fmt.Errorf("no quota reading for %q: %s", profile, st.Error)
		}
		return "", time.Time{}, fmt.Errorf("no quota reading for %q yet", profile)
	}
	now := s.now()
	var picked *Window
	if want := NormalizeWindow(window); want != "" {
		for i := range st.Windows {
			if st.Windows[i].Name == want {
				picked = &st.Windows[i]
				break
			}
		}
		if picked == nil {
			names := make([]string, len(st.Windows))
			for i, w := range st.Windows {
				names[i] = w.Name
			}
			return "", time.Time{}, fmt.Errorf("provider %q has no %q quota window (available: %s)", profile, want, strings.Join(names, ", "))
		}
	} else {
		// No window named: if any window is exhausted (>= 100 %), wait for the
		// binding exhausted window (latest reset, or the exhausted one with no
		// reset so it is refused rather than silently falling back). Otherwise
		// pick the window whose reset comes soonest.
		var exhausted []Window
		for _, w := range st.Windows {
			if w.UsedPct >= 100 {
				exhausted = append(exhausted, w)
			}
		}
		if len(exhausted) > 0 {
			best := exhausted[0]
			for _, w := range exhausted[1:] {
				if w.ResetsAt == nil || !w.ResetsAt.After(now) {
					best = w
					break
				}
				if best.ResetsAt != nil && best.ResetsAt.After(now) && w.ResetsAt.After(*best.ResetsAt) {
					best = w
				}
			}
			picked = &best
		} else {
			best := st.Windows[0]
			for _, w := range st.Windows[1:] {
				if w.ResetsAt == nil || !w.ResetsAt.After(now) {
					continue
				}
				if best.ResetsAt == nil || !best.ResetsAt.After(now) || w.ResetsAt.Before(*best.ResetsAt) {
					best = w
				}
			}
			picked = &best
		}
	}
	if picked.ResetsAt == nil {
		if st.Source == SourceSession {
			return "", time.Time{}, fmt.Errorf("the %s window of %q has no reset time (token account read only from sessions)", picked.Name, profile)
		}
		return "", time.Time{}, fmt.Errorf("the %s window of %q has no known reset time", picked.Name, profile)
	}
	if !picked.ResetsAt.After(now) {
		if st.Source == SourceSession {
			return "", time.Time{}, fmt.Errorf("the %s window of %q has no future reset time (last session reading is from before its reset)", picked.Name, profile)
		}
		return "", time.Time{}, fmt.Errorf("the %s window of %q has no future reset time", picked.Name, profile)
	}
	return picked.Name, picked.ResetsAt.UTC(), nil
}

var windowNames = map[string]string{"five_hour": "5 h", "seven_day": "7 d"}

func sortWindows(ws []Window) {
	rank := func(n string) int {
		if n == "5 h" {
			return 0
		}
		return 1
	}
	for i := 1; i < len(ws); i++ {
		for j := i; j > 0 && rank(ws[j].Name) < rank(ws[j-1].Name); j-- {
			ws[j], ws[j-1] = ws[j-1], ws[j]
		}
	}
}

// streamWindows reads the CLI's rate_limit_info: utilization there is a
// fraction (0–1) and resetsAt is in Unix seconds.
func streamWindows(info map[string]any) []Window {
	var out []Window
	if uw, ok := info["unifiedWindows"].(map[string]any); ok {
		for _, key := range []string{"five_hour", "seven_day"} {
			w, ok := uw[key].(map[string]any)
			if !ok {
				continue
			}
			u, ok := w["utilization"].(float64)
			if !ok {
				continue
			}
			out = append(out, Window{Name: windowNames[key], UsedPct: pct(u * 100), ResetsAt: unix(w["resetsAt"])})
		}
		if len(out) > 0 {
			return out
		}
	}
	typ, _ := info["rateLimitType"].(string)
	u, ok := info["utilization"].(float64)
	if name := windowNames[typ]; name != "" && ok {
		out = append(out, Window{Name: name, UsedPct: pct(u * 100), ResetsAt: unix(info["resetsAt"])})
	}
	return out
}

func pct(v float64) float64 { return math.Round(math.Max(0, v)*10) / 10 }

func unix(v any) *time.Time {
	f, ok := v.(float64)
	if !ok || f <= 0 {
		return nil
	}
	t := time.Unix(int64(f), 0).UTC()
	return &t
}

func (s *Service) fetch(ctx context.Context, t target) result {
	switch t.spec.Kind {
	case KindAnthropic:
		return s.fetchAnthropic(ctx, t)
	case KindDeepSeek:
		return s.fetchDeepSeek(ctx, t)
	case KindOpenRouter:
		return s.fetchOpenRouter(ctx, t)
	}
	return result{err: &fail{class: errOther, msg: "Unknown quota kind"}}
}

func (s *Service) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return http.DefaultClient
}

// get sends one authorised GET and decodes a 200 JSON body into v. Errors
// name the status, never the request.
func (s *Service) get(ctx context.Context, url, token string, hdr map[string]string, v any, authMsg string) *fail {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return &fail{class: errOther, msg: "Bad quota URL"}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	for k, x := range hdr {
		req.Header.Set(k, x)
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return &fail{class: errTransient, msg: "Could not reach the provider"}
	}
	defer resp.Body.Close()
	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		_ = json.Unmarshal(data, &body)
		f := &fail{errOther, fmt.Sprintf("Quota request failed (HTTP %d)", resp.StatusCode), refusal(resp.StatusCode, body.Error.Type, body.Error.Message, token)}
		switch {
		case resp.StatusCode == http.StatusTooManyRequests || body.Error.Type == "rate_limit_error":
			f.class, f.msg = errLimited, "Rate limited by the provider; trying again later"
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			f.msg = authMsg
		case resp.StatusCode >= 500:
			f.class, f.msg = errTransient, fmt.Sprintf("The provider failed (HTTP %d)", resp.StatusCode)
		}
		return f
	}
	if json.Unmarshal(data, &body) == nil && body.Error.Type == "rate_limit_error" {
		return &fail{errLimited, "Rate limited by the provider; trying again later", refusal(resp.StatusCode, body.Error.Type, body.Error.Message, token)}
	}
	if !json.Valid(data) {
		return &fail{class: errTransient, msg: "The provider sent an unreadable reply"}
	}
	if err := json.Unmarshal(data, v); err != nil {
		return &fail{class: errOther, msg: "The provider sent an unexpected reply"}
	}
	return nil
}

const (
	loginExpired = "Login expired; run this provider once"
	noToken      = "No token yet; paste it in Settings → Providers"
)

// secretLike is any Anthropic key or token shape, taken out of logged text.
var secretLike = regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]+`)

// refusal is a refused read for the log (V0.122): the status, the reply's
// error type and message, cut short, with the token and anything shaped like
// one taken out.
func refusal(status int, typ, msg, token string) string {
	if token != "" {
		msg = strings.ReplaceAll(msg, token, "[token]")
	}
	msg = secretLike.ReplaceAllString(msg, "[token]")
	msg = strings.Join(strings.Fields(msg), " ")
	if r := []rune(msg); len(r) > 200 {
		msg = string(r[:200]) + "…"
	}
	out := fmt.Sprintf("HTTP %d", status)
	if typ != "" {
		out += " " + typ
	}
	if msg != "" {
		out += ": " + msg
	}
	return out
}

func hasToken(t target) bool {
	if t.key == nil {
		return false
	}
	k, err := t.key()
	return err == nil && strings.TrimSpace(k) != ""
}

func (s *Service) fetchAnthropic(ctx context.Context, t target) result {
	var cred struct {
		OAuth struct {
			AccessToken string  `json:"accessToken"`
			ExpiresAt   float64 `json:"expiresAt"` // Unix milliseconds
		} `json:"claudeAiOauth"`
	}
	if t.spec.Token {
		// A token account is never read (V0.122), and never with the folder's login (V0.50).
		return result{err: &fail{class: errOther, msg: noToken}}
	} else {
		b, err := claudecli.Credentials(t.spec.ConfigDir) // the file, or macOS's Keychain
		if err != nil {
			return result{err: &fail{class: errOther, msg: "No Claude login found; run this provider once"}}
		}
		if json.Unmarshal(b, &cred) != nil || cred.OAuth.AccessToken == "" {
			return result{err: &fail{class: errOther, msg: "No Claude login found; run this provider once"}}
		}
	}
	if ms := cred.OAuth.ExpiresAt; ms > 0 && s.now().After(time.UnixMilli(int64(ms))) {
		return result{err: &fail{class: errOther, msg: loginExpired}}
	}
	// The reply holds more than the windows (flags, numbers, other objects):
	// read it loosely and decode only the two windows (found live: a strict
	// decode failed and the 5 h window went missing).
	var u map[string]json.RawMessage
	if f := s.get(ctx, t.spec.BaseURL+"/api/oauth/usage", cred.OAuth.AccessToken,
		map[string]string{"anthropic-beta": "oauth-2025-04-20"}, &u, loginExpired); f != nil {
		return result{err: f}
	}
	var ws []Window
	for _, key := range []string{"five_hour", "seven_day"} {
		var w *struct {
			Utilization *float64 `json:"utilization"`
			ResetsAt    *string  `json:"resets_at"`
		}
		if raw, ok := u[key]; !ok || json.Unmarshal(raw, &w) != nil || w == nil || w.Utilization == nil {
			continue
		}
		x := Window{Name: windowNames[key], UsedPct: pct(*w.Utilization)}
		if w.ResetsAt != nil {
			if at, err := time.Parse(time.RFC3339Nano, *w.ResetsAt); err == nil {
				at = at.UTC()
				x.ResetsAt = &at
			}
		}
		ws = append(ws, x)
	}
	if len(ws) == 0 {
		return result{err: &fail{class: errOther, msg: "The provider sent no usage windows"}}
	}
	return result{windows: ws}
}

func (s *Service) key(t target) (string, *fail) {
	if t.key != nil {
		if k, err := t.key(); err == nil && strings.TrimSpace(k) != "" {
			return strings.TrimSpace(k), nil
		}
	}
	if t.spec.KeyFile != "" {
		b, err := os.ReadFile(t.spec.KeyFile)
		if err != nil {
			return "", &fail{class: errOther, msg: "Cannot read the API key file"}
		}
		if k := strings.TrimSpace(string(b)); k != "" {
			return k, nil
		}
		return "", &fail{class: errOther, msg: "The API key file is empty"}
	}
	if t.spec.KeyEnv != "" {
		if k := strings.TrimSpace(envOf(t.env, t.spec.KeyEnv)); k != "" {
			return k, nil
		}
		getenv := s.Getenv
		if getenv == nil {
			getenv = os.Getenv
		}
		if k := strings.TrimSpace(getenv(t.spec.KeyEnv)); k != "" {
			return k, nil
		}
	}
	return "", &fail{class: errOther, msg: "No API key: set keyFile or keyEnv"}
}

func (s *Service) fetchDeepSeek(ctx context.Context, t target) result {
	k, f := s.key(t)
	if f != nil {
		return result{err: f}
	}
	var r struct {
		Infos []struct {
			Currency string `json:"currency"`
			Total    string `json:"total_balance"`
		} `json:"balance_infos"`
	}
	if f := s.get(ctx, t.spec.BaseURL+"/user/balance", k, nil, &r, "The API key was rejected"); f != nil {
		return result{err: f}
	}
	for _, x := range r.Infos {
		var amt float64
		if _, err := fmt.Sscanf(x.Total, "%g", &amt); err == nil {
			return result{balance: &Balance{Amount: &amt, Currency: x.Currency}}
		}
	}
	return result{err: &fail{class: errOther, msg: "The provider sent no balance"}}
}

func (s *Service) fetchOpenRouter(ctx context.Context, t target) result {
	k, f := s.key(t)
	if f != nil {
		return result{err: f}
	}
	var r struct {
		Data *struct {
			Usage          *float64 `json:"usage"`
			LimitRemaining *float64 `json:"limit_remaining"`
		} `json:"data"`
	}
	if f := s.get(ctx, t.spec.BaseURL+"/key", k, nil, &r, "The API key was rejected"); f != nil {
		return result{err: f}
	}
	switch {
	case r.Data == nil:
	case r.Data.LimitRemaining != nil:
		return result{balance: &Balance{Amount: r.Data.LimitRemaining, Currency: "USD"}}
	case r.Data.Usage != nil:
		return result{balance: &Balance{Spent: r.Data.Usage, Currency: "USD"}}
	}
	return result{err: &fail{class: errOther, msg: "The provider sent no balance"}}
}

// saved is one identity's entry on disk (V0.122): its data and its wait.
type saved struct {
	St      Status    `json:"st"`
	Next    time.Time `json:"next"`
	Limited int       `json:"limited"`
}

// load reads StatePath into s.ent; called with s.mu held, before the targets.
func (s *Service) load() {
	b, err := os.ReadFile(s.StatePath)
	if err != nil {
		return
	}
	var m map[string]saved
	if json.Unmarshal(b, &m) != nil {
		s.logf("quota: ignoring an unreadable %s", s.StatePath)
		return
	}
	for id, x := range m {
		if s.ent[id] != nil || x.St.Kind == "" {
			continue
		}
		x.St.Profile = ""
		s.ent[id] = &entry{st: x.St, next: x.Next, limited: x.Limited}
	}
}

// save writes every entry to StatePath; called with s.mu held. A token or
// key is never in an entry.
func (s *Service) save() {
	if s.StatePath == "" {
		return
	}
	m := make(map[string]saved, len(s.ent))
	for id, e := range s.ent {
		m[id] = saved{St: e.st, Next: e.next, Limited: e.limited}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	if err := fsutil.WriteAtomic(s.StatePath, b, 0o600); err != nil {
		s.logf("quota: cannot save its state: %v", err)
	}
}
