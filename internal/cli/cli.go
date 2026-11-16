// Package cli is what Fyr needs from an agent CLI (SPEC 0.5 V0.50, decision
// 0015): an Adapter launches a session process with a provider's auth and
// endpoint, speaks its protocol, says where its sessions live (handover
// rung 1) and what it can do. The claude adapter (package claudecli) is the
// only one today; another CLI is a new Adapter, not a special case.
//
// The event stream a Session emits is Fyr's observation format: the
// stream-json messages of the claude CLI (system/init, assistant, user,
// result, rate_limit_event, …). Another adapter translates its own events
// into that shape.
package cli

import (
	"context"
	"io"

	"github.com/meros/fyr/internal/runner"
)

// Msg is one protocol message.
type Msg = map[string]any

// AuthKind is how a provider proves who it is.
type AuthKind string

const (
	// CLILogin: the CLI keeps its own login in a config folder.
	CLILogin AuthKind = "cli-login"
	// APIKey: a key from Fyr's secret store.
	APIKey AuthKind = "api-key"
	// NoAuth: nothing to apply.
	NoAuth AuthKind = "none"
)

// Auth is data the adapter applies; it never holds a secret value.
type Auth struct {
	Kind AuthKind `json:"kind"`
	// ConfigDir: where the CLI keeps its login and sessions; "" is the
	// CLI's default (~/.claude). cli-login, optional for api-key.
	ConfigDir string `json:"configDir,omitempty"`
	// Secret: the name in the secret store (api-key; a cli-login may carry
	// a login token instead of a stored login).
	Secret string `json:"secret,omitempty"`
}

// Endpoint is where an api-key provider sends its requests.
type Endpoint struct {
	BaseURL string `json:"baseURL,omitempty"`
	// ModelEnv: non-secret variables naming models for the CLI's internal
	// roles, e.g. ANTHROPIC_DEFAULT_HAIKU_MODEL.
	ModelEnv map[string]string `json:"modelEnv,omitempty"`
}

// Model is one model a provider offers, with what it supports.
type Model struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// Efforts are the effort levels the model takes, empty when it has none.
	Efforts []string `json:"efforts,omitempty"`
}

// Caps is what an adapter can do.
type Caps struct {
	Auth []AuthKind `json:"auth"`
	// Efforts the CLI takes at all (a model narrows them).
	Efforts []string `json:"efforts,omitempty"`
	// Models: the CLI's own list for a login (an endpoint lists its own).
	Models []Model `json:"models,omitempty"`
}

// Launch is everything one session process starts with. Key is the
// resolved secret; it goes only into the process environment.
type Launch struct {
	Binary   string // override of the adapter's binary (tests); "" is the default
	Dir      string
	Auth     Auth
	Key      string
	Endpoint Endpoint
	Env      []string // extra KEY=VALUE (legacy profiles)

	Model, Effort  string
	Session        string
	Resume         bool
	AddDirs        []string
	PermissionMode string
	SystemPrompt   string // appended
	MCPConfig      string // JSON for Fyr's MCP server; "" for none
	AllowedTools   []string
	Log            io.Writer
	// Cgroup: the thread's cgroup directory (V0.80); "" is the daemon's.
	Cgroup string
	// Runner, when set, starts the process in the launcher's runner, so it
	// outlives fyrd (V0.90); ProcID names it there and Meta is fyrd's
	// note about it, given back after a restart.
	Runner *runner.Client
	ProcID string
	// RunnerKey: the runner keeps one live process per key (the thread id).
	RunnerKey string
	Meta      []byte
}

// Handlers answer the CLI's callbacks; they run on their own goroutines.
type Handlers struct {
	CanUseTool func(req Msg) Msg
	Cancel     func(requestID string)
	Hook       func(callbackID string, input Msg) Msg
}

// Session is one running CLI process.
type Session interface {
	Out() <-chan Msg // closes when the process output ends
	Done() <-chan struct{}
	Err() error
	Initialize(ctx context.Context, hooks Msg) (Msg, error)
	SendUser(content []Msg, uuid string) error
	Interrupt(ctx context.Context) error
	SetModel(ctx context.Context, model string) error
	CancelMessage(ctx context.Context, uuid string) (bool, error)
	Close() error
	Kill()
}

// Reattacher is an adapter that can speak to a process the runner kept
// across a restart of fyrd (V0.90).
type Reattacher interface {
	Reattach(p runner.Pipe, log io.Writer, h Handlers) (Session, error)
}

// Adapter is one agent CLI.
type Adapter interface {
	ID() string
	Caps() Caps
	// Env is the environment the auth and endpoint add (pure; tested).
	Env(l Launch) ([]string, error)
	Start(l Launch, h Handlers) (Session, error)
	// SessionsDir is where the CLI keeps sessions for this auth.
	SessionsDir(a Auth) string
	// SessionFile finds a session's transcript in a sessions dir, or "".
	SessionFile(dir, cwd, session string) string
	// LoggedIn reports whether the CLI has its own login for this auth
	// (it checks that the login exists; it never reads a token).
	LoggedIn(a Auth) bool
	// CopySession copies a session between two sessions dirs.
	CopySession(src, fromDir, toDir, session string) error
}

// Accepts reports whether the adapter takes an auth kind.
func Accepts(a Adapter, k AuthKind) bool {
	for _, x := range a.Caps().Auth {
		if x == k {
			return true
		}
	}
	return false
}

// EffortFor is the effort to pass for a model: the asked one when the
// model takes it, else the first default that it takes, else none. A
// model the list does not know takes no effort unless one is asked.
func EffortFor(models []Model, model, asked string, defaults ...string) string {
	for _, m := range models {
		if m.ID != model {
			continue
		}
		for _, want := range append([]string{asked}, defaults...) {
			for _, e := range m.Efforts {
				if want != "" && e == want {
					return e
				}
			}
		}
		return ""
	}
	return asked
}
