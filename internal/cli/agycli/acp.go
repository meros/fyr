package agycli

import (
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/launchenv"
	"github.com/meros/fyr/internal/runner"
)

type acpSpec struct {
	Binary          string
	Args            []string
	Dir             string
	Env             []string
	Log             io.Writer
	Cgroup          string
	Wrap            []string
	Runner          *runner.Client
	ID              string
	Key             string
	Meta            []byte
	SessionID       string
	Resume          bool
	Model           string
	Effort          string
	PermissionMode  string
	SystemPrompt    string
	AddDirs         []string
	MCPServers      []cli.Msg
	AllowedTools    []string
	DisallowedTools []string
	Cleanup         func()
	ReattachRunning bool
	ReattachQueued  []cli.QueuedMessage
	SettleDelay     time.Duration
}

type acpSession struct {
	// Link writes and logs lines and keeps the runner's ack state (V0.90),
	// as for the claude client.
	*runner.Link
	spec    acpSpec
	h       cli.Handlers
	cleanup func()

	pmu     sync.Mutex
	pending map[string]pendingReq
	seq     atomic.Int64

	// Session + turn + prompt queue state.
	// Lock order: emitMu -> mu (never mu -> emitMu).
	emitMu          sync.Mutex
	mu              sync.Mutex
	sessionID       string
	currentModel    string
	currentEffort   string
	configOptions   []cli.Msg
	availableModels []availModel
	availableModes  map[string]bool
	commands        []cli.Msg
	mcpNames        []string
	initEmitted     bool
	turnRunning     bool
	queue           []queuedPrompt
	inputClosed     bool
	killed          bool
	textBuf         strings.Builder
	turnText        strings.Builder
	emittedTools    map[string]bool
	finishedTools   map[string]bool
	rawNames        map[string]string
	toolNames       map[string]string
	toolDescs       map[string]bgTool
	bgTools         map[string]bgTool
	openPerms       map[string]bool
	reattachActive  bool

	// Unsolicited (reactive-wakeup) turn tracking and settle timer state
	// (V0.agy-reactive-wakeup).
	unsolicitedActive      bool
	unsolicitedInterrupted bool
	settleDelay            time.Duration
	settleArmed            bool
	settleGen              uint64
	settleDeadline         time.Time

	qCh        chan struct{}
	readDone   chan struct{}
	workerDone chan struct{}
	out        chan cli.Msg
	done       chan struct{}
	err        error
}

func startACP(s acpSpec, h cli.Handlers) (*acpSession, error) {
	env := launchenv.AgentEnv(CredentialVars, s.Env...) // #167, V0.review-hangs (1)
	p, err := runner.Start(s.Runner, runner.Spawn{ID: s.ID, Key: s.Key, Binary: s.Binary, Args: s.Args, Dir: s.Dir,
		Env: env, Cgroup: s.Cgroup, Meta: s.Meta}, s.Wrap)
	if err != nil {
		return nil, err
	}
	return openACP(p, s, h, false), nil
}

func openACP(p runner.Pipe, s acpSpec, h cli.Handlers, reattached bool) *acpSession {
	var mcpNames []string
	for _, m := range s.MCPServers {
		if n, _ := m["name"].(string); n != "" {
			mcpNames = append(mcpNames, n)
		}
	}
	var queue []queuedPrompt
	for _, q := range s.ReattachQueued {
		queue = append(queue, queuedPrompt{uuid: q.ID, content: q.Content})
	}
	settleDelay := s.SettleDelay
	if settleDelay <= 0 {
		settleDelay = defaultUnsolicitedSettle
	}
	c := &acpSession{
		Link:           runner.NewLink(p, s.Log),
		spec:           s,
		h:              h,
		cleanup:        s.Cleanup,
		pending:        map[string]pendingReq{},
		sessionID:      s.SessionID,
		currentModel:   s.Model,
		currentEffort:  s.Effort,
		mcpNames:       mcpNames,
		initEmitted:    reattached && s.SessionID != "",
		queue:          queue,
		emittedTools:   map[string]bool{},
		finishedTools:  map[string]bool{},
		rawNames:       map[string]string{},
		toolNames:      map[string]string{},
		toolDescs:      map[string]bgTool{},
		bgTools:        map[string]bgTool{},
		openPerms:      map[string]bool{},
		settleDelay:    settleDelay,
		reattachActive: reattached && s.ReattachRunning,
		qCh:            make(chan struct{}, 1),
		readDone:       make(chan struct{}),
		workerDone:     make(chan struct{}),
		out:            make(chan cli.Msg, 256),
		done:           make(chan struct{}),
	}
	if reattached {
		// Avoid colliding with any in-flight pre-restart JSON-RPC request IDs.
		c.seq.Store(1_000_000)
	}
	go c.promptWorker()
	go c.read()
	return c
}

func (c *acpSession) Out() <-chan cli.Msg   { return c.out }
func (c *acpSession) Done() <-chan struct{} { return c.done }
func (c *acpSession) Err() error            { return c.err }

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
