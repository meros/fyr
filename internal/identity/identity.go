// Package identity is an install's own key (decision 0026, SPEC 0.5 V0.124).
// Each fyrd makes one Ed25519 key pair at its first start and keeps it in its
// data folder. The key's id names the install: a host is known by its id,
// not by its address or its name.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// File is the key's file name in the data folder.
const File = "identity.key"

const pemType = "FYR ED25519 PRIVATE KEY"

// Key is an install's key pair.
type Key struct {
	Private ed25519.PrivateKey
	Public  ed25519.PublicKey
}

// Load reads the key in dir, or makes one when there is none. A key file
// that cannot be read as a key is an error: it is never replaced, because a
// new key is a new install to every front that knew the old one.
func Load(dir string) (*Key, error) {
	path := filepath.Join(dir, File)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return create(dir, path)
	}
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil || blk.Type != pemType || len(blk.Bytes) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s is not a Fyr install key; move it away to make a new one (a new key is a new install to the other fronts)", path)
	}
	priv := ed25519.NewKeyFromSeed(blk.Bytes)
	return &Key{Private: priv, Public: priv.Public().(ed25519.PublicKey)}, nil
}

func create(dir, path string) (*Key, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	b := pem.EncodeToMemory(&pem.Block{Type: pemType, Bytes: priv.Seed()})
	// O_EXCL: two processes starting at once keep one key.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return Load(dir)
	}
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return nil, err
	}
	return &Key{Private: priv, Public: pub}, nil
}

// ID is the install's id: the SHA-256 of its public key, the first 80 bits
// in base32, as four groups of four ("F7QK-2M9X-…").
func (k *Key) ID() string { return IDOf(k.Public) }

// IDOf is the id of a public key.
func IDOf(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:10])
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
}

// Short is the first group of an id, the form a row shows.
func Short(id string) string {
	s, _, _ := strings.Cut(id, "-")
	return s
}

// NextFile is where a rotated key waits until it takes over (decision 0030 3.6).
const NextFile = "identity.key.next"

// New makes a key pair that is kept nowhere yet.
func New() (*Key, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Key{Private: priv, Public: pub}, nil
}

func encode(k *Key) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: pemType, Bytes: k.Private.Seed()})
}

// WriteNext keeps a new key beside the current one, 0600; Promote makes it the
// install's key once the roster says so.
func WriteNext(dir string, k *Key) error {
	return os.WriteFile(filepath.Join(dir, NextFile), encode(k), 0o600)
}

// LoadNext reads the waiting key; nil with no error when there is none.
func LoadNext(dir string) (*Key, error) {
	b, err := os.ReadFile(filepath.Join(dir, NextFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil || blk.Type != pemType || len(blk.Bytes) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s is not a Fyr install key", NextFile)
	}
	priv := ed25519.NewKeyFromSeed(blk.Bytes)
	return &Key{Private: priv, Public: priv.Public().(ed25519.PublicKey)}, nil
}

// Promote makes the waiting key the install's key: the old one is kept as
// identity.key.old, the new one is moved over it. A key that may be stolen is
// removed, not rotated: the old file is only for the owner's records.
func Promote(dir string) error {
	cur := filepath.Join(dir, File)
	if b, err := os.ReadFile(cur); err == nil {
		os.WriteFile(filepath.Join(dir, File+".old"), b, 0o600)
	}
	return os.Rename(filepath.Join(dir, NextFile), cur)
}

// DropNext removes a waiting key that was never taken into use.
func DropNext(dir string) { os.Remove(filepath.Join(dir, NextFile)) }
