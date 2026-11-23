package claudecli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/fsutil"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// SessionFile finds <config>/projects/<dir>/<session>.jsonl. It prefers the
// dir the CLI derives from cwd, and else takes any (long paths are cut).
func SessionFile(cfgDir, cwd, sid string) string {
	if sid == "" || strings.ContainsAny(sid, "/\\*?[") {
		return ""
	}
	exact := filepath.Join(cfgDir, "projects", nonAlnum(cwd), sid+".jsonl")
	if _, err := os.Stat(exact); err == nil {
		return exact
	}
	m, _ := filepath.Glob(filepath.Join(cfgDir, "projects", "*", sid+".jsonl"))
	if len(m) == 0 {
		return ""
	}
	return m[0]
}

func nonAlnum(s string) string {
	b := []byte(s)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			b[i] = '-'
		}
	}
	return string(b)
}

// CopySession puts the transcript, the session folder and its file history
// into the target's config dir under the same project dir.
func CopySession(src, fromDir, toDir, sid string) error {
	proj := filepath.Base(filepath.Dir(src))
	dstProj := filepath.Join(toDir, "projects", proj)
	if err := os.MkdirAll(dstProj, 0o700); err != nil {
		return err
	}
	if err := fsutil.CopyFile(src, filepath.Join(dstProj, sid+".jsonl"), 0o600); err != nil {
		return err
	}
	if err := fsutil.CopyTree(strings.TrimSuffix(src, ".jsonl"), filepath.Join(dstProj, sid)); err != nil {
		return err
	}
	return fsutil.CopyTree(filepath.Join(fromDir, "file-history", sid), filepath.Join(toDir, "file-history", sid))
}

// ErrSessionTooLarge is cli.ErrSessionTooLarge.
var ErrSessionTooLarge = cli.ErrSessionTooLarge

// ExportSession packs the transcript, its session folder and its file history
// into a gzip-compressed tar archive bounded by maxBytes (both raw and
// compressed) for cross-host handover rung 1 (decision 0039).
func ExportSession(src, fromDir, sid string, maxBytes int64) ([]byte, error) {
	if sid == "" || strings.ContainsAny(sid, "/\\*?[") {
		return nil, errors.New("invalid session id")
	}
	if maxBytes <= 0 {
		maxBytes = 20 << 20
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	var total int64
	addFile := func(diskPath, arcName string) error {
		fi, err := os.Stat(diskPath)
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		total += fi.Size()
		if total > maxBytes {
			return ErrSessionTooLarge
		}
		hdr := &tar.Header{Name: arcName, Mode: 0o600, Size: fi.Size(), ModTime: fi.ModTime()}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(diskPath)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, io.LimitReader(f, fi.Size()))
		return err
	}
	addTree := func(root, prefix string) error {
		if _, err := os.Stat(root); err != nil {
			return nil
		}
		return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !d.Type().IsRegular() {
				return err
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			return addFile(p, path.Join(prefix, filepath.ToSlash(rel)))
		})
	}
	if err := addFile(src, "session.jsonl"); err != nil {
		return nil, err
	}
	if err := addTree(strings.TrimSuffix(src, ".jsonl"), "session"); err != nil {
		return nil, err
	}
	if err := addTree(filepath.Join(fromDir, "file-history", sid), "file-history"); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	if int64(buf.Len()) > maxBytes {
		return nil, ErrSessionTooLarge
	}
	return buf.Bytes(), nil
}

// ImportSession unpacks an ExportSession archive into toDir under the target
// cwd's project folder so `claude --resume <sid>` finds it on the target host
// (decision 0039, V0.trans-host-handover).
func ImportSession(archive []byte, toDir, targetCwd, sid string, maxBytes int64) error {
	if sid == "" || strings.ContainsAny(sid, "/\\*?[") {
		return errors.New("invalid session id")
	}
	if targetCwd == "" {
		return errors.New("target cwd is required")
	}
	if maxBytes <= 0 {
		maxBytes = 20 << 20
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("bad session archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	dstProj := filepath.Join(toDir, "projects", nonAlnum(targetCwd))
	var total int64
	gotMain := false
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read session archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		if hdr.Size < 0 || total+hdr.Size > maxBytes {
			return ErrSessionTooLarge
		}
		clean := path.Clean(strings.TrimPrefix(hdr.Name, "./"))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("unsafe archive path %q", hdr.Name)
		}
		var dst string
		switch {
		case clean == "session.jsonl":
			dst = filepath.Join(dstProj, sid+".jsonl")
			gotMain = true
		case strings.HasPrefix(clean, "session/"):
			rel := strings.TrimPrefix(clean, "session/")
			dst = filepath.Join(dstProj, sid, filepath.FromSlash(rel))
		case strings.HasPrefix(clean, "file-history/"):
			rel := strings.TrimPrefix(clean, "file-history/")
			dst = filepath.Join(toDir, "file-history", sid, filepath.FromSlash(rel))
		default:
			return fmt.Errorf("unexpected archive entry %q", hdr.Name)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		cr := &capReader{r: tr, left: maxBytes - total}
		if err := fsutil.WriteAtomicFrom(dst, cr, 0o600); err != nil {
			return err
		}
		total = maxBytes - cr.left
	}
	if !gotMain {
		return errors.New("session archive is missing session.jsonl")
	}
	return nil
}

// capReader reads r until it has given left bytes more; a byte beyond that
// is ErrSessionTooLarge, so an archive larger than allowed is never kept.
type capReader struct {
	r    io.Reader
	left int64
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		var one [1]byte
		if n, _ := c.r.Read(one[:]); n > 0 {
			return 0, ErrSessionTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}
