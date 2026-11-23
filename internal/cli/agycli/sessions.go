package agycli

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/fsutil"
	"github.com/meros/fyr/internal/hostfs"
)

// GeminiDir returns the `.gemini` root folder for configDir (default ~/.gemini).
func GeminiDir(configDir string) string {
	if configDir != "" {
		return filepath.Clean(hostfs.ExpandHome(configDir, ""))
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".gemini")
}

// AppDataDir returns the Antigravity CLI data directory inside GeminiDir
// (`<geminiDir>/antigravity-cli`, unless configDir already names it).
func AppDataDir(configDir string) string {
	dir := GeminiDir(configDir)
	if filepath.Base(dir) == "antigravity-cli" {
		return dir
	}
	return filepath.Join(dir, "antigravity-cli")
}

// SessionsDir is where Antigravity CLI keeps its conversations and brain data
// for auth `a`.
func SessionsDir(a cli.Auth) string {
	return AppDataDir(a.ConfigDir)
}

// SessionFile finds `<dir>/conversations/<sid>.db`, or "" when absent.
func SessionFile(dir, cwd, sid string) string {
	if sid == "" || strings.ContainsAny(sid, "/\\*?[") {
		return ""
	}
	p := filepath.Join(dir, "conversations", sid+".db")
	if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
		return p
	}
	return ""
}

// CopySession copies a session's conversation database (plus any SQLite WAL/SHM
// sidecars) and brain folder between two Antigravity app-data dirs.
func CopySession(src, fromDir, toDir, sid string) error {
	dstConv := filepath.Join(toDir, "conversations")
	if err := os.MkdirAll(dstConv, 0o700); err != nil {
		return err
	}
	if err := fsutil.CopyFile(src, filepath.Join(dstConv, sid+".db"), 0o600); err != nil {
		return err
	}
	for _, suf := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(src + suf); err == nil {
			if err := fsutil.CopyFile(src+suf, filepath.Join(dstConv, sid+".db"+suf), 0o600); err != nil {
				return err
			}
		}
	}
	return copyTree(filepath.Join(fromDir, "brain", sid), filepath.Join(toDir, "brain", sid))
}

// LoggedIn reports whether Antigravity CLI has a stored login for auth `a`
// without running a subprocess or reading any secret value:
//   - `<appDataDir>/cache/onboarding.json` has `onboardingComplete: true`, OR
//   - `<geminiDir>/oauth_creds.json` exists as a regular file, OR
//   - `AGY_ADC_AUTH=true` when using the default config dir.
func LoggedIn(a cli.Auth) bool {
	appDir := AppDataDir(a.ConfigDir)
	if hasOnboardingComplete(filepath.Join(appDir, "cache", "onboarding.json")) {
		return true
	}
	gdir := GeminiDir(a.ConfigDir)
	if st, err := os.Stat(filepath.Join(gdir, "oauth_creds.json")); err == nil && st.Mode().IsRegular() {
		return true
	}
	if a.ConfigDir == "" && strings.EqualFold(os.Getenv("AGY_ADC_AUTH"), "true") {
		return true
	}
	return false
}

func hasOnboardingComplete(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var o struct {
		OnboardingComplete           bool `json:"onboardingComplete"`
		ConsumerOnboardingComplete   bool `json:"consumerOnboardingComplete"`
		EnterpriseOnboardingComplete bool `json:"enterpriseOnboardingComplete"`
	}
	if json.Unmarshal(b, &o) != nil {
		return false
	}
	return o.OnboardingComplete || o.ConsumerOnboardingComplete || o.EnterpriseOnboardingComplete
}


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
		return fsutil.CopyFile(p, target, 0o600)
	})
}
