// Package cli is what Fyr needs from an agent CLI (SPEC 0.5 V0.50, decision
// 0015): an Adapter launches a session process with a provider's auth and
// endpoint, speaks its protocol, says where its sessions live (handover
// rung 1) and what it can do. Adapters (claudecli, agycli) sit behind this
// package boundary as independent siblings, not special cases.
//
// The event stream a Session emits is Fyr's observation format: the
// stream-json messages of the claude CLI (system/init, assistant, user,
// result, rate_limit_event, …). Another adapter translates its own events
// into that shape.
package cli

import (
	"context"
	"errors"
	"io"
	"maps"
	"slices"

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

// Env is ModelEnv as KEY=VALUE, sorted by name: a launch's environment
// is the same for the same endpoint.
func (e Endpoint) Env() []string {
	out := make([]string, 0, len(e.ModelEnv))
	for _, k := range slices.Sorted(maps.Keys(e.ModelEnv)) {
		out = append(out, k+"="+e.ModelEnv[k])
	}
	return out
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
	// CredentialVars are the variables that name who the CLI talks to and
	// as whom. The adapter sets every one the provider needs (V0.50 (2));
	// a process must not inherit a stale one from whatever started fyrd,
	// which would point the agent at that endpoint (or that key) instead
	// (V0.agent-env, V0.review-hangs (1)). Every launch of the CLI (session,
	// print, model list, login) starts from launchenv.AgentEnv with them. A
	// profile's own env still sets them, so a legacy profile keeps working.
	CredentialVars []string `json:"-"`
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
	MCPConfigFile  string // a 0600 file with the JSON for Fyr's MCP server (V0.129); "" for none
	AllowedTools   []string
	// DisallowedTools are the CLI's own tools the session may not use
	// (V0.wakeups-via-watch: its timers, which die with the process).
	DisallowedTools []string
	Log             io.Writer
	// Cgroup: the thread's cgroup directory (V0.80); "" is the daemon's.
	Cgroup string
	// Wrap: the command line that starts the CLI in the thread's scope
	// (V0.agents-unit); nil for none.
	Wrap []string
	// Runner, when set, starts the process in the launcher's runner, so it
	// outlives fyrd (V0.90); ProcID names it there and Meta is fyrd's
	// note about it, given back after a restart.
	Runner *runner.Client
	ProcID string
	// RunnerKey: the runner keeps one live process per key (the thread id).
	RunnerKey string
	Meta      []byte
	// Result, when set, hears a Print run's result message, in the shape
	// the adapter's UsageReader reads (V0.usage-jobs); Print calls it before
	// it returns, also when the result is an error.
	Result func(Msg)
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

// Backgrounder is a Session whose CLI can move a running foreground tool
// call to the background without stopping it, so a message reaches the
// agent at once (V0.background-on-message). CanBackground: the CLI said it
// can; Background answers false when the CLI found no such call.
type Backgrounder interface {
	CanBackground() bool
	Background(ctx context.Context, toolUseID string) (bool, error)
}

// Reattacher is an adapter that can speak to a process the runner kept
// across a restart of fyrd (V0.90).
type Reattacher interface {
	Reattach(p runner.Pipe, log io.Writer, h Handlers) (Session, error)
}

// QueuedMessage is a user message waiting in an adapter's queue across a
// restart of fyrd (V0.90, V0.agy-reattach-queue).
type QueuedMessage struct {
	ID      string
	Content []Msg
}

// ReattachState is what fyrd knows about a thread's kept session when
// reattaching after a restart (V0.90, V0.agy-reattach-queue).
type ReattachState struct {
	SessionID string
	Model     string
	Running   bool
	Queued    []QueuedMessage
	// AllowedTools are the launch's AllowedTools: the tools the adapter
	// answers itself instead of asking. They are not part of the ACP
	// handshake, so an adapter that reattaches without them asks the owner
	// for calls the process had never asked about (V0.agy-reattach-allowlist).
	AllowedTools []string
	// DisallowedTools are the launch's DisallowedTools, likewise.
	DisallowedTools []string
	// MCPServers are the launch's MCPServers: an ACP adapter tells an eager
	// `mcp_<server>_<tool>` name apart from a flatter name by them, and
	// without them it guesses (V0.agy-reattach-allowlist).
	MCPServers []Msg
}

// SessionReattacher is a Reattacher that also takes the thread's known
// session state when reattaching to a kept process (e.g. ACP).
type SessionReattacher interface {
	ReattachSession(p runner.Pipe, state ReattachState, log io.Writer, h Handlers) (Session, error)
}

// Listing is what a CLI reported when asked for its models (V0.120): the
// catalog to keep, the ids the CLI itself named, and an optional note when
// the answer fell back to Fyr's known list.
type Listing struct {
	Models   []Model
	Reported []string
	Note     string
}

// ModelLister is an adapter that can ask its CLI which models it offers.
type ModelLister interface {
	ListModels(ctx context.Context, l Launch) (Listing, error)
}

// LoginSpec says how to run an interactive CLI login through Fyr (V0.53).
type LoginSpec struct {
	Binary    string   // CLI binary name when no test override is set
	Args      []string // command arguments
	Terminal  bool     // needs a pseudo-terminal
	Env       []string // full scrubbed process environment
	Throwaway bool     // run in a private temp folder and store the printed token
}

// LoginOut is what Fyr reads from a login process's output so far (V0.53).
type LoginOut struct {
	URL    string // sign-in link to show the owner
	Prompt bool   // waiting for pasted code
	Token  string // printed long-lived token, if any (Throwaway logins)
	Line   string // last redacted error line worth showing
	Retry  bool   // terminal waits for Enter to retry
}

// Loginer is an adapter that supports signing in through Fyr's Settings UI.
type Loginer interface {
	LoginSpec(base []string, configDir string, token bool) LoginSpec
	ParseLogin(buf []byte, done bool) LoginOut
	RedactLogin(s string) string
	LoginNeed(configDir string, token bool) string
}

// Defaults lets an adapter declare its binary name, default config folder
// (tilde form, e.g. "~/.claude" or "~/.gemini"), and fast system-agent model
// (e.g. Haiku for Claude, Flash for Antigravity).
type Defaults interface {
	DefaultBinary() string
	DefaultConfigDir() string
	FastModel(models []Model, modelEnv map[string]string, isEndpoint bool) string
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

// Printer is an adapter that answers one prompt with no tools and no saved
// session (V0.108: the title call). The prompt goes on stdin; system
// replaces the CLI's own system prompt.
type Printer interface {
	Print(ctx context.Context, l Launch, system, prompt string) (string, error)
}

// SessionArchiver is an adapter that can move a session to another host
// (cross-host handover rung 1, decision 0039): ExportSession packs session
// sid (its transcript src, under the sessions folder fromDir) into an archive
// of at most maxBytes; ImportSession unpacks one under toDir so a resume in
// targetCwd finds it. A session over maxBytes is ErrSessionTooLarge.
type SessionArchiver interface {
	ExportSession(src, fromDir, sid string, maxBytes int64) ([]byte, error)
	ImportSession(archive []byte, toDir, targetCwd, sid string, maxBytes int64) error
}

// ErrSessionTooLarge: a session archive exceeds the transfer cap; the
// handover falls back to rung 2 (decision 0039, V0.trans-host-handover).
var ErrSessionTooLarge = errors.New("session transcript is too large to transfer")

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

// Prober runs a one-shot request with the launch's appended prompt, MCP
// config and tool lists, and returns its result message, so Fyr can
// measure what its prompt and tools add to a request (V0.usage-overhead).
type Prober interface {
	Probe(ctx context.Context, l Launch, prompt string) (Msg, error)
}
