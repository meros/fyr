package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/meros/fyr/internal/fsutil"
	"github.com/meros/fyr/internal/kvstore"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/proc"
	"github.com/meros/fyr/internal/release"
)

// Decision 0027 (#149, #150, #153): auto-update installs only what CI
// published for a green commit on main: a signed release, downloaded,
// verified, smoke-tested and switched to like any build. A local build of a
// checkout (Deploy with a dir, #159) stays for working on Fyr from Fyr; it
// needs the owner's press and pauses auto-update.

// Releases reads Fyr's releases on GitHub over HTTPS (V0.release-without-gh):
// the tag with git ls-remote, the compares and the release lookups from the
// REST API, the assets from their download URLs. No gh, no login for a
// public repo.
type Releases struct {
	Repo string // owner/name, default meros/fyr
	// Git runs git (tests); nil is the git CLI (V0.173).
	Git func(ctx context.Context, args ...string) ([]byte, error)
	// Key verifies a release; empty is release.PublicKey.
	Key string
	// API and Web are GitHub's REST API and web bases (tests); empty is
	// api.github.com and github.com. HTTP nil is http.DefaultClient.
	API, Web string
	HTTP     *http.Client
	// Token, when it returns one, is sent to the API (a higher limit, a
	// private fork); nil or "" reads anonymously.
	Token func() string
	// KV keeps the GitHub API rate-limit hold across restarts
	// (V0.persist-runtime-state); nil keeps it in memory only.
	KV    kvstore.Table
	Now   func() time.Time // tests
	state apiState
	// Channel is the config's updateChannel (V0.versioned-releases): "" or
	// "main", "stable", or a version to pin to.
	Channel string
	mu      sync.Mutex
	tags    map[string]string // commit → the version tag the channel found it under
}

// Name is owner/name of the repo the releases come from (V0.update-changelog: the changelog links to it).
func (r *Releases) Name() string { return r.repo() }

func (r *Releases) key() string {
	if strings.TrimSpace(r.Key) == "" {
		return release.PublicKey
	}
	return r.Key
}

// KeyInfo names the key releases are verified with (V0.release-without-gh):
// its fingerprint, whether it is the built-in one, and an error when the
// configured key is not an ed25519 public key.
func (r *Releases) KeyInfo() (fingerprint string, builtIn bool, err error) {
	builtIn = strings.TrimSpace(r.Key) == "" || strings.TrimSpace(r.Key) == release.PublicKey
	fingerprint, err = release.Fingerprint(r.key())
	return fingerprint, builtIn, err
}

func (r *Releases) repo() string {
	if r.Repo == "" {
		return "meros/fyr"
	}
	return r.Repo
}

// Deployable is the commit the host's channel points at
// (V0.versioned-releases): on main, the tag `deployable` (CI's newest green,
// published commit); on stable, the newest version release's commit; pinned,
// that version's.
func (r *Releases) Deployable(ctx context.Context) (string, error) {
	sha, _, err := r.wanted(ctx)
	return sha, err
}

// mainHead is the commit the tag `deployable` points at. V0.173: read with
// git ls-remote, which costs no GitHub API call; the API only when git
// cannot say.
func (r *Releases) mainHead(ctx context.Context) (string, error) {
	sha, err := r.lsRemote(ctx, release.Deployable)
	if err == nil {
		return sha, nil
	}
	log.Printf("deployable by git: %v; asking the API", err)
	return r.tagCommit(ctx, release.Deployable)
}

func isSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// lsRemote reads the tag with git ls-remote; an annotated tag's commit is
// its peeled (^{}) line.
func (r *Releases) lsRemote(ctx context.Context, tag string) (string, error) {
	git := r.Git
	if git == nil {
		git = func(ctx context.Context, args ...string) ([]byte, error) {
			c, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			return proc.Git(c, "", args...).Output()
		}
	}
	ref := "refs/tags/" + tag
	// No credential helper (V0.release-without-gh): a public repo needs
	// none, and a private one fails here and is read by the API with the
	// token.
	out, err := git(ctx, "-c", "credential.helper=", "ls-remote", "--tags", r.webBase()+"/"+r.repo()+".git", ref)
	if err != nil {
		return "", err
	}
	sha := ""
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) != 2 || !isSHA(f[0]) {
			continue
		}
		switch f[1] {
		case ref + "^{}":
			return f[0], nil
		case ref:
			sha = f[0]
		}
	}
	if sha == "" {
		return "", fmt.Errorf("no %s in git ls-remote", ref)
	}
	return sha, nil
}

