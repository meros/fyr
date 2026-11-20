// Package release is what CI publishes for every green commit on main and
// what a host checks before it runs it (decision 0027, #158, #149): fyr and
// fyrd for each OS and architecture, a manifest, SHA256SUMS and an ed25519
// signature of SHA256SUMS. Auto-update installs nothing else.
package release

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PublicKey verifies every release unless the config names another key
// (updateKey, V0.release-without-gh). Its private half is the repo secret
// FYR_RELEASE_KEY; `go run ./cmd/fyrrelease keygen` makes a new pair.
const PublicKey = "iXJ77L7f2XSk5YZtHIUqpjHl0/qmuh328YRyGP6Igyk="

// ParseKey reads a base64 ed25519 public key.
func ParseKey(pubB64 string) (ed25519.PublicKey, error) {
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pubB64))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, errors.New("bad release public key")
	}
	return ed25519.PublicKey(pub), nil
}

// Fingerprint names a base64 public key the way ssh-keygen -l does:
// "SHA256:" and the unpadded base64 of the key's SHA-256
// (V0.release-without-gh: Settings → Updates shows it).
func Fingerprint(pubB64 string) (string, error) {
	pub, err := ParseKey(pubB64)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(pub)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(h[:]), nil
}

// Targets are the OS/architecture pairs CI builds.
var Targets = []string{"linux-amd64", "linux-arm64", "darwin-arm64", "darwin-amd64"}

// File names inside a release.
const (
	Sums     = "SHA256SUMS"
	Sig      = "SHA256SUMS.sig"
	Manifest = "manifest.json"
	// TagPrefix + the 12-character SHA is a release's tag.
	TagPrefix = "build-"
	// Deployable is the tag CI moves to the newest released commit.
	Deployable = "deployable"
)

// Here is this host's target, e.g. "linux-amd64".
func Here() string { return runtime.GOOS + "-" + runtime.GOARCH }

// Asset is the name of a binary for a target: "fyrd-linux-amd64".
func Asset(bin, target string) string { return bin + "-" + target }

// Tag is the release tag of a commit.
func Tag(sha string) string { return TagPrefix + Short(sha) }

// Short is the 12-character form of a SHA, the build id.
func Short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// Info is manifest.json.
type Info struct {
	Commit    string `json:"commit"`
	Build     string `json:"build"`
	ServerSum string `json:"serverSum,omitempty"`
	Go        string `json:"go,omitempty"`
	Node      string `json:"node,omitempty"`
	FlakeLock string `json:"flakeLock,omitempty"`
	Time      string `json:"time,omitempty"`
	Run       string `json:"run,omitempty"`
}

// SumFile hashes files (names relative to dir) into SHA256SUMS lines.
func SumFile(dir string, names []string) ([]byte, error) {
	var b bytes.Buffer
	for _, n := range names {
		h, err := hashFile(filepath.Join(dir, n))
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "%s  %s\n", h, n)
	}
	return b.Bytes(), nil
}

func hashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ParseSums reads SHA256SUMS: name → hex digest.
func ParseSums(b []byte) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		h, name, ok := strings.Cut(line, "  ")
		name = strings.TrimPrefix(name, "*")
		if !ok || len(h) != 64 || name == "" || strings.ContainsAny(name, "/\\") {
			return nil, fmt.Errorf("bad line in %s: %q", Sums, line)
		}
		out[name] = strings.ToLower(h)
	}
	return out, sc.Err()
}

// Sign signs data with a base64 ed25519 private key (seed or full key).
func Sign(privB64 string, data []byte) (string, error) {
	k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(privB64))
	if err != nil {
		return "", fmt.Errorf("release key: %w", err)
	}
	var priv ed25519.PrivateKey
	switch len(k) {
	case ed25519.SeedSize:
		priv = ed25519.NewKeyFromSeed(k)
	case ed25519.PrivateKeySize:
		priv = ed25519.PrivateKey(k)
	default:
		return "", errors.New("release key: wrong length")
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)), nil
}

// VerifySig checks a base64 signature of data against a base64 public key.
func VerifySig(pubB64 string, data []byte, sigB64 string) error {
	pub, err := ParseKey(pubB64)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("bad signature encoding")
	}
	if !ed25519.Verify(pub, data, sig) {
		return errors.New("signature does not match the release key")
	}
	return nil
}

// Verify checks a downloaded release in dir: the signature of SHA256SUMS
// with pub, then every file the sums name that is present in dir, and that
// each of want is present. It returns the manifest.
func Verify(dir, pub string, want ...string) (Info, error) {
	var info Info
	sums, err := os.ReadFile(filepath.Join(dir, Sums))
	if err != nil {
		return info, err
	}
	sig, err := os.ReadFile(filepath.Join(dir, Sig))
	if err != nil {
		return info, err
	}
	if err := VerifySig(pub, sums, string(sig)); err != nil {
		return info, err
	}
	m, err := ParseSums(sums)
	if err != nil {
		return info, err
	}
	for _, n := range append([]string{Manifest}, want...) {
		if _, ok := m[n]; !ok {
			return info, fmt.Errorf("%s is not in %s", n, Sums)
		}
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			return info, fmt.Errorf("%s is missing", n)
		}
	}
	for n, h := range m {
		p := filepath.Join(dir, n)
		if _, err := os.Stat(p); err != nil {
			continue // another target's binary
		}
		got, err := hashFile(p)
		if err != nil {
			return info, err
		}
		if got != h {
			return info, fmt.Errorf("checksum of %s does not match", n)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, Manifest))
	if err != nil {
		return info, err
	}
	if err := json.Unmarshal(b, &info); err != nil {
		return info, fmt.Errorf("%s: %w", Manifest, err)
	}
	if len(info.Commit) != 40 {
		return info, fmt.Errorf("%s names no full commit", Manifest)
	}
	return info, nil
}
