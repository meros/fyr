package deploy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/release"
)

const (
	v100SHA = "1000000000000000000000000000000000000000"
	v110SHA = "1100000000000000000000000000000000000000"
	rcSHA   = "1200000000000000000000000000000000000000"
)

// signedRelease is the five files of a release of info, signed with key.
func signedRelease(t *testing.T, info release.Info, key ed25519.PrivateKey) map[string][]byte {
	t.Helper()
	tgt := release.Here()
	dir := t.TempDir()
	b, _ := json.Marshal(info)
	os.WriteFile(filepath.Join(dir, release.Manifest), b, 0o644)
	os.WriteFile(filepath.Join(dir, release.Asset("fyrd", tgt)), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(dir, release.Asset("fyr", tgt)), []byte("#!/bin/sh\n"), 0o755)
	sums, _ := release.SumFile(dir, []string{release.Asset("fyr", tgt), release.Asset("fyrd", tgt), release.Manifest})
	os.WriteFile(filepath.Join(dir, release.Sums), sums, 0o644)
	sig, _ := release.Sign(base64.StdEncoding.EncodeToString(key.Seed()), sums)
	os.WriteFile(filepath.Join(dir, release.Sig), []byte(sig), 0o644)
	files := map[string][]byte{}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		files[e.Name()], _ = os.ReadFile(filepath.Join(dir, e.Name()))
	}
	return files
}

// versionedGitHub is the fake GitHub with `deployable` at relSHA (its build
// release), and version releases: v1.0.0, v1.1.0, the prerelease
// v1.2.0-rc.1, a draft v9.0.0 and build releases in the list. The tags are
// read by git ls-remote from it.
func versionedGitHub(t *testing.T) (*fakeGitHub, *Releases) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	g := newFakeGitHub(t, relSHA, priv)
	tags := map[string]string{"deployable": relSHA, "v1.0.0": v100SHA, "v1.1.0": v110SHA, "v1.2.0-rc.1": rcSHA, "v9.0.0": relSHA}
	rels := map[string]map[string][]byte{}
	for tag, sha := range tags {
		if release.IsVersion(tag) {
			rels[tag] = signedRelease(t, release.Info{Commit: sha, Build: release.Short(sha), Version: tag}, priv)
		}
	}
	g.handle = func(w http.ResponseWriter, q *http.Request) bool {
		if q.URL.Path == "/repos/meros/fyr/releases" {
			// Newest first, as GitHub lists them: a build release, the
			// prerelease (by its flag and its name), a draft, the versions.
			fmt.Fprint(w, `[
				{"tag_name":"build-`+release.Short(relSHA)+`","draft":false,"prerelease":false},
				{"tag_name":"v9.0.0","draft":true,"prerelease":false},
				{"tag_name":"v1.2.0-rc.1","draft":false,"prerelease":true},
				{"tag_name":"v1.1.0","draft":false,"prerelease":false},
				{"tag_name":"v1.0.0","draft":false,"prerelease":false},
				{"tag_name":"not-a-version","draft":false,"prerelease":false}]`)
			return true
		}
		for tag, files := range rels {
			if name, ok := strings.CutPrefix(q.URL.Path, "/meros/fyr/releases/download/"+tag+"/"); ok && files[name] != nil {
				w.Write(files[name])
				return true
			}
		}
		return false
	}
	r := g.releases()
	r.Key = base64.StdEncoding.EncodeToString(pub)
	r.Git = func(_ context.Context, args ...string) ([]byte, error) {
		ref := args[len(args)-1]
		tag := strings.TrimPrefix(ref, "refs/tags/")
		if sha, ok := tags[tag]; ok {
			return []byte(sha + "\t" + ref + "\n"), nil
		}
		return nil, fmt.Errorf("no %s", ref)
	}
	return g, r
}

// V0.versioned-releases: stable follows the newest version release that is
// not a prerelease and not a draft, passes build releases over, and fetches
// that version's release (its manifest names the version).
func TestChannelStablePicksNewestVersion(t *testing.T) {
	_, r := versionedGitHub(t)
	r.Channel = "stable"
	sha, err := r.Deployable(context.Background())
	if err != nil || sha != v110SHA {
		t.Fatalf("stable: %q %v, want v1.1.0's commit", sha, err)
	}
	if v := r.VersionOf(sha); v != "v1.1.0" {
		t.Fatalf("version of the stable commit: %q", v)
	}
	info, err := r.fetch(context.Background(), sha, t.TempDir())
	if err != nil || info.Version != "v1.1.0" || info.Commit != v110SHA {
		t.Fatalf("fetch: %+v %v", info, err)
	}
}

// V0.versioned-releases: main (and a config without the key) is unchanged:
// `deployable` and its build release.
func TestChannelMainUnchanged(t *testing.T) {
	for _, ch := range []string{"", "main", "nonsense"} {
		g, r := versionedGitHub(t)
		r.Channel = ch
		sha, err := r.Deployable(context.Background())
		if err != nil || sha != relSHA {
			t.Fatalf("%q: %q %v", ch, sha, err)
		}
		if _, err := r.fetch(context.Background(), sha, t.TempDir()); err != nil {
			t.Fatalf("%q: fetch %v", ch, err)
		}
		for _, q := range g.requests() {
			if strings.Contains(q, "/releases/download/") && !strings.Contains(q, "/download/"+release.Tag(relSHA)+"/") || strings.HasSuffix(q, "/repos/meros/fyr/releases") {
				t.Fatalf("%q: main read %s", ch, q)
			}
		}
		if r.ChannelName() != ChannelMain {
			t.Fatalf("%q is channel %q", ch, r.ChannelName())
		}
	}
}

