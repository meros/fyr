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
	"sync"
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
	soft bool
}

// Matcher names the class of a command. V0.164: the owner's added patterns
// (SetAdded) are read after the others, and may change while it runs.
type Matcher struct {
	rules []rule

	mu      sync.RWMutex
	added   []rule
	refused map[string]int
}

// SetAdded replaces the added patterns (V0.164) and the ones Fyr learned
// (V0.209: soft, a full host never refuses them); a pattern that does not
// compile is an error and changes nothing.
func (m *Matcher) SetAdded(ps []Pattern, learned ...Pattern) error {
	var rs []rule
	add := func(ps []Pattern, soft bool) error {
		for _, p := range ps {
			re, err := regexp.Compile(`^(?:` + p.Re + `)`)
			if err != nil {
				return fmt.Errorf("heavy command %q: %w", p.Name, err)
			}
			rs = append(rs, rule{p.Name, re, soft})
		}
		return nil
	}
	if err := add(ps, false); err != nil {
		return err
	}
	if err := add(learned, true); err != nil {
		return err
	}
	m.mu.Lock()
	m.added = rs
	m.mu.Unlock()
	return nil
}

// Soft says class comes from a pattern Fyr learned (V0.209): it is admitted
// when there is room and otherwise runs anyway.
func (m *Matcher) Soft(class string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.added {
		if r.soft && r.name == class {
			return true
		}
	}
	return false
}

// Refused counts a refusal of class by the gate (V0.164: shown with an added
// pattern); a class that is not an added pattern's is ignored.
func (m *Matcher) Refused(class string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.added {
		if r.name == class {
			if m.refused == nil {
				m.refused = map[string]int{}
			}
			m.refused[class]++
		}
	}
}

// RefusedCount is how often the gate refused class since this start.
func (m *Matcher) RefusedCount(class string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.refused[class]
}

// New compiles ps; a pattern that does not compile is an error.
func New(ps []Pattern) (*Matcher, error) {
	m := &Matcher{}
	for _, p := range ps {
		re, err := regexp.Compile(`^(?:` + p.Re + `)`)
		if err != nil {
			return nil, fmt.Errorf("heavy command %q: %w", p.Name, err)
		}
		m.rules = append(m.rules, rule{p.Name, re, false})
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
	// time, nice, ionice (V0.freeze-fast-class-name), env, exec, command, npx,
	// nix develop -c, sh -c, a quote.
	wrap = regexp.MustCompile(`^(?:\w+=\S*\s+|timeout\s+(?:-\S+\s+)*\S+\s+|time\s+|nice\s+(?:-n\s*-?\d+\s+|-\d+\s+)?|ionice\s+(?:-[cnpt]\s*\d*\s+)*|env\s+|exec\s+|command\s+|npx\s+|nix\s+develop\s+(?:\S+\s+)*?-c\s+|(?:ba)?sh\s+-l?c\s+|['"])`)
)

// script is the "-c " (sh -c, bash -lc, nix develop -c) that makes the
// single-quoted text after it a command line of its own.
var script = regexp.MustCompile(`(?:^|\s)-l?c\s*$`)

// blankQuoted replaces the text inside single quotes with spaces, except a
// script after -c: the shell runs nothing in single quotes, so a `|` or a
// backtick there starts no command (V0.heavy-quoted-args). On 2026-11-19 a sed edit of a PR body,
// 's/- `make check SHELL_RUN=`: …/', was queued as a heavy job.
func blankQuoted(cmd string) string {
	b := []byte(cmd)
	double := false
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '\\':
			i++ // an escaped character is no quote
		case b[i] == '"':
			double = !double
		case b[i] == '\'' && !double:
			end := strings.IndexByte(cmd[i+1:], '\'')
			if end < 0 {
				return string(b)
			}
			end += i + 1
			if !script.MatchString(cmd[:i]) {
				for j := i + 1; j < end; j++ {
					if b[j] != '\n' {
						b[j] = ' '
					}
				}
			}
			i = end
		}
	}
	return string(b)
}

// segments are the commands a shell line runs: heredoc bodies dropped,
// single-quoted arguments blanked (blankQuoted), split at && || ; | ( ) $(
// ` and newlines, each without its wrappers.
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
	for _, seg := range split.Split(blankQuoted(strings.Join(keep, "\n")), -1) {
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
	c, _ := m.Match(cmd)
	return c
}

// Match is Class and the matched text as cmd has it (decision 0029 S5: the
// job's processes are found by it in their command lines).
func (m *Matcher) Match(cmd string) (class, text string) {
	if m == nil {
		return "", ""
	}
	cmd = message.ReplaceAllString(cmd, " ''")
	best, bestLen, bestText := "", 0, ""
	m.mu.RLock()
	all := append(append([]rule(nil), m.rules...), m.added...)
	m.mu.RUnlock()
	for _, seg := range segments(cmd) {
		for _, r := range all {
			loc := r.re.FindStringSubmatchIndex(seg)
			if loc == nil || loc[1]-loc[0] <= bestLen {
				continue
			}
			name := r.name
			if len(loc) >= 4 && loc[2] >= 0 {
				name = strings.ReplaceAll(name, "$1", seg[loc[2]:loc[3]])
			}
			best, bestLen, bestText = name, loc[1]-loc[0], seg[loc[0]:loc[1]]
		}
	}
	return best, bestText
}

var detached = regexp.MustCompile(`(?:^|[\s;&|(])(?:nohup|setsid|disown)\b|[^&]&\s*(?:$|[\n;)])`)

// Detached says cmd leaves work running after the shell returns (V0.209), so
// the end of the tool call is not the end of the job.
func Detached(cmd string) bool {
	return detached.MatchString(message.ReplaceAllString(cmd, " ''"))
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

// Runners that start a worker per CPU of the host and read no variable for it
// (V0.job-limits): Playwright's test runner (`--workers`, `-j`) and turbo
// (`--concurrency`, default 10).
var (
	playwrightRun = regexp.MustCompile(`(^|[\s;&|(])(npx\s+|yarn\s+|pnpm\s+(exec\s+)?|bunx\s+)?playwright\s+test\b`)
	playwrightSet = regexp.MustCompile(`(\s--workers[=\s]|\s-j\s*\d)`)
	turboRun      = regexp.MustCompile(`(^|[\s;&|(])(npx\s+|yarn\s+|pnpm\s+(exec\s+)?|bunx\s+)?turbo\s+(run\s+)?[\w:-]`)
	turboSet      = regexp.MustCompile(`\s--concurrency[=\s]`)
)

// WorkerHint is the gate hook's notice for a command whose runner sizes its
// workers from the host's CPUs and ignores the job's cgroup: the flag to add,
// with workers as its value. "" when the command sets it or runs neither.
func WorkerHint(command string, workers int) string {
	var flags []string
	if playwrightRun.MatchString(command) && !playwrightSet.MatchString(command) {
		flags = append(flags, fmt.Sprintf("`--workers=%d` to `playwright test`", workers))
	}
	if turboRun.MatchString(command) && !turboSet.MatchString(command) {
		flags = append(flags, fmt.Sprintf("`--concurrency=%d` to `turbo`", workers))
	}
	if len(flags) == 0 {
		return ""
	}
	return "[Fyr] This runner starts a worker per CPU of the host and does not see the share this thread's job gets. " +
		"Next time add " + strings.Join(flags, " and ") + " (the share of this host's agents' cores Fyr gives a job)."
}
