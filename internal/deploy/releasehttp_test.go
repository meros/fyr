package deploy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/release"
)

// noGh empties PATH so that no gh (or anything else) can run.
func noGh(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
}

// V0.release-without-gh: a public release is listed, downloaded and
// verified over plain HTTPS with no gh on PATH and no token: the assets come
// from their download URLs, with no API call and no Authorization header.
func TestFetchWithoutGh(t *testing.T) {
	noGh(t)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	g := newFakeGitHub(t, relSHA, priv)
	r := g.releases()
	r.Key = base64.StdEncoding.EncodeToString(pub)
	r.Token = TokenFromEnv() // nothing in the env, no gh: none
	sha, err := r.Deployable(context.Background())
	if err != nil || sha != relSHA {
		t.Fatalf("deployable %q %v", sha, err)
	}
	info, err := r.fetch(context.Background(), sha, t.TempDir())
	if err != nil || info.Commit != relSHA {
		t.Fatalf("fetch %+v %v", info, err)
	}
	for _, q := range g.asked {
		if q.Header.Get("Authorization") != "" {
			t.Fatalf("%s sent a token", q.URL.Path)
		}
		if !strings.HasPrefix(q.URL.Path, "/meros/fyr/releases/download/"+release.Tag(relSHA)+"/") {
			t.Fatalf("not a download URL: %s", q.URL.Path)
		}
	}
	if len(g.asked) != 5 {
		t.Fatalf("requests %v", g.requests())
	}
}

// V0.release-without-gh: the key comes from the config; a release signed
// with it verifies, and the built-in key refuses it.
func TestFetchVerifiesWithTheConfiguredKey(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	g := newFakeGitHub(t, relSHA, priv)
	r := g.releases()
	if _, err := r.fetch(context.Background(), relSHA, t.TempDir()); err == nil || !strings.Contains(err.Error(), "did not verify") {
		t.Fatalf("the built-in key took a fork's release: %v", err)
	}
	if fp, builtIn, err := r.KeyInfo(); err != nil || !builtIn || !strings.HasPrefix(fp, "SHA256:") {
		t.Fatalf("built-in key info %q %v %v", fp, builtIn, err)
	}
	r.Key = base64.StdEncoding.EncodeToString(pub)
	if _, err := r.fetch(context.Background(), relSHA, t.TempDir()); err != nil {
		t.Fatalf("the configured key: %v", err)
	}
	want, _ := release.Fingerprint(r.Key)
	if fp, builtIn, err := r.KeyInfo(); err != nil || builtIn || fp != want {
		t.Fatalf("configured key info %q %v %v", fp, builtIn, err)
	}
	r.Key = "not a key"
	if _, _, err := r.KeyInfo(); err == nil {
		t.Fatal("a bad key has a fingerprint")
	}
	if _, err := r.fetch(context.Background(), relSHA, t.TempDir()); err == nil {
		t.Fatal("a bad key verified a release")
	}
}

