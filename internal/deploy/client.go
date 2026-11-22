package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/meros/fyr/internal/fsutil"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SPEC 0.5 V0.81: a deploy whose fyrd sources equal the running daemon's
// switches the web client only, without a drain or a restart.

// sumFile holds a build's SourceSum, beside its fyrd.
const sumFile = "server.sum"

// skipDirs are the checkout's folders that never reach the fyrd binary.
var skipDirs = map[string]bool{"node_modules": true, "bin": true, "docs": true, "research": true, "design": true, "spikes": true, "testdata": true, "__pycache__": true}

// SourceSum hashes the files of dir that can change fyrd: every regular file
// except hidden entries, symlinks, skipDirs, tests, Markdown, and everything
// under web/ but its Go files (the client is embedded, and it is what may
// differ). Anything unknown counts, so a doubt takes the full path.
func SourceSum(dir string) (string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == "." {
			return nil
		}
		name := e.Name()
		if e.IsDir() {
			if strings.HasPrefix(name, ".") || skipDirs[name] {
				return filepath.SkipDir
			}
			if strings.HasPrefix(rel, "web"+string(filepath.Separator)) {
				return filepath.SkipDir // web/src, web/dist, web/test…
			}
			return nil
		}
		if !e.Type().IsRegular() || strings.HasPrefix(name, ".") || strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, ".md") {
			return nil
		}
		if filepath.Dir(rel) == "web" && !strings.HasSuffix(name, ".go") {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", errors.New("no sources in " + dir)
	}
	sort.Strings(files)
	h := sha256.New()
	for _, rel := range files {
		f, err := os.Open(filepath.Join(dir, rel))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00", filepath.ToSlash(rel))
		n, err := io.Copy(h, f)
		f.Close()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "\x00%d\x00", n)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// liveSum is the SourceSum of the running daemon's build, or "".
func (d *Deployer) liveSum() string {
	dir := d.LiveDir
	if dir == "" {
		exe, err := os.Executable()
		if err != nil {
			return ""
		}
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		dir = filepath.Dir(exe)
	}
	b, _ := os.ReadFile(filepath.Join(dir, sumFile))
	return strings.TrimSpace(string(b))
}

// deployClient finishes a tested build on the client-only path: no
// snapshot, no drain, no restart; the result goes to the thread at once.
func (d *Deployer) deployClient(ctx context.Context, run Runner, prog *Progress, gen int, dir, out, id string) (string, error) {
	d.mu.Lock()
	if d.gen != gen {
		d.mu.Unlock()
		return "", errReplaced
	}
	d.cancel = nil // past this point the switch happens; nothing cancels it
	d.mu.Unlock()
	prog.Client = true // V0.118 (3)
	d.report(prog, "client", "", nil)
	build, err := d.switchClient(context.WithoutCancel(ctx), run, dir, out, id)
	if err != nil {
		d.report(prog, "failed", err.Error(), nil)
		return "", err
	}
	d.report(prog, "done", "", nil)
	return fmt.Sprintf("Fyr now runs build %s (%s); only the web client changed, so nothing restarted and open pages reload.", id, build), nil
}

// switchClient is the client-only path's end (V0.81): copy the built client
// into the build, point current at it (previous at the old one), and make the
// front serve it. A failure puts the links back; the old client stays.
func (d *Deployer) switchClient(ctx context.Context, run Runner, dir, out, id string) (string, error) {
	web := filepath.Join(out, "web")
	if err := copyDir(filepath.Join(dir, "web", "dist"), web); err != nil {
		return "", fmt.Errorf("copying the web client: %w; nothing was switched", err)
	}
	if _, err := os.Stat(filepath.Join(web, "index.html")); err != nil {
		return "", errors.New("the built web client has no index.html; nothing was switched")
	}
	build := id
	if b, err := run(ctx, dir, filepath.Join(out, "fyrd"), "version"); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" && !strings.ContainsAny(s, " \n\t") {
			build = s
		}
	}
	cur, prev := filepath.Join(d.Data, "current"), filepath.Join(d.Data, "previous")
	oldCur, curErr := os.Readlink(cur)
	oldPrev, prevErr := os.Readlink(prev)
	restore := func() {
		if curErr == nil {
			setLink(cur, oldCur)
		}
		if prevErr == nil {
			setLink(prev, oldPrev)
		} else {
			os.Remove(prev)
		}
	}
	if curErr == nil {
		if err := setLink(prev, oldCur); err != nil {
			return "", fmt.Errorf("linking previous: %w; nothing was switched", err)
		}
	}
	if err := setLink(cur, filepath.Join("builds", id)); err != nil {
		restore()
		return "", fmt.Errorf("linking current: %w; nothing was switched", err)
	}
	if d.SwitchClient != nil {
		if err := d.SwitchClient(build, web); err != nil {
			restore()
			return "", fmt.Errorf("switching the web client: %w; the old client stays", err)
		}
	}
	d.mu.Lock()
	d.Live = build
	d.mu.Unlock()
	return build, nil
}

// setLink points name at target atomically.
func setLink(name, target string) error {
	return fsutil.SymlinkAtomic(target, name)
}

// copyDir copies the regular files and folders of src into dst.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		to := filepath.Join(dst, rel)
		if e.IsDir() {
			return os.MkdirAll(to, 0o755)
		}
		if !e.Type().IsRegular() {
			return nil
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		o, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		if _, err := io.Copy(o, in); err != nil {
			o.Close()
			return err
		}
		return o.Close()
	})
}
