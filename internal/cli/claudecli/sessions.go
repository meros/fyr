package claudecli

import (
	"io"
	"io/fs"
	"os"
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
	if err := copyFile(src, filepath.Join(dstProj, sid+".jsonl")); err != nil {
		return err
	}
	if err := copyTree(strings.TrimSuffix(src, ".jsonl"), filepath.Join(dstProj, sid)); err != nil {
		return err
	}
	return copyTree(filepath.Join(fromDir, "file-history", sid), filepath.Join(toDir, "file-history", sid))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".fyr-tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// copyTree copies a folder if it exists; a missing one is not an error.
func copyTree(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(p, target)
	})
}
