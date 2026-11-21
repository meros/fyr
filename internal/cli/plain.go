package cli

import (
	"regexp"
	"strings"
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
