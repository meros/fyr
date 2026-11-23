package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/httpguard"
	"github.com/meros/fyr/internal/proc"
)

// V0.release-without-gh: the updater reads a release repo over plain HTTPS,
// with no gh. A public repo needs no login: the API answers anonymously and
// a release's assets download from github.com. A token, when one is at hand,
// is sent to the API only (it raises the limit and reads a private fork).

const (
	defaultAPI = "https://api.github.com"
	defaultWeb = "https://github.com"
	// apiBodyMax bounds one API answer (a compare lists up to 250 commits
	// and 300 files).
	apiBodyMax = 32 << 20
	// etagKeep is how many API answers are kept for a conditional request.
	etagKeep = 64
)

// errRateLimited is an API call refused, or not made, because GitHub's
// limit is spent until a time.
type errRateLimited struct{ until time.Time }

func (e errRateLimited) Error() string {
	return "GitHub's API limit is spent until " + e.until.Local().Format("15:04:05")
}

const apiHoldKey = "deploy.apiHold"

// apiState is what a Releases keeps between calls: the answers it may ask
// for again with If-None-Match, and the time GitHub said to wait until.
type apiState struct {
	mu     sync.Mutex
	loaded bool
	etags  map[string]etagged
	order  []string
	hold   time.Time
}

func (r *Releases) loadHoldLocked() {
	st := &r.state
	if st.loaded {
		return
	}
	st.loaded = true
	if r.KV == nil {
		return
	}
	raw, _ := r.KV.GetKV(apiHoldKey)
	if raw == "" {
		return
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil && r.now().Before(t) {
		st.hold = t
	} else {
		r.KV.SetKV(apiHoldKey, "") // passed or unreadable
	}
}

func (r *Releases) saveHoldLocked() {
	if r.KV == nil {
		return
	}
	if r.now().Before(r.state.hold) {
		r.KV.SetKV(apiHoldKey, r.state.hold.UTC().Format(time.RFC3339Nano))
	} else {
		r.KV.SetKV(apiHoldKey, "")
	}
}

type etagged struct {
	tag  string
	body []byte
}

func (r *Releases) client() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	return httpguard.NewClient()
}

func (r *Releases) token() string {
	if r.Token == nil {
		return ""
	}
	return strings.TrimSpace(r.Token())
}

func (r *Releases) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Releases) apiBase() string {
	if r.API != "" {
		return strings.TrimRight(r.API, "/")
	}
	return defaultAPI
}

func (r *Releases) webBase() string {
	if r.Web != "" {
		return strings.TrimRight(r.Web, "/")
	}
	return defaultWeb
}

// api GETs path (from "repos/...") from GitHub's REST API and returns the
// body of a 200. An answer it has is asked for again with its ETag, and a
// 304 returns it. A 403 or 429 that says the limit is spent holds every
// call until GitHub's reset (or its Retry-After), without a request.
func (r *Releases) api(ctx context.Context, path string) ([]byte, error) {
	u := r.apiBase() + "/" + strings.TrimLeft(path, "/")
	st := &r.state
	st.mu.Lock()
	r.loadHoldLocked()
	if hold := st.hold; r.now().Before(hold) {
		st.mu.Unlock()
		return nil, errRateLimited{hold}
	} else if !hold.IsZero() {
		st.hold = time.Time{}
		r.saveHoldLocked()
	}
	cached, have := st.etags[u]
	st.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "fyrd")
	if t := r.token(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	if have {
		req.Header.Set("If-None-Match", cached.tag)
	}
	resp, err := r.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, apiBodyMax))
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusNotModified && have:
		return cached.body, nil
	case resp.StatusCode == http.StatusOK:
		if tag := resp.Header.Get("ETag"); tag != "" {
			r.keep(u, etagged{tag, body})
		}
		return body, nil
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		if until, ok := r.limitedUntil(resp); ok {
			st.mu.Lock()
			if until.After(st.hold) {
				st.hold = until
				r.saveHoldLocked()
			}
			st.mu.Unlock()
			return nil, errRateLimited{until}
		}
	}
	return nil, fmt.Errorf("GET %s: %s %s", path, resp.Status, tail(body, 300))
}

