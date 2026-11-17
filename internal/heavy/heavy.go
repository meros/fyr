// Package heavy recognises heavy commands (full test suites, builds, e2e
// runs) in an agent's Bash command and names their class, the part of a job's
// identity that recurs across threads (decision 0029 section 2, #185; #174
// uses the same patterns). The host setting `heavyCommands` replaces the
// defaults.
package heavy

import (
	"fmt"
	"regexp"
	"strings"
)

// Pattern is one heavy command: Re matches it, Name is its class, where $1
// is Re's first group.
type Pattern struct {
	Name string `json:"name"`
	Re   string `json:"pattern"`
}

// Defaults are the patterns without a host setting (#174: the commands that
// ran 1–10 min in the thread logs). A pattern matches where the shell runs a
// command: at the start of a segment (see segments). A quick check (one
// package, -run; `npm test`, 3 s at the median; `npm run build`, 9 s) is
// not a heavy job.
var Defaults = []Pattern{
	{"make $1", `make\s+(?:-C\s+\S+\s+|-\S+\s+)*(test[\w:-]*|check|e2e[\w-]*)\b`},
	{"go test ./...", `go\s+test\b[^|;&]*\s\./(?:[\w.-]+/)*\.\.\.`},
	{"playwright test", `playwright\s+test\b`},
	{"yarn $1", `yarn\s+(?:run\s+)?(test[\w:-]*|screenshot:diff|e2e[\w:-]*)\b`},
	{"npm run $1", `npm\s+run\s+(test:[\w:-]+|e2e[\w:-]*)\b`},
	{"nix build", `nix\s+build\b`},
	{"cargo test", `cargo\s+test\b`},
	{"pytest", `pytest\b`},
	// A browser harness over many headless Chrome tabs (the layout and
	// timing tests), a repo's pre-push gate, and anything that starts
	// headless Chrome: the two jobs behind the 14:04 thrash matched none.
	{"node $1", `node\s+(?:--\S+\s+)*(?:\S+/)?([\w.-]*e2e[\w.-]*\.m?js)\b`},
	{"yarn $1", `yarn\s+(?:run\s+)?(pre-push[\w:-]*)`},
	{"headless chrome", `(?:google-chrome|chromium|chrome)(?:-\S+)?\s+(?:\S+\s+)*?--headless\b`},
}

type rule struct {
	name string
	re   *regexp.Regexp
}

// Matcher names the class of a command.
type Matcher struct{ rules []rule }

// New compiles ps; a pattern that does not compile is an error.
func New(ps []Pattern) (*Matcher, error) {
	m := &Matcher{}
	for _, p := range ps {
		re, err := regexp.Compile(`^(?:` + p.Re + `)`)
		if err != nil {
			return nil, fmt.Errorf("heavy command %q: %w", p.Name, err)
		}
		m.rules = append(m.rules, rule{p.Name, re})
	}
	return m, nil
}

// Default is the matcher of Defaults.
func Default() *Matcher {
	m, err := New(Defaults)
	if err != nil {
		panic(err)
	}
	return m
}

// message is a quoted message or title argument, so "git commit -m 'make
// test'" is not a run; a quoted script (sh -c '...') still is. A message
// ends at its line, so a --body "$(cat <<'EOF'" keeps its heredoc.
var message = regexp.MustCompile(`(?:\s-[mt]|\s--(?:message|title|body|subject))(?:=|\s+)(?:'[^'\n]*'|"[^"\n]*")`)

var (
	heredoc = regexp.MustCompile(`<<-?\s*['"]?(\w+)['"]?`)
	split   = regexp.MustCompile("&&|\\|\\||[;|\n()]|\\$\\(|`")
	// wrap is what runs a command without being it: an assignment, timeout,
	// time, nice, env, exec, command, npx, nix develop -c, sh -c, a quote.
	wrap = regexp.MustCompile(`^(?:\w+=\S*\s+|timeout\s+(?:-\S+\s+)*\S+\s+|time\s+|nice\s+(?:-n\s*-?\d+\s+)?|env\s+|exec\s+|command\s+|npx\s+|nix\s+develop\s+(?:\S+\s+)*?-c\s+|(?:ba)?sh\s+-l?c\s+|['"])`)
)

// segments are the commands a shell line runs: heredoc bodies dropped,
// split at && || ; | ( ) $( ` and newlines, each without its wrappers.
func segments(cmd string) []string {
	var keep []string
	end := ""
	for _, ln := range strings.Split(cmd, "\n") {
		if end != "" {
			if strings.TrimSpace(ln) == end {
				end = ""
			}
			continue
		}
		if m := heredoc.FindStringSubmatch(ln); m != nil {
			end = m[1]
		}
		keep = append(keep, ln)
	}
	var out []string
	for _, seg := range split.Split(strings.Join(keep, "\n"), -1) {
		seg = strings.TrimSpace(seg)
		for {
			t := strings.TrimLeft(wrap.ReplaceAllString(seg, ""), " \t")
			if t == seg {
				break
			}
			seg = t
		}
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

// Class is the class of cmd's heavy command, the longest match of all
// patterns over the commands it runs, or "" when it runs none.
func (m *Matcher) Class(cmd string) string {
	if m == nil {
		return ""
	}
	cmd = message.ReplaceAllString(cmd, " ''")
	best, bestLen := "", 0
	for _, seg := range segments(cmd) {
		for _, r := range m.rules {
			loc := r.re.FindStringSubmatchIndex(seg)
			if loc == nil || loc[1]-loc[0] <= bestLen {
				continue
			}
			name := r.name
			if len(loc) >= 4 && loc[2] >= 0 {
				name = strings.ReplaceAll(name, "$1", seg[loc[2]:loc[3]])
			}
			best, bestLen = name, loc[1]-loc[0]
		}
	}
	return best
}

// KindOf is the slot kind for a class: e2e for browser and screenshot runs,
// build for builds, else test.
func KindOf(class string) string {
	c := strings.ToLower(class)
	switch {
	case strings.Contains(c, "e2e") || strings.Contains(c, "playwright") || strings.Contains(c, "screenshot") || strings.Contains(c, "layout") || strings.Contains(c, "chrome"):
		return "e2e"
	case strings.Contains(c, "build"):
		return "build"
	}
	return "test"
}