// V0.versioned-releases: a pinned version stays on that version, a
// prerelease too; no list of releases is read.
func TestChannelPinned(t *testing.T) {
	for _, c := range []struct{ tag, sha string }{{"v1.0.0", v100SHA}, {"v1.2.0-rc.1", rcSHA}} {
		g, r := versionedGitHub(t)
		r.Channel = c.tag
		sha, err := r.Deployable(context.Background())
		if err != nil || sha != c.sha {
			t.Fatalf("%s: %q %v", c.tag, sha, err)
		}
		info, err := r.fetch(context.Background(), sha, t.TempDir())
		if err != nil || info.Version != c.tag {
			t.Fatalf("%s: fetch %+v %v", c.tag, info, err)
		}
		for _, q := range g.requests() {
			if strings.HasSuffix(q, "/repos/meros/fyr/releases") {
				t.Fatalf("%s: a pinned host listed the releases", c.tag)
			}
		}
	}
}

// V0.versioned-releases: a release whose signed manifest names another
// version is refused, as one of another commit is.
func TestChannelRefusesAMislabelledVersion(t *testing.T) {
	g, r := versionedGitHub(t)
	next := g.handle
	pub, k, _ := ed25519.GenerateKey(rand.Reader)
	r.Key = base64.StdEncoding.EncodeToString(pub)
	wrong := signedRelease(t, release.Info{Commit: v100SHA, Build: release.Short(v100SHA), Version: "v1.1.0"}, k)
	g.handle = func(w http.ResponseWriter, q *http.Request) bool {
		if name, ok := strings.CutPrefix(q.URL.Path, "/meros/fyr/releases/download/v1.0.0/"); ok {
			w.Write(wrong[name])
			return true
		}
		return next(w, q)
	}
	r.Channel = "v1.0.0"
	sha, err := r.Deployable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.fetch(context.Background(), sha, t.TempDir()); err == nil || !strings.Contains(err.Error(), `says it is version "v1.1.0"`) {
		t.Fatalf("a mislabelled version: %v", err)
	}
}

// V0.versioned-releases: stable with no version release yet says so.
func TestChannelStableWithoutVersions(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	g := newFakeGitHub(t, relSHA, priv)
	g.handle = func(w http.ResponseWriter, q *http.Request) bool {
		if q.URL.Path == "/repos/meros/fyr/releases" {
			fmt.Fprint(w, `[{"tag_name":"build-0123456789ab","draft":false,"prerelease":false},{"tag_name":"v2.0.0-rc.1","draft":false,"prerelease":false}]`)
			return true
		}
		return false
	}
	r := g.releases()
	r.Channel = "stable"
	if _, err := r.Deployable(context.Background()); err == nil || !strings.Contains(err.Error(), "no stable release") {
		t.Fatalf("stable without versions: %v", err)
	}
}

func TestParseChannel(t *testing.T) {
	for in, want := range map[string]string{"": "main", "main": "main", " stable ": "stable", "v1.2.3": "v1.2.3", "v1.0.0-rc.1": "v1.0.0-rc.1"} {
		if got, err := ParseChannel(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"beta", "1.2.3", "v1.2", "v01.2.3", "build-0123456789ab"} {
		if _, err := ParseChannel(bad); err == nil {
			t.Errorf("%q passed", bad)
		}
	}
}

// V0.versioned-releases: an Update now on the stable channel installs the
// newest version's release (downloaded from its version tag), and the
// summary the fronts show names the channel and the running version.
func TestStableInstallAndSummary(t *testing.T) {
	d, _, steps := newDeployer(t, "")
	g, r := versionedGitHub(t)
	r.Channel = "stable"
	d.Releases = r
	d.LiveVersion = "v1.0.0"
	d.Run = func(_ context.Context, dir string, args ...string) ([]byte, error) {
		*steps = append(*steps, strings.Join(args, " "))
		if len(args) == 2 && args[1] == "version" {
			return []byte(release.Short(v110SHA) + "\n"), nil
		}
		return []byte("ok"), nil
	}
	req := core.EffectReq{Action: "fyr.deploy", Args: map[string]any{}}
	if _, err := d.Deploy(context.Background(), "t1", req, ""); !errors.Is(err, core.ErrReportLater) {
		t.Fatalf("err %v", err)
	}
	if target(d.Data, "next") != release.Short(v110SHA) {
		t.Fatalf("next=%s", target(d.Data, "next"))
	}
	dl := false
	for _, q := range g.requests() {
		if strings.Contains(q, "/releases/download/") {
			dl = true
			if !strings.Contains(q, "/releases/download/v1.1.0/") {
				t.Fatalf("stable downloaded %s", q)
			}
		}
	}
	if !dl {
		t.Fatalf("nothing downloaded: %v", g.requests())
	}
	if s := d.Summary(); s.Channel != "stable" || s.Version != "v1.0.0" {
		t.Fatalf("summary %+v", s)
	}
}