// Contains reports whether commit holds ancestor (or is it), by GitHub's
// compare; false when it cannot say.
func (r *Releases) Contains(ctx context.Context, commit, ancestor string) bool {
	if commit == "" || ancestor == "" || strings.HasPrefix(commit, "-") || strings.HasPrefix(ancestor, "-") {
		return false
	}
	if strings.HasPrefix(commit, ancestor) || strings.HasPrefix(ancestor, commit) {
		return true
	}
	c, err := r.compare(ctx, ancestor, commit)
	return err == nil && (c.Status == "ahead" || c.Status == "identical")
}

// comparison is the part of GitHub's compare the updater reads.
type comparison struct {
	Status       string `json:"status"`
	AheadBy      int    `json:"ahead_by"`
	TotalCommits int    `json:"total_commits"`
	Commits      []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	} `json:"commits"`
}

// compare is one GitHub compare of base...head.
func (r *Releases) compare(ctx context.Context, base, head string) (comparison, error) {
	var c comparison
	b, err := r.api(ctx, "repos/"+r.repo()+"/compare/"+url.PathEscape(base)+"..."+url.PathEscape(head))
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("compare %s...%s: %w", base, head, err)
	}
	return c, nil
}

// Changes lists the commits of to that from lacks (V0.update-changelog), by one GitHub compare: newest first, at most
// changesKeep, and the count of all. A compare lists at most 250 commits; total_commits counts them all.
func (r *Releases) Changes(ctx context.Context, from, to string) ([]Change, int, error) {
	if strings.HasPrefix(from, "-") || strings.HasPrefix(to, "-") {
		return nil, 0, errors.New("not a commit")
	}
	c, err := r.compare(ctx, from, to)
	if err != nil {
		return nil, 0, err
	}
	var lines strings.Builder
	for _, x := range c.Commits {
		title, _, _ := strings.Cut(x.Commit.Message, "\n")
		lines.WriteString(x.SHA + "\t" + strings.TrimSpace(title) + "\n")
	}
	cs, n := parseChanges(lines.String(), true)
	total := c.TotalCommits
	if total < n {
		total = n
	}
	return cs, total, nil
}

// fetch downloads the release of sha for this host into dir (a fresh
// folder), verifies it and lays it out as a build: fyrd, fyr,
// manifest.json, server.sum.
func (r *Releases) fetch(ctx context.Context, sha, dir string) (release.Info, error) {
	t := release.Here()
	fyrd, fyr := release.Asset("fyrd", t), release.Asset("fyr", t)
	names := []string{fyrd, fyr, release.Manifest, release.Sums, release.Sig}
	tag := r.releaseTag(sha) // V0.versioned-releases: a version's release on stable or pinned
	if err := r.download(ctx, tag, names, dir); err != nil {
		return release.Info{}, fmt.Errorf("downloading %s from %s: %w", tag, r.repo(), err)
	}
	info, err := release.Verify(dir, r.key(), fyrd, fyr)
	if err != nil {
		return info, fmt.Errorf("release %s did not verify: %w", tag, err)
	}
	if info.Commit != sha {
		return info, fmt.Errorf("release %s is for commit %s, not %s", tag, release.Short(info.Commit), release.Short(sha))
	}
	if release.IsVersion(tag) && info.Version != tag {
		// The signed manifest names the version, so a release cannot be
		// passed off as another version of the same commit.
		return info, fmt.Errorf("release %s says it is version %q", tag, info.Version)
	}
	for from, to := range map[string]string{fyrd: "fyrd", fyr: "fyr"} {
		if err := os.Rename(filepath.Join(dir, from), filepath.Join(dir, to)); err != nil {
			return info, err
		}
		if err := os.Chmod(filepath.Join(dir, to), 0o755); err != nil {
			return info, err
		}
	}
	if info.ServerSum != "" {
		if err := fsutil.WriteAtomic(filepath.Join(dir, sumFile), []byte(info.ServerSum+"\n"), 0o644); err != nil {
			return info, err
		}
	}
	return info, nil
}

// releaseDir is the build folder of a release: builds/<sha12>.
func (d *Deployer) releaseDir(sha string) string {
	return filepath.Join(d.Data, "builds", release.Short(sha))
}

// verifiedMark marks a release build that downloaded and verified whole.
const verifiedMark = ".verified"

