package cli

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/meros/fyr/internal/launchenv"
)

var (
	// An OSC 8 hyperlink carries the whole link even when its visible text
	// wraps: the hyperlink becomes its link.
	osc8RE = regexp.MustCompile("(?s)\x1b\\]8;[^;\x07\x1b]*;(https://[^\x07\x1b]+)(?:\x07|\x1b\\\\).*?\x1b\\]8;;(?:\x07|\x1b\\\\)")
	oscRE  = regexp.MustCompile("\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)")
	fwdRE  = regexp.MustCompile("\x1b\\[\\d*[CG]")   // cursor forward or to a column: a space on screen
	downRE = regexp.MustCompile("\x1b\\[\\d*[ABEF]") // cursor up or down: a new line
	csiRE  = regexp.MustCompile("\x1b(?:\\[[0-?]*[ -/]*[@-~]|[()][0-9A-Za-z]|[78=>NOcM])")
)

// Plain is terminal output as text: escapes removed, cursor moves as
// spaces, line ends as \n. Shared by CLI adapters when parsing interactive
// login output.
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

// Clip is s cut to at most n runes: a byte cut could split a character
// and leave invalid UTF-8 in a line meant for a person.
func Clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// ErrText is a CLI's error output as one message: trimmed, its last 300
// runes (the end says what failed), or fallback when there is none.
func ErrText(msg, fallback string) string {
	msg = strings.TrimSpace(msg)
	if r := []rune(msg); len(r) > 300 {
		msg = "…" + string(r[len(r)-300:])
	}
	if msg == "" {
		return fallback
	}
	return msg
}

// LastErrorLine is the line of a login's output worth showing a person: the
// last one that holds one of words (lower case) and no link, one-spaced,
// redacted and cut to 240 runes; "" without one.
func LastErrorLine(text string, words []string, redact func(string) string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.Join(strings.Fields(lines[i]), " ")
		if strings.Contains(l, "https://") {
			continue
		}
		low := strings.ToLower(l)
		for _, w := range words {
			if strings.Contains(low, w) {
				return Clip(redact(l), 240)
			}
		}
	}
	return ""
}

// LoginEnv is the environment of an interactive login: base without the
// adapter's credential variables and without a way to open a browser on
// the host (the sign-in link is shown in Fyr's page instead), then extra.
func LoginEnv(base, credentials []string, extra ...string) []string {
	out := launchenv.Without(base, slices.Concat(credentials, []string{"DISPLAY", "WAYLAND_DISPLAY", "BROWSER", "TERM"}))
	return append(append(out, extra...), "BROWSER=true", "TERM=xterm-256color", "NO_COLOR=1")
}
