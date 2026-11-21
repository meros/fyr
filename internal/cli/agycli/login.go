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
func LoginEnv(base []string) []string {
	drop := map[string]bool{
		"GEMINI_API_KEY":         true,
		"GOOGLE_GEMINI_BASE_URL": true,
		"AGY_ADC_AUTH":           true,
		"DISPLAY":                true,
		"WAYLAND_DISPLAY":        true,
		"BROWSER":                true,
		"TERM":                   true,
	}
	out := make([]string, 0, len(base)+3)
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[k] {
			out = append(out, kv)
		}
	}
	return append(out, "BROWSER=true", "TERM=xterm-256color", "NO_COLOR=1")
}

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
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.Join(strings.Fields(lines[i]), " ")
		low := strings.ToLower(l)
		if strings.Contains(l, "https://") || !(strings.Contains(low, "invalid") || strings.Contains(low, "error") ||
			strings.Contains(low, "failed") || strings.Contains(low, "cancelled") || strings.Contains(low, "canceled") || strings.Contains(low, "expired")) {
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
