// Package fsutil holds the one way Fyr replaces a file: written to a
// temporary file in the same folder, synced, and renamed over the target,
// so a reader sees the old content or the new and never half of it, and a
// crash leaves no short file behind.
//
// What it does not do, for a caller to weigh:
//   - The mode is forced: an existing file's mode is replaced by the mode
//     given, not kept.
//   - A symlink at path is replaced by a regular file, not written through.
//   - The temporary file is ".<name>.tmp-<random>" in the same folder. A crash
//     between create and rename leaves it there. The leading dot is the
//     contract: Fyr's readers of such folders key on the exact names they
//     write or on ids, and its tree walks skip hidden entries, so a stray one
//     is never an entry. A reader that lists every file of such a folder must
//     skip dot names too.
//   - The folder must exist and be writable: the rename needs it.
package fsutil

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
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
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	syncDir(filepath.Dir(path))
	return nil
}

// syncDir makes a rename durable. Best effort: a folder that cannot be opened
// or synced (some filesystems refuse) leaves the file as written.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
}

// SymlinkAtomic points path at target, replacing a link or file there, so
// that no reader finds path missing.
func SymlinkAtomic(target, path string) error {
	// A unique name, so two callers on one path never share a temporary link.
	var tmp string
	for i := 0; ; i++ {
		tmp = fmt.Sprintf("%s.tmp-%d-%d-%d", path, os.Getpid(), time.Now().UnixNano(), i)
		err := os.Symlink(target, tmp)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) || i > 100 {
			return err
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	syncDir(filepath.Dir(path))
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
