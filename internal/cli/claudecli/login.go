package claudecli

import (
	"regexp"
	"strings"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/hostfs"
)

// SPEC 0.5 V0.53: the CLI's own login, run by Fyr. Two kinds:
//   - LoginFolder: `claude auth login` in the account's config folder; the
//     CLI keeps and refreshes the login there. It runs on plain pipes (it
//     prints the link and reads the code as lines).
//   - LoginToken: `claude setup-token`, which prints a long-lived token for
//     Fyr to keep in its secret store. It draws only in a terminal, so it
//     runs on a pseudo-terminal.
//
// Both print a sign-in link and wait for the code the sign-in page shows
// ("Paste code here if prompted >").
type LoginKind string

const (
	LoginFolder LoginKind = "folder"
	LoginToken  LoginKind = "token"
)

// LoginArgs are the arguments of a login and whether it needs a terminal.
func LoginArgs(k LoginKind) (args []string, terminal bool) {
	if k == LoginToken {
		return []string{"setup-token"}, true
	}
	return []string{"auth", "login", "--claudeai"}, false
}

// LoginEnv is the environment of a login: the daemon's, without any
// credential or config folder it carries, with configDir as
// CLAUDE_CONFIG_DIR ("" keeps the CLI's default folder), and with no way to
// open a browser on the host (the link is shown in Fyr's page instead).
func LoginEnv(base []string, configDir string) []string {
	if configDir == "" {
		return cli.LoginEnv(base, claude.CredentialVars)
	}
	return cli.LoginEnv(base, claude.CredentialVars, "CLAUDE_CONFIG_DIR="+hostfs.ExpandHome(configDir, ""))
}

// loginErrorWords mark a line of a login worth showing (cli.LastErrorLine).
var loginErrorWords = []string{"invalid", "error", "fail", "wasn't accepted", "expired"}

// LoginOut is what Fyr reads from a login's output so far.
type LoginOut struct {
	URL    string // the sign-in link
	Prompt bool   // it asks for the code
	// Token: the token `setup-token` printed, complete (followed by more
	// output). Only the secret store gets it.
	Token string
	// Line: the last line worth showing (an error such as "Invalid code"),
	// with anything token-like removed.
	Line string
	// Retry: the screen waits for Enter to start over (setup-token after a
	// wrong code).
	Retry bool
}

var (
	urlRE   = regexp.MustCompile(`https://claude\.(?:com|ai)/\S+|https://\S*oauth\S+`)
	tokenRE = regexp.MustCompile(`sk-ant-[a-z]+\d*-[A-Za-z0-9_-]{20,}`)
	longRE  = regexp.MustCompile(`[A-Za-z0-9_\-#]{32,}`)
)

// Plain is terminal output as text (shared in internal/cli).
func Plain(b []byte) string { return cli.Plain(b) }

// Redact removes anything token-like from a line meant for a person.
func Redact(s string) string {
	s = tokenRE.ReplaceAllString(s, "••••")
	return longRE.ReplaceAllString(s, "••••")
}

// ParseLogin reads a login's output so far. done: the program ended, so a
// token at the very end is complete. The link is the latest one shown (a
// retry shows a new one).
func ParseLogin(b []byte, done bool) LoginOut {
	var o LoginOut
	text := Plain(b)
	if ms := urlRE.FindAllString(text, -1); len(ms) > 0 {
		o.URL = strings.TrimRight(ms[len(ms)-1], ".,)")
	}
	o.Prompt = strings.Contains(text, "Paste code here")
	if loc := tokenRE.FindStringIndex(text); loc != nil && (done || loc[1] < len(text)) {
		o.Token = text[loc[0]:loc[1]]
	}
	o.Retry = strings.Contains(text, "Enter to retry")
	o.Line = cli.LastErrorLine(text, loginErrorWords, Redact)
	return o
}
