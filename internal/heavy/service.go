package heavy

// V0.services-not-jobs (decision 0036 (D)): a service is a command that keeps
// running for the work beside it (a dev stack, a dev server, a compose project)
// rather than a job that ends. Fyr reserves its memory, never freezes it and
// never names it in a turn. The built-in patterns can be removed and others
// added in Settings → Automation; the list is kept in the store's KV table.

import (
	"encoding/json"
	"errors"
	"github.com/meros/fyr/internal/kvstore"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

// DefaultServices are the built-in service patterns, matched against a Bash
// command and against a process's command line.
var DefaultServices = []string{
	`(^|[\s/])process-compose(\s+up\b|\s+-f\b|\s+--config\b|\s+-t\b|\s+--tui\b|\s*$)`,
	`(^|[\s/])docker\s+compose\b.*\sup\b`,
	`(^|[\s/])docker-compose\b.*\sup\b`,
	`(^|[\s/])(npm|pnpm|bun)\s+run\s+dev\b`,
	`(^|[\s/])(yarn|pnpm)\s+dev\b`,
	`(^|[\s/])yarn\s+run\s+dev\b`,
	`(^|[\s/])vite(\s+(dev|serve|preview)\b|\s+--|\s*$)`,
}

const servicesKey = "service-patterns"

// ServicePattern is one pattern as Settings shows it.
type ServicePattern struct {
	Pattern string `json:"pattern"`
	BuiltIn bool   `json:"builtIn,omitempty"`
}

// Services is the host's service patterns: the built-in ones less those the
// owner removed, then the config's and the owner's own. Match never waits.
type Services struct {
	KV   kvstore.Table
	Base []string // the config's (`servicePatterns`)

	mu      sync.Mutex
	added   []string
	removed []string
	cur     atomic.Pointer[serviceSet]
}

type serviceSet struct {
	list []ServicePattern
	res  []*regexp.Regexp
}

type servicesStored struct {
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// Load reads what the owner saved; call it at start.
func (s *Services) Load() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.KV != nil {
		if raw, _ := s.KV.GetKV(servicesKey); raw != "" {
			var v servicesStored
			if json.Unmarshal([]byte(raw), &v) == nil {
				s.added, s.removed = v.Added, v.Removed
			}
		}
	}
	s.applyLocked()
}

func (s *Services) applyLocked() {
	set := &serviceSet{}
	add := func(p string, builtIn bool) {
		if slices.ContainsFunc(set.list, func(x ServicePattern) bool { return x.Pattern == p }) {
			return
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return
		}
		set.list = append(set.list, ServicePattern{Pattern: p, BuiltIn: builtIn})
		set.res = append(set.res, re)
	}
	for _, p := range DefaultServices {
		if !slices.Contains(s.removed, p) {
			add(p, true)
		}
	}
	for _, p := range append(slices.Clone(s.Base), s.added...) {
		add(p, false)
	}
	s.cur.Store(set)
}

func (s *Services) set() *serviceSet {
	if c := s.cur.Load(); c != nil {
		return c
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.cur.Load(); c != nil {
		return c
	}
	s.applyLocked()
	return s.cur.Load()
}

// Match says whether command (a Bash command or a process's command line) is a service.
func (s *Services) Match(command string) bool {
	if s == nil {
		return false
	}
	for _, re := range s.set().res {
		if re.MatchString(command) {
			return true
		}
	}
	return false
}

// List is the patterns in force.
func (s *Services) List() []ServicePattern {
	if s == nil {
		return nil
	}
	return slices.Clone(s.set().list)
}

// Edit applies the owner's change (host.servicePatterns): add a pattern, or
// remove one (a built-in one stays removed until it is added again).
func (s *Services) Edit(add, remove string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if add = strings.TrimSpace(add); add != "" {
		if _, err := regexp.Compile(add); err != nil {
			return errors.New("not a valid pattern: " + err.Error())
		}
		if i := slices.Index(s.removed, add); i >= 0 {
			s.removed = slices.Delete(s.removed, i, i+1)
		} else if !slices.Contains(s.added, add) && !slices.Contains(DefaultServices, add) {
			s.added = append(s.added, add)
		}
	}
	if remove = strings.TrimSpace(remove); remove != "" {
		switch i := slices.Index(s.added, remove); {
		case i >= 0:
			s.added = slices.Delete(s.added, i, i+1)
		case slices.Contains(DefaultServices, remove) && !slices.Contains(s.removed, remove):
			s.removed = append(s.removed, remove)
		default:
			return errors.New("no such service pattern")
		}
	}
	if s.KV != nil {
		raw, _ := json.Marshal(servicesStored{Added: s.added, Removed: s.removed})
		if err := s.KV.SetKV(servicesKey, string(raw)); err != nil {
			return err
		}
	}
	s.applyLocked()
	return nil
}

var (
	composeCmd  = regexp.MustCompile(`(^|[\s/;&|(])(docker\s+compose|docker-compose)\b`)
	composeDown = regexp.MustCompile(`\s(down|stop|kill|rm)\b`)
	composeUp   = regexp.MustCompile(`\sup\b`)
	unsafeName  = regexp.MustCompile(`[^a-z0-9_-]`)
)

// ComposeProject is the compose project a command names, the way compose
// itself names it: -p/--project-name, else COMPOSE_PROJECT_NAME in the
// command, else the directory of --project-directory or of the first -f file
// (relative to cwd), else cwd's name. up says the command starts it (`up`),
// down that it stops it (`down`, `stop`, `kill`, `rm`). "" when the command
// runs no compose.
func ComposeProject(command, cwd string) (project string, up, down bool) {
	loc := composeCmd.FindStringIndex(command)
	if loc == nil {
		return "", false, false
	}
	pre, rest := command[:loc[0]], command[loc[1]:]
	// The compose command ends at the next shell separator.
	if i := strings.IndexAny(rest, ";&|\n"); i >= 0 {
		rest = rest[:i]
	}
	args := strings.Fields(rest)
	arg := func(i int) string {
		if i < len(args) {
			return strings.Trim(args[i], `"'`)
		}
		return ""
	}
	var name, dir, file string
	for i := 0; i < len(args); i++ {
		a := args[i]
		k, v, eq := strings.Cut(a, "=")
		val := func() string {
			if eq {
				return strings.Trim(v, `"'`)
			}
			i++
			return arg(i)
		}
		switch k {
		case "-p", "--project-name":
			name = val()
		case "--project-directory":
			dir = val()
		case "-f", "--file":
			if f := val(); file == "" {
				file = f
			}
		}
	}
	if name == "" {
		for _, f := range strings.Fields(pre) {
			if v, ok := strings.CutPrefix(f, "COMPOSE_PROJECT_NAME="); ok {
				name = strings.Trim(v, `"'`)
			}
		}
	}
	if name == "" {
		switch {
		case dir != "":
			name = filepath.Base(join(cwd, dir))
		case file != "":
			name = filepath.Base(filepath.Dir(join(cwd, file)))
		case cwd != "":
			name = filepath.Base(cwd)
		}
	}
	name = unsafeName.ReplaceAllString(strings.ToLower(name), "")
	if name == "" || name == "." || name == "/" {
		return "", false, false
	}
	return name, composeUp.MatchString(" " + rest), composeDown.MatchString(" " + rest)
}

func join(cwd, p string) string {
	if filepath.IsAbs(p) || cwd == "" {
		return filepath.Clean(p)
	}
	return filepath.Join(cwd, p)
}
