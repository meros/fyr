package claudecli

import (
	"github.com/meros/fyr/internal/hostfs"
	"regexp"
	"strings"
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
	drop := map[string]bool{"CLAUDE_CONFIG_DIR": true, "CLAUDE_CODE_OAUTH_TOKEN": true, "ANTHROPIC_API_KEY": true,
		"ANTHROPIC_AUTH_TOKEN": true, "ANTHROPIC_BASE_URL": true, "DISPLAY": true, "WAYLAND_DISPLAY": true, "BROWSER": true, "TERM": true}
	out := []string{}
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[k] {
			out = append(out, kv)
		}
	}
	if configDir != "" {
		out = append(out, "CLAUDE_CONFIG_DIR="+hostfs.ExpandHome(configDir, ""))
	}
	return append(out, "BROWSER=true", "TERM=xterm-256color", "NO_COLOR=1")
}

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
	// An OSC 8 hyperlink carries the whole link even when its visible text
	// wraps: the hyperlink becomes its link.
	osc8RE  = regexp.MustCompile("(?s)\x1b\\]8;[^;\x07\x1b]*;(https://[^\x07\x1b]+)(?:\x07|\x1b\\\\).*?\x1b\\]8;;(?:\x07|\x1b\\\\)")
	oscRE   = regexp.MustCompile("\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)")
	fwdRE   = regexp.MustCompile("\x1b\\[\\d*[CG]")   // cursor forward or to a column: a space on screen
	downRE  = regexp.MustCompile("\x1b\\[\\d*[ABEF]") // cursor up or down: a new line
	csiRE   = regexp.MustCompile("\x1b(?:\\[[0-?]*[ -/]*[@-~]|[()][0-9A-Za-z]|[78=>NOcM])")
	urlRE   = regexp.MustCompile(`https://claude\.(?:com|ai)/\S+|https://\S*oauth\S+`)
	tokenRE = regexp.MustCompile(`sk-ant-[a-z]+\d*-[A-Za-z0-9_-]{20,}`)
	longRE  = regexp.MustCompile(`[A-Za-z0-9_\-#]{32,}`)
)

// Plain is terminal output as text: escapes removed, cursor moves as
// spaces, line ends as \n.
func Plain(b []byte) string {
	s := string(b)
	s = osc8RE.ReplaceAllString(s, " $1 ")
	s = oscRE.ReplaceAllString(s, "")
	s = fwdRE.ReplaceAllString(s, " ")
	s = downRE.ReplaceAllString(s, "\n")
	s = csiRE.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
}

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
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.Join(strings.Fields(lines[i]), " ")
		low := strings.ToLower(l)
		if strings.Contains(l, "https://") || !(strings.Contains(low, "invalid") || strings.Contains(low, "error") ||
			strings.Contains(low, "fail") || strings.Contains(low, "wasn't accepted") || strings.Contains(low, "expired")) {
			continue
		}
		l = Redact(l)
		if len(l) > 240 {
			l = l[:240]
		}
		o.Line = l
		break
	}
	return o
}
