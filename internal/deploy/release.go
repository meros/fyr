package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
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

// Releases reads Fyr's releases on GitHub; Gh runs the gh CLI.
type Releases struct {
	Repo string // owner/name, default meros/fyr
	Gh   func(ctx context.Context, args ...string) ([]byte, error)
	// Git runs git (tests); nil is the git CLI (V0.173).
	Git func(ctx context.Context, args ...string) ([]byte, error)
	// Key verifies a release; empty is release.PublicKey.
	Key string
}

func (r *Releases) repo() string {
	if r.Repo == "" {
		return "meros/fyr"
	}
	return r.Repo
}

// Deployable is the commit the tag `deployable` points at: CI's newest
// green, published commit. V0.173: read with git ls-remote, which costs no
// GitHub API call; the API only when git cannot say.
func (r *Releases) Deployable(ctx context.Context) (string, error) {
	if sha, err := r.lsRemote(ctx); err == nil {
		return sha, nil
	} else {
		log.Printf("deployable by git: %v; asking the API", err)
	}
	b, err := r.Gh(ctx, "api", "repos/"+r.repo()+"/git/ref/tags/"+release.Deployable, "--jq", ".object.sha")
	sha := strings.TrimSpace(string(b))
	if err != nil {
		return "", fmt.Errorf("reading %s: %v %s", release.Deployable, err, sha)
	}
	if !isSHA(sha) {
		return "", fmt.Errorf("%s points at %q", release.Deployable, sha)
	}
	return sha, nil
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
func (r *Releases) lsRemote(ctx context.Context) (string, error) {
	git := r.Git
	if git == nil {
		git = func(ctx context.Context, args ...string) ([]byte, error) {
			c, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			cmd := proc.Command(c, "git", args...)
			cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
			return cmd.Output()
		}
	}
	ref := "refs/tags/" + release.Deployable
	// gh is the credential helper: the repo may be private, and gh holds
	// the login (it reads its stored token; no API call).
	out, err := git(ctx, "-c", "credential.helper=", "-c", "credential.helper=!gh auth git-credential",
		"ls-remote", "--tags", "https://github.com/"+r.repo()+".git", ref)
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
	b, err := r.Gh(ctx, "api", "repos/"+r.repo()+"/compare/"+ancestor+"..."+commit, "--jq", ".status")
	s := strings.TrimSpace(string(b))
	return err == nil && (s == "ahead" || s == "identical")
}

// fetch downloads the release of sha for this host into dir (a fresh
// folder), verifies it and lays it out as a build: fyrd, fyr,
// manifest.json, server.sum.
func (r *Releases) fetch(ctx context.Context, sha, dir string) (release.Info, error) {
	t := release.Here()
	fyrd, fyr := release.Asset("fyrd", t), release.Asset("fyr", t)
	args := []string{"release", "download", release.Tag(sha), "-R", r.repo(), "-D", dir,
		"-p", fyrd, "-p", fyr, "-p", release.Manifest, "-p", release.Sums, "-p", release.Sig}
	if b, err := r.Gh(ctx, args...); err != nil {
		return release.Info{}, fmt.Errorf("downloading %s: %v %s", release.Tag(sha), err, tail(b, 500))
	}
	key := r.Key
	if key == "" {
		key = release.PublicKey
	}
	info, err := release.Verify(dir, key, fyrd, fyr)
	if err != nil {
		return info, fmt.Errorf("release %s did not verify: %w", release.Tag(sha), err)
	}
	if info.Commit != sha {
		return info, fmt.Errorf("release %s is for commit %s, not %s", release.Tag(sha), release.Short(info.Commit), release.Short(sha))
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
		os.WriteFile(filepath.Join(dir, sumFile), []byte(info.ServerSum+"\n"), 0o644)
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
		os.WriteFile(filepath.Join(tmp, verifiedMark), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
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