// limitedUntil reads whether a 403 or 429 is GitHub's limit and until when:
// Retry-After first, then x-ratelimit-reset once remaining is 0, else a
// minute for a secondary limit that names no time.
func (r *Releases) limitedUntil(resp *http.Response) (time.Time, bool) {
	now := r.now()
	if s, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && s >= 0 {
		return now.Add(time.Duration(s) * time.Second), true
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		if s, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			if t := time.Unix(s, 0); t.After(now) {
				return t, true
			}
		}
		return now.Add(time.Minute), true
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return now.Add(time.Minute), true
	}
	return time.Time{}, false
}

func (r *Releases) keep(u string, e etagged) {
	st := &r.state
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.etags == nil {
		st.etags = map[string]etagged{}
	}
	if _, ok := st.etags[u]; !ok {
		st.order = append(st.order, u)
	}
	st.etags[u] = e
	for len(st.order) > etagKeep {
		delete(st.etags, st.order[0])
		st.order = st.order[1:]
	}
}

// download saves the asset name of the release tag into dir. It reads the
// public download URL on github.com (no API call, no login); a private repo
// answers 404 there, and with a token the API's asset endpoint is asked.
func (r *Releases) download(ctx context.Context, tag string, names []string, dir string) error {
	var assets map[string]string // name → API URL, read once if needed
	for _, n := range names {
		u := r.webBase() + "/" + r.repo() + "/releases/download/" + tag + "/" + n
		code, err := r.save(ctx, u, "", filepath.Join(dir, n))
		if err == nil {
			continue
		}
		if code != http.StatusNotFound || r.token() == "" {
			return fmt.Errorf("%s: %w", n, err)
		}
		if assets == nil {
			b, aerr := r.api(ctx, "repos/"+r.repo()+"/releases/tags/"+tag)
			if aerr != nil {
				return fmt.Errorf("%s: %v; by the API: %w", n, err, aerr)
			}
			var rel struct {
				Assets []struct {
					Name string `json:"name"`
					URL  string `json:"url"`
				} `json:"assets"`
			}
			if aerr := json.Unmarshal(b, &rel); aerr != nil {
				return fmt.Errorf("release %s: %w", tag, aerr)
			}
			assets = map[string]string{}
			for _, a := range rel.Assets {
				assets[a.Name] = a.URL
			}
		}
		au, ok := assets[n]
		if !ok {
			return fmt.Errorf("release %s has no asset %s", tag, n)
		}
		if _, err := r.save(ctx, au, r.token(), filepath.Join(dir, n)); err != nil {
			return fmt.Errorf("%s: %w", n, err)
		}
	}
	return nil
}

// save GETs u into the file to; a token makes it an API asset download.
func (r *Releases) save(ctx context.Context, u, token, to string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "fyrd")
	if token != "" {
		// Go drops the header on the redirect to the storage host.
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/octet-stream")
	}
	resp, err := r.client().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return resp.StatusCode, fmt.Errorf("%s %s", resp.Status, tail(b, 200))
	}
	f, err := os.Create(to)
	if err != nil {
		return resp.StatusCode, err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return resp.StatusCode, err
	}
	return resp.StatusCode, f.Close()
}

// TokenFromEnv is the token the updater may send to GitHub's API: GH_TOKEN
// or GITHUB_TOKEN (github.env), else gh's stored login when gh is on PATH
// (`gh auth token` reads it locally, no API call), else none: a public
// release repo is read anonymously. gh's answer is kept for 10 minutes.
func TokenFromEnv() func() string {
	var mu sync.Mutex
	var tok string
	var at time.Time
	return func() string {
		for _, k := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
			if v := strings.TrimSpace(os.Getenv(k)); v != "" {
				return v
			}
		}
		mu.Lock()
		defer mu.Unlock()
		if !at.IsZero() && time.Since(at) < 10*time.Minute {
			return tok
		}
		at, tok = time.Now(), ""
		if _, err := exec.LookPath("gh"); err != nil {
			return ""
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if out, err := proc.Command(ctx, "gh", "auth", "token").Output(); err == nil {
			tok = strings.TrimSpace(string(out))
		}
		return tok
	}
}
