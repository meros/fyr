package claudecli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"github.com/meros/fyr/internal/proc"
)

// keychainTimeout bounds one `security` call (V0.review-hangs (4)): a locked
// Keychain can make it wait for a dialog nobody answers. securityBin is the
// tool; both are variables for the test.
var (
	keychainTimeout = 3 * time.Second
	securityBin     = "/usr/bin/security"
)

// security runs /usr/bin/security with args and returns its output, killed
// after keychainTimeout.
func security(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), keychainTimeout)
	defer cancel()
	return proc.Command(ctx, securityBin, args...).Output()
}

// On macOS the CLI keeps its login in the login Keychain, not in
// <config>/.credentials.json: the item "Claude Code-credentials", with "-"
// and the first 8 hex digits of sha256(the folder) when CLAUDE_CONFIG_DIR
// is set, for the account $USER (the CLI's own rule). It reads the item with
// `security`, so Fyr does the same and no Keychain dialog shows.

// keychainServices are the item names the folder dir can have: hashed when
// the CLI ran with CLAUDE_CONFIG_DIR, plain for the default folder without it.
func keychainServices(dir string) []string {
	sum := sha256.Sum256([]byte(dir))
	s := []string{"Claude Code-credentials-" + hex.EncodeToString(sum[:])[:8]}
	if h, err := os.UserHomeDir(); err == nil && filepath.Clean(dir) == filepath.Join(h, ".claude") {
		s = append(s, "Claude Code-credentials")
	}
	return s
}

var keychainUser = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func keychainAccount() string {
	n := os.Getenv("USER")
	if n == "" {
		if u, err := user.Current(); err == nil {
			n = u.Username
		}
	}
	if !keychainUser.MatchString(n) {
		return "claude-code-user"
	}
	return n
}

// Credentials is the CLI's stored login for the config folder dir: the JSON
// of <dir>/.credentials.json, else on macOS of its Keychain item.
func Credentials(dir string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(dir, ".credentials.json"))
	if err == nil || runtime.GOOS != "darwin" {
		return b, err
	}
	for _, s := range keychainServices(dir) {
		out, kerr := security("find-generic-password", "-a", keychainAccount(), "-w", "-s", s)
		if out = bytes.TrimSpace(out); kerr == nil && len(out) > 0 {
			return out, nil
		}
	}
	return nil, err
}

// hasKeychainLogin: macOS has the folder's Keychain item (its secret is not read).
func hasKeychainLogin(dir string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	for _, s := range keychainServices(dir) {
		if _, err := security("find-generic-password", "-a", keychainAccount(), "-s", s); err == nil {
			return true
		}
	}
	return false
}
