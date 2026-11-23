package agycli

import (
	"regexp"
	"strings"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/hostfs"
)

// LoginArgs are the arguments for `agy acp --login`, which prints the Google
// OAuth URL and reads the authorization code (or redirect URL) on plain pipes.
func LoginArgs(configDir string) (args []string, terminal bool) {
	args = []string{"acp", "--login"}
	if configDir != "" {
		args = append(args, "--gemini-dir", hostfs.ExpandHome(configDir, ""))
	}
	return args, false
}

// LoginEnv is the environment of an `agy` login: scrubs API-key and browser
// variables so `agy acp --login` prints the sign-in URL instead of opening a
// local browser.
func LoginEnv(base []string) []string { return cli.LoginEnv(base, CredentialVars) }

// loginErrorWords mark a line of a login worth showing (cli.LastErrorLine).
var loginErrorWords = []string{"invalid", "error", "failed", "cancelled", "canceled", "expired"}

// LoginOut is what Fyr reads from `agy acp --login`'s output so far.
type LoginOut struct {
	URL    string // Google OAuth sign-in link
	Prompt bool   // waiting for authorization code
	Line   string // last redacted error line, if any
}

var (
	agyURLRE = regexp.MustCompile(`https://accounts\.google\.com/o/oauth2/auth\S+|https://\S*oauth\S+`)
	longRE   = regexp.MustCompile(`[A-Za-z0-9_\-#/]{40,}`)
)

// Redact removes anything token-like from a line meant for a person.
func Redact(s string) string {
	return longRE.ReplaceAllString(s, "••••")
}

// ParseLogin reads `agy acp --login` output so far.
func ParseLogin(b []byte) LoginOut {
	var o LoginOut
	text := cli.Plain(b)
	if ms := agyURLRE.FindAllString(text, -1); len(ms) > 0 {
		o.URL = strings.TrimRight(ms[len(ms)-1], ".,)")
	}
	lowAll := strings.ToLower(text)
	o.Prompt = strings.Contains(lowAll, "paste the authorization code") || strings.Contains(lowAll, "paste code")
	o.Line = cli.LastErrorLine(text, loginErrorWords, Redact)
	return o
}
