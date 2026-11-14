// Package secrets keeps Fyr's secrets (API keys, login tokens) on the host
// (SPEC 0.5 V0.47, decision 0015). Callers name a secret and never touch
// its storage: the Backend is swappable (files today; age, sops or
// systemd-creds later). A value never appears in a log line, an error or
// a reply; clients only learn whether a secret is set and its last 4
// characters.
package secrets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrNotFound: no secret by that name.
var ErrNotFound = errors.New("secret not set")

// Backend stores secret values by name.
type Backend interface {
	Put(name string, value []byte) error
	Get(name string) ([]byte, error) // ErrNotFound when missing
	Delete(name string) error        // no error when missing
}

// Store is the one place Fyr reads and writes secrets.
type Store struct{ B Backend }

// Files is the file backend: one file per secret, mode 0600, in a 0700
// folder (default ~/.local/share/fyr/secrets).
func Files(dir string) *Store { return &Store{B: &fileBackend{dir: dir}} }

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ValidName reports whether a name is safe to store under.
func ValidName(name string) bool { return nameRE.MatchString(name) && !strings.Contains(name, "..") }

func check(name string) error {
	if !ValidName(name) {
		return fmt.Errorf("bad secret name %q", name)
	}
	return nil
}

// Put stores a value (trimmed); an empty value deletes the secret.
func (s *Store) Put(name, value string) error {
	if err := check(name); err != nil {
		return err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return s.B.Delete(name)
	}
	return s.B.Put(name, []byte(value))
}

// Get returns the value, or ErrNotFound.
func (s *Store) Get(name string) (string, error) {
	if err := check(name); err != nil {
		return "", err
	}
	b, err := s.B.Get(name)
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", ErrNotFound
	}
	return v, nil
}

// Has reports whether a non-empty secret is set.
func (s *Store) Has(name string) bool { _, err := s.Get(name); return err == nil }

// Delete removes a secret; a missing one is no error.
func (s *Store) Delete(name string) error {
	if err := check(name); err != nil {
		return err
	}
	return s.B.Delete(name)
}

// Last4 is what a client may see of a value: its last 4 characters, and
// nothing for a value shorter than 12.
func Last4(v string) string {
	if len(v) < 12 {
		return ""
	}
	return v[len(v)-4:]
}

// Last4 of a stored secret, "" when unset or short.
func (s *Store) Last4(name string) string {
	v, err := s.Get(name)
	if err != nil {
		return ""
	}
	return Last4(v)
}

type fileBackend struct{ dir string }

func (f *fileBackend) path(name string) string { return filepath.Join(f.dir, name) }

func (f *fileBackend) Put(name string, value []byte) error {
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return fmt.Errorf("secret store: %w", err)
	}
	os.Chmod(f.dir, 0o700)
	tmp, err := os.CreateTemp(f.dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("secret store: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secret store: %w", err)
	}
	if _, err := tmp.Write(append(value, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("secret store: write failed")
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("secret store: write failed")
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("secret store: write failed")
	}
	if err := os.Rename(tmp.Name(), f.path(name)); err != nil {
		return fmt.Errorf("secret store: %w", err)
	}
	return nil
}

func (f *fileBackend) Get(name string) ([]byte, error) {
	b, err := os.ReadFile(f.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("secret store: cannot read %s", name)
	}
	return b, nil
}

func (f *fileBackend) Delete(name string) error {
	if err := os.Remove(f.path(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("secret store: %w", err)
	}
	return nil
}

// Ref is a secret kept outside the store that Fyr may read at runtime:
// "env:NAME" (the daemon's environment) or "file:/abs/path" (such as a
// systemd or sops secret under /run/secrets). Auto-imported providers
// point at one until the owner stores a key (V0.47).
func Ref(ref string, getenv func(string) string) (string, error) {
	switch {
	case strings.HasPrefix(ref, "env:"):
		if getenv == nil {
			getenv = os.Getenv
		}
		if v := strings.TrimSpace(getenv(ref[4:])); v != "" {
			return v, nil
		}
		return "", ErrNotFound
	case strings.HasPrefix(ref, "file:") && filepath.IsAbs(ref[5:]):
		b, err := os.ReadFile(ref[5:])
		if err != nil {
			return "", ErrNotFound
		}
		if v := strings.TrimSpace(string(b)); v != "" {
			return v, nil
		}
		return "", ErrNotFound
	}
	return "", ErrNotFound
}

// RefExists reports whether a ref points at something, without reading a
// file's contents.
func RefExists(ref string, getenv func(string) string) bool {
	if strings.HasPrefix(ref, "file:") {
		st, err := os.Stat(ref[5:])
		return filepath.IsAbs(ref[5:]) && err == nil && st.Mode().IsRegular() && st.Size() > 0
	}
	_, err := Ref(ref, getenv)
	return err == nil
}