// V0.release-without-gh: a 403 that says the limit is spent holds every API
// call until GitHub's reset, without a request; a 429's Retry-After too.
func TestRateLimitHoldsUntilReset(t *testing.T) {
	_, k, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Unix(1_800_000_000, 0)
	for _, c := range []struct {
		name   string
		code   int
		header map[string]string
		until  time.Time
	}{
		{"403 remaining 0", 403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": strconv.FormatInt(now.Add(20*time.Minute).Unix(), 10)}, now.Add(20 * time.Minute)},
		{"429 retry-after", 429, map[string]string{"Retry-After": "90"}, now.Add(90 * time.Second)},
		{"403 secondary retry-after", 403, map[string]string{"Retry-After": "60"}, now.Add(time.Minute)},
	} {
		g := newFakeGitHub(t, relSHA, k)
		limited := true
		g.handle = func(w http.ResponseWriter, q *http.Request) bool {
			if !limited {
				return false
			}
			for h, v := range c.header {
				w.Header().Set(h, v)
			}
			w.WriteHeader(c.code)
			fmt.Fprint(w, `{"message":"API rate limit exceeded"}`)
			return true
		}
		r := g.releases()
		r.Now = func() time.Time { return now }
		var e errRateLimited
		if ok := r.Contains(context.Background(), relSHA, oldSHA); ok {
			t.Fatalf("%s: contains while limited", c.name)
		}
		if _, err := r.compare(context.Background(), oldSHA, relSHA); !errors.As(err, &e) || !e.until.Equal(c.until) {
			t.Fatalf("%s: err %v", c.name, err)
		}
		if n := len(g.requests()); n != 1 {
			t.Fatalf("%s: %d requests while held, want 1", c.name, n)
		}
		limited = false
		r.Now = func() time.Time { return c.until.Add(time.Second) }
		if _, err := r.compare(context.Background(), oldSHA, relSHA); err != nil || len(g.requests()) != 2 {
			t.Fatalf("%s: after the reset: %v, %d requests", c.name, err, len(g.requests()))
		}
	}

	// A 403 that is not the limit (a private repo without a token) holds nothing.
	g := newFakeGitHub(t, relSHA, k)
	g.handle = func(w http.ResponseWriter, q *http.Request) bool {
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.WriteHeader(403)
		return true
	}
	r := g.releases()
	r.compare(context.Background(), oldSHA, relSHA)
	r.compare(context.Background(), oldSHA, relSHA)
	if len(g.requests()) != 2 {
		t.Fatalf("a plain 403 held the next call: %v", g.requests())
	}
}

// V0.release-without-gh: an answer with an ETag is asked for again with
// If-None-Match, and a 304 gives the kept answer.
func TestAPIAsksAgainWithETag(t *testing.T) {
	_, k, _ := ed25519.GenerateKey(rand.Reader)
	g := newFakeGitHub(t, relSHA, k)
	g.handle = func(w http.ResponseWriter, q *http.Request) bool {
		if !strings.Contains(q.URL.Path, "/compare/") {
			return false
		}
		if q.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return true
		}
		w.Header().Set("ETag", `"v1"`)
		fmt.Fprint(w, `{"status":"ahead","ahead_by":4}`)
		return true
	}
	r := g.releases()
	for i := 0; i < 2; i++ {
		if n, ok := r.Behind(context.Background(), oldSHA, relSHA); !ok || n != 4 {
			t.Fatalf("read %d: behind %d %v", i, n, ok)
		}
	}
	if g.asked[0].Header.Get("If-None-Match") != "" || g.asked[1].Header.Get("If-None-Match") != `"v1"` {
		t.Fatalf("conditional headers %q %q", g.asked[0].Header.Get("If-None-Match"), g.asked[1].Header.Get("If-None-Match"))
	}
}

// V0.release-without-gh: a token, when there is one, goes to the API only;
// a private repo's assets (404 on the download URL) come from the API's
// asset endpoint with it. Without a token that is an error.
func TestTokenGoesToTheAPIOnly(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	g := newFakeGitHub(t, relSHA, priv)
	tag := release.Tag(relSHA)
	g.handle = func(w http.ResponseWriter, q *http.Request) bool {
		switch {
		case strings.Contains(q.URL.Path, "/releases/download/"):
			http.NotFound(w, q) // private: no anonymous download
		case q.URL.Path == "/repos/meros/fyr/releases/tags/"+tag:
			var as []string
			for n := range g.files {
				as = append(as, fmt.Sprintf(`{"name":%q,"url":%q}`, n, g.srv.URL+"/repos/meros/fyr/releases/assets/"+n))
			}
			fmt.Fprintf(w, `{"tag_name":%q,"assets":[%s]}`, tag, strings.Join(as, ","))
		case strings.HasPrefix(q.URL.Path, "/repos/meros/fyr/releases/assets/"):
			if q.Header.Get("Authorization") != "Bearer tok" || q.Header.Get("Accept") != "application/octet-stream" {
				w.WriteHeader(401)
				return true
			}
			w.Write(g.files[strings.TrimPrefix(q.URL.Path, "/repos/meros/fyr/releases/assets/")])
		default:
			return false
		}
		return true
	}
	r := g.releases()
	r.Key = base64.StdEncoding.EncodeToString(pub)
	if _, err := r.fetch(context.Background(), relSHA, t.TempDir()); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("a private release without a token: %v", err)
	}
	r.Token = func() string { return "tok" }
	if _, err := r.fetch(context.Background(), relSHA, t.TempDir()); err != nil {
		t.Fatalf("with a token: %v", err)
	}
	api := 0
	for _, q := range g.asked {
		if strings.Contains(q.URL.Path, "/releases/download/") && q.Header.Get("Authorization") != "" {
			t.Fatalf("the token went to the download URL %s", q.URL.Path)
		}
		if q.URL.Path == "/repos/meros/fyr/releases/tags/"+tag {
			api++
		}
	}
	if api != 1 {
		t.Fatalf("the release was listed %d times, want once", api)
	}
}

// V0.release-without-gh: the token comes from GH_TOKEN or GITHUB_TOKEN;
// with neither and no gh, there is none.
func TestTokenFromEnv(t *testing.T) {
	noGh(t)
	if tok := TokenFromEnv()(); tok != "" {
		t.Fatalf("token %q with no env and no gh", tok)
	}
	t.Setenv("GITHUB_TOKEN", "b")
	if tok := TokenFromEnv()(); tok != "b" {
		t.Fatalf("token %q", tok)
	}
	t.Setenv("GH_TOKEN", "a")
	if tok := TokenFromEnv()(); tok != "a" {
		t.Fatalf("token %q", tok)
	}
}
