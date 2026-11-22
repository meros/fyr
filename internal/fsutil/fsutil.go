// Package fsutil holds the one way Fyr replaces a file: written to a
// temporary file in the same folder, synced, and renamed over the target,
// so a reader sees the old content or the new and never half of it, and a
// crash leaves no short file behind.
package fsutil

import (
	"io"
	"os"
	"path/filepath"
)

// WriteAtomic replaces path with data, created with mode where it is new
// (the mode is also set on the replacement of an existing file).
func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	return writeAtomic(path, mode, func(f *os.File) error {
		_, err := f.Write(data)
		return err
	})
}

// WriteAtomicFrom is WriteAtomic with the content read from r. If r fails,
// path is left as it was.
func WriteAtomicFrom(path string, r io.Reader, mode os.FileMode) error {
	return writeAtomic(path, mode, func(f *os.File) error {
		_, err := io.Copy(f, r)
		return err
	})
}

func writeAtomic(path string, mode os.FileMode, fill func(*os.File) error) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if err = fill(f); err != nil {
		return err
	}
	if err = f.Chmod(mode); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SymlinkAtomic points path at target, replacing a link or file there, so
// that no reader finds path missing.
func SymlinkAtomic(target, path string) error {
	tmp := path + ".tmp"
	os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// CopyFile replaces dst with the content of src, atomically, with mode.
func CopyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return WriteAtomicFrom(dst, in, mode)
}