// install is the release path of Deploy: no checkout, no local build, no
// make test. It downloads (or reuses) the release of sha, smoke-tests it and
// switches to it with the same snapshot, drain and link as a local build.
func (d *Deployer) install(ctx context.Context, run Runner, prog *Progress, gen int, threadID string, req core.EffectReq, sha string, fail func(error) (string, error), failed func(error) (string, error)) (string, error) {
	if d.Releases == nil {
		return failed(errors.New("this Fyr has no release source; ask with a checkout (args.dir) for a local build"))
	}
	if sha == "" {
		s, err := d.Releases.Deployable(ctx)
		if err != nil {
			return failed(err)
		}
		sha = s
	}
	id := release.Short(sha)
	prog.Build = id
	if mk := prog.start; mk != nil {
		prog.start = func() Update { u := mk(); u.Commit = sha; return u }
	}
	if strings.HasPrefix(d.live(), id) {
		d.Unpause()
		return fmt.Sprintf("Fyr already runs %s, the newest release.", id), nil
	}
	d.recordChanges(prog, sha, func(from, to string) ([]Change, int, error) { return d.Releases.Changes(ctx, from, to) })
	if err := diskRoom(d.Data); err != nil {
		return failed(err)
	}
	out := d.releaseDir(sha)
	if _, err := os.Stat(filepath.Join(out, verifiedMark)); err != nil {
		d.report(prog, "download", "release "+release.Tag(sha), nil)
		os.MkdirAll(filepath.Join(d.Data, "builds"), 0o755)
		tmp, err := os.MkdirTemp(filepath.Join(d.Data, "builds"), ".dl-"+id+"-")
		if err != nil {
			return failed(err)
		}
		dctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		_, ferr := d.Releases.fetch(dctx, sha, tmp)
		cancel()
		if ferr != nil {
			os.RemoveAll(tmp)
			return failed(firstErr(fail(fmt.Errorf("%w; nothing was switched", ferr))))
		}
		if err := fsutil.WriteAtomic(filepath.Join(tmp, verifiedMark), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
			os.RemoveAll(tmp)
			return failed(err)
		}
		os.RemoveAll(out)
		if err := os.Rename(tmp, out); err != nil {
			os.RemoveAll(tmp)
			return failed(err)
		}
	}
	d.report(prog, "smoke", "", nil)
	if err := d.smoke(ctx, run, out, id); err != nil {
		if ctx.Err() == nil { // not a newer deploy that cancelled this one
			d.markBad(sha, "failed its smoke test") // #154: a bad build fails the same way again
		}
		return failed(firstErr(fail(fmt.Errorf("the smoke test of %s failed, nothing was switched: %w", id, err))))
	}
	return d.switchTo(ctx, prog, gen, threadID, req, id, sha, "https://github.com/"+d.Releases.repo(), "", out)
}

// smoke runs the build's own `fyrd selftest` on a snapshot of the database
// (#150): the binary starts on this machine, says it is the build it
// should be, opens the data with its schema and has its client. A few
// seconds; no make test, no slot.
func (d *Deployer) smoke(ctx context.Context, run Runner, out, id string) error {
	sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	exe := filepath.Join(out, "fyrd")
	b, err := run(sctx, out, exe, "version")
	if err != nil {
		return fmt.Errorf("fyrd version: %v %s", err, tail(b, 300))
	}
	if v := strings.TrimSpace(string(b)); id != "" && !strings.HasPrefix(v, id) {
		return fmt.Errorf("the binary says it is %q, not %s", v, id)
	}
	tmp, err := os.MkdirTemp("", "fyr-smoke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := d.DB.Snapshot(filepath.Join(tmp, "fyr.db")); err != nil {
		return fmt.Errorf("database snapshot for the smoke test: %w", err)
	}
	b, err = run(sctx, out, exe, "selftest", "-data", tmp)
	os.WriteFile(filepath.Join(out, "smoke.log"), b, 0o644)
	if err != nil {
		return fmt.Errorf("%v: %s", err, tail(b, 1500))
	}
	return nil
}

// Paused is a local build that holds auto-update off (#159).
type Paused struct {
	Build  string    `json:"build"`
	Commit string    `json:"commit,omitempty"`
	Branch string    `json:"branch,omitempty"`
	Dir    string    `json:"dir,omitempty"`
	At     time.Time `json:"at"`
}

const pausedKey = "update_paused"

// Pause records that a local build runs, so auto-update leaves it.
func (d *Deployer) pause(p Paused) {
	b, _ := json.Marshal(p)
	d.DB.SetKV(pausedKey, string(b))
}

// Unpause puts auto-update back on the release track.
func (d *Deployer) Unpause() { d.DB.SetKV(pausedKey, "") }

// PausedBy is the local build that pauses auto-update, or nil.
func (d *Deployer) PausedBy() *Paused {
	raw, _ := d.DB.GetKV(pausedKey)
	if raw == "" {
		return nil
	}
	var p Paused
	if json.Unmarshal([]byte(raw), &p) != nil {
		return nil
	}
	return &p
}

func (d *Deployer) live() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.Live
}
