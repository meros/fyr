package agycli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/launchenv"
	"github.com/meros/fyr/internal/runner"
)

type acpToolStatus string

const (
	toolStatusPending    acpToolStatus = "pending"
	toolStatusInProgress acpToolStatus = "in_progress"
	toolStatusCompleted  acpToolStatus = "completed"
	toolStatusFailed     acpToolStatus = "failed"
	toolStatusCancelled  acpToolStatus = "cancelled"
)

type acpPermKind string

const (
	permAllowOnce    acpPermKind = "allow_once"
	permAllowAlways  acpPermKind = "allow_always"
	permRejectOnce   acpPermKind = "reject_once"
	permRejectAlways acpPermKind = "reject_always"
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

// defaultUnsolicitedSettle is how long an unsolicited (reactive-wakeup) ACP
// turn waits after a text step's usage_update before flushing buffered text
// and closing the turn with a synthetic result (V0.agy-reactive-wakeup).
// Live agy sessions emit any subsequent tool_call or JSON-RPC stopReason
// response within ~250ms of usage_update; 1.5s leaves generous headroom while
// settling reactive wakeups promptly.
const defaultUnsolicitedSettle = 1500 * time.Millisecond

type bgTool struct {
	id       string
	taskType string
	desc     string
}

type queuedPrompt struct {
	uuid    string
	content []cli.Msg
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("acp error %d: %s", e.Code, e.Message)
}

type rpcResp struct {
	result cli.Msg
	err    *rpcError
	seq    int64
}

type pendingReq struct {
	ch      chan rpcResp
	holdSeq bool
}

type acpSession struct {
	pipe    runner.Pipe
	spec    acpSpec
	h       cli.Handlers
	log     io.Writer
	cleanup func()

	wmu sync.Mutex

	// Runner sequence ack state (V0.90), matching internal/claude.
	amu      sync.Mutex
	unacked  []int64
	open     map[string]int64
	seen     int64
	lastAck  int64
	lastKeep []int64

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
	bin, args := s.Binary, s.Args
	if len(s.Wrap) > 0 {
		args = append(append(append([]string{}, s.Wrap[1:]...), bin), args...)
		bin = s.Wrap[0]
	}
	var p runner.Pipe
	var err error
	if s.Runner != nil {
		p, err = s.Runner.Spawn(runner.Spawn{
			ID: s.ID, Key: s.Key, Binary: bin, Args: args, Dir: s.Dir, Env: env, Cgroup: s.Cgroup, Meta: s.Meta,
		})
	} else {
		p, err = startChild(bin, args, s.Dir, env, s.Cgroup)
	}
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
		pipe:           p,
		spec:           s,
		h:              h,
		log:            s.Log,
		cleanup:        s.Cleanup,
		open:           map[string]int64{},
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
func (c *acpSession) Pid() int              { return c.pipe.Pid() }
func (c *acpSession) Pipe() runner.Pipe     { return c.pipe }

func (c *acpSession) Handled(m cli.Msg) {
	seq, ok := m[claude.SeqKey].(int64)
	if !ok {
		return
	}
	c.amu.Lock()
	for i, x := range c.unacked {
		if x == seq {
			c.unacked = append(c.unacked[:i], c.unacked[i+1:]...)
			break
		}
	}
	c.amu.Unlock()
	c.ack()
}

func (c *acpSession) note(seq int64, queued bool) {
	if seq == 0 {
		return
	}
	c.amu.Lock()
	c.seen = seq
	if queued {
		c.unacked = append(c.unacked, seq)
	}
	c.amu.Unlock()
}

func (c *acpSession) ack() {
	c.amu.Lock()
	upTo := c.seen
	if len(c.unacked) > 0 {
		upTo = c.unacked[0] - 1
	}
	var keep []int64
	for _, seq := range c.open {
		if seq <= upTo {
			keep = append(keep, seq)
		}
	}
	sort.Slice(keep, func(i, j int) bool { return keep[i] < keep[j] })
	same := upTo == c.lastAck && slices.Equal(keep, c.lastKeep)
	c.lastAck, c.lastKeep = upTo, keep
	c.amu.Unlock()
	if !same {
		c.pipe.Ack(upTo, keep)
	}
}

func (c *acpSession) opened(id string, seq int64) {
	if seq == 0 || id == "" {
		return
	}
	c.amu.Lock()
	c.open[id] = seq
	c.amu.Unlock()
}

func (c *acpSession) answered(id string) {
	c.amu.Lock()
	_, ok := c.open[id]
	delete(c.open, id)
	c.amu.Unlock()
	if ok {
		c.ack()
	}
}

func (c *acpSession) logf(dir string, line []byte) {
	if c.log != nil {
		fmt.Fprintf(c.log, "%s %s %s\n", time.Now().Format("15:04:05.000"), dir, line)
	}
}

func (c *acpSession) write(m cli.Msg) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.logf(">>", b)
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.pipe.WriteLine(b)
}

func (c *acpSession) emit(m cli.Msg, seq int64) {
	if seq > 0 {
		m[claude.SeqKey] = seq
		c.note(seq, true)
	}
	c.out <- m
}

// emitHeld emits a message whose runner seq was already recorded in c.unacked
// by read() so the runner keeps it until Fyr handles the message (V0.agy-reattach-queue).
func (c *acpSession) emitHeld(m cli.Msg, seq int64) {
	if seq > 0 {
		m[claude.SeqKey] = seq
	}
	c.out <- m
}

func (c *acpSession) request(ctx context.Context, method string, params cli.Msg) (cli.Msg, error) {
	res, _, err := c.requestOn(ctx, method, params, false, nil)
	return res, err
}

// requestOn is request, with onWritten run once the request is on the agent's
// pipe and before its answer is waited for: a turn is announced "started"
// there, so an Interrupt written after that announcement reaches the agent
// after the prompt it cancels (V0.agy-interrupt-before-prompt). When holdSeq is
// true, read() records the response's runner seq in c.unacked and returns it so
// the caller can attach it to the emitted result message (V0.agy-reattach-queue).
func (c *acpSession) requestOn(ctx context.Context, method string, params cli.Msg, holdSeq bool, onWritten func()) (cli.Msg, int64, error) {
	idNum := c.seq.Add(1)
	idKey := strconv.FormatInt(idNum, 10)
	ch := make(chan rpcResp, 1)
	c.pmu.Lock()
	c.pending[idKey] = pendingReq{ch: ch, holdSeq: holdSeq}
	c.pmu.Unlock()
	defer func() {
		c.pmu.Lock()
		delete(c.pending, idKey)
		c.pmu.Unlock()
	}()
	req := cli.Msg{"jsonrpc": "2.0", "id": idNum, "method": method}
	if params != nil {
		req["params"] = params
	}
	if err := c.write(req); err != nil {
		return nil, 0, err
	}
	if onWritten != nil {
		onWritten()
	}
	select {
	case r, open := <-ch:
		if !open {
			return nil, 0, fmt.Errorf("process exited before %s was answered", method)
		}
		if r.err != nil {
			return nil, r.seq, r.err
		}
		return r.result, r.seq, nil
	case <-c.readDone:
		return nil, 0, fmt.Errorf("process exited before %s was answered", method)
	case <-ctx.Done():
		return nil, 0, fmt.Errorf("%s: %w", method, ctx.Err())
	}
}

func (c *acpSession) notify(method string, params cli.Msg) error {
	m := cli.Msg{"jsonrpc": "2.0", "method": method}
	if params != nil {
		m["params"] = params
	}
	return c.write(m)
}

// Initialize performs the ACP handshake (`initialize`), opens or resumes the
// session (`session/new` or `session/resume`), applies the requested model,
// effort, and permission mode, and returns Fyr's initialize answer with
// discovered commands and models.
func (c *acpSession) Initialize(ctx context.Context, hooks cli.Msg) (cli.Msg, error) {
	if _, err := c.request(ctx, "initialize", cli.Msg{
		"protocolVersion":    1,
		"clientCapabilities": cli.Msg{},
	}); err != nil {
		return nil, err
	}
	mcpServers := c.spec.MCPServers
	if mcpServers == nil {
		mcpServers = []cli.Msg{}
	}
	params := cli.Msg{
		"cwd":        c.spec.Dir,
		"mcpServers": mcpServers,
	}
	if len(c.spec.AddDirs) > 0 {
		params["additionalDirectories"] = c.spec.AddDirs
	}
	if c.spec.SystemPrompt != "" {
		params["_meta"] = cli.Msg{
			"antigravity": cli.Msg{
				"systemPrompt": cli.Msg{"append": c.spec.SystemPrompt},
			},
		}
	}
	method := "session/new"
	if c.spec.Resume && c.spec.SessionID != "" {
		method = "session/resume"
		params["sessionId"] = c.spec.SessionID
	}
	res, err := c.request(ctx, method, params)
	if err != nil {
		return nil, err
	}
	sid, _ := res["sessionId"].(string)
	if sid == "" {
		sid = c.spec.SessionID
	}
	if sid == "" {
		return nil, fmt.Errorf("%s returned no sessionId", method)
	}
	models := ParseACPModels(res)
	c.mu.Lock()
	c.sessionID = sid
	c.availableModels = parseAvailableModels(res)
	c.updateConfigOptionsLocked(res["configOptions"])
	c.availableModes = parseAvailableModes(res)
	if mObj, _ := res["models"].(map[string]any); mObj != nil {
		if cur, _ := mObj["currentModelId"].(string); cur != "" && c.currentModel == "" {
			c.currentModel = cur
		}
	}
	c.mu.Unlock()

	if c.spec.Model != "" || c.spec.Effort != "" {
		if err := c.applyModelAndEffort(ctx, c.spec.Model, c.spec.Effort); err != nil {
			return nil, err
		}
	}
	if mode := mapPermissionMode(c.spec.PermissionMode); mode != "" {
		c.mu.Lock()
		modeOK := len(c.availableModes) == 0 || c.availableModes[mode]
		c.mu.Unlock()
		if modeOK {
			// Best-effort: some account tiers (e.g. enterprise) disallow `auto`.
			_, _ = c.request(ctx, "session/set_mode", cli.Msg{"sessionId": sid, "modeId": mode})
		}
	}

	c.mu.Lock()
	cmds := append([]cli.Msg(nil), c.commands...)
	c.mu.Unlock()
	resp := cli.Msg{"session_id": sid, "models": models}
	if len(cmds) > 0 {
		raw := make([]any, len(cmds))
		for i, x := range cmds {
			raw[i] = x
		}
		resp["commands"] = raw
	}
	return cli.Msg{"subtype": "success", "response": resp}, nil
}

func (c *acpSession) applyModelAndEffort(ctx context.Context, model, effort string) error {
	c.mu.Lock()
	sid := c.sessionID
	hasModelOpt := c.hasConfigValueLocked("model", model)
	avail := append([]availModel(nil), c.availableModels...)
	c.mu.Unlock()

	if model != "" {
		if hasModelOpt {
			res, err := c.request(ctx, "session/set_config_option", cli.Msg{
				"sessionId": sid, "configId": "model", "value": model,
			})
			if err != nil {
				return err
			}
			c.mu.Lock()
			c.currentModel = model
			c.updateConfigOptionsLocked(res["configOptions"])
			c.mu.Unlock()
		} else {
			target := model
			if effort != "" {
				cand := model + "-" + effort
				for _, a := range avail {
					if a.id == cand {
						target = cand
						effort = "" // already encoded in modelId
						break
					}
				}
			}
			if _, err := c.request(ctx, "session/set_model", cli.Msg{
				"sessionId": sid, "modelId": target,
			}); err != nil {
				return err
			}
			c.mu.Lock()
			c.currentModel = target
			c.mu.Unlock()
		}
	}
	if effort != "" {
		c.mu.Lock()
		hasEff := c.hasConfigValueLocked("thought_level", effort)
		c.mu.Unlock()
		if hasEff {
			res, err := c.request(ctx, "session/set_config_option", cli.Msg{
				"sessionId": sid, "configId": "thought_level", "value": effort,
			})
			if err != nil {
				return err
			}
			c.mu.Lock()
			c.currentEffort = effort
			c.updateConfigOptionsLocked(res["configOptions"])
			c.mu.Unlock()
		}
	}
	return nil
}

func (c *acpSession) updateConfigOptionsLocked(raw any) {
	opts, _ := raw.([]any)
	if len(opts) == 0 {
		return
	}
	out := make([]cli.Msg, 0, len(opts))
	for _, x := range opts {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
			if id, _ := m["id"].(string); id == "model" {
				if cur, _ := m["currentValue"].(string); cur != "" {
					c.currentModel = cur
				}
			}
			if id, _ := m["id"].(string); id == "thought_level" {
				if cur, _ := m["currentValue"].(string); cur != "" {
					c.currentEffort = cur
				}
			}
		}
	}
	c.configOptions = out
}

func (c *acpSession) hasConfigValueLocked(configID, value string) bool {
	if value == "" {
		return false
	}
	for _, o := range c.configOptions {
		if id, _ := o["id"].(string); id != configID {
			continue
		}
		rawOpts, _ := o["options"].([]any)
		for _, ro := range rawOpts {
			if m, _ := ro.(map[string]any); m["value"] == value {
				return true
			}
		}
	}
	return false
}

func parseAvailableModes(res cli.Msg) map[string]bool {
	out := map[string]bool{}
	modesObj, _ := res["modes"].(map[string]any)
	raw, _ := modesObj["availableModes"].([]any)
	for _, x := range raw {
		if m, _ := x.(map[string]any); m != nil {
			if id, _ := m["id"].(string); id != "" {
				out[id] = true
			}
		}
	}
	return out
}

// SendUser enqueues a user message for the session's prompt worker and emits
// command_lifecycle ("queued") immediately when a turn is already running.
func (c *acpSession) SendUser(content []cli.Msg, uuid string) error {
	c.mu.Lock()
	if c.inputClosed {
		c.mu.Unlock()
		return fmt.Errorf("session input is closed")
	}
	busy := c.turnRunning || c.reattachActive || c.unsolicitedActive || len(c.queue) > 0
	c.queue = append(c.queue, queuedPrompt{uuid: uuid, content: content})
	c.mu.Unlock()
	if busy && uuid != "" {
		c.emit(cli.Msg{"type": "command_lifecycle", "command_uuid": uuid, "state": "queued"}, 0)
	}
	c.signalWorker()
	return nil
}

// CancelMessage withdraws a queued user message before it starts executing.
func (c *acpSession) CancelMessage(ctx context.Context, uuid string) (bool, error) {
	c.mu.Lock()
	for i, q := range c.queue {
		if q.uuid == uuid {
			c.queue = append(c.queue[:i], c.queue[i+1:]...)
			c.mu.Unlock()
			c.emit(cli.Msg{"type": "command_lifecycle", "command_uuid": uuid, "state": "cancelled"}, 0)
			return true, nil
		}
	}
	c.mu.Unlock()
	return false, nil
}

// Interrupt cancels the active turn via ACP `session/cancel`.
func (c *acpSession) Interrupt(ctx context.Context) error {
	c.mu.Lock()
	sid := c.sessionID
	wake := false
	if !c.turnRunning && (c.unsolicitedActive || c.reattachActive) {
		c.unsolicitedInterrupted = true
		c.armSettleLocked(c.settleDelay)
		wake = true
	}
	c.mu.Unlock()
	if wake {
		c.signalWorker()
	}
	if sid == "" {
		return nil
	}
	return c.notify("session/cancel", cli.Msg{"sessionId": sid})
}

// SetModel changes the active model mid-session (re-applying the current
// effort when supported by the new model).
func (c *acpSession) SetModel(ctx context.Context, model string) error {
	c.mu.Lock()
	eff := c.currentEffort
	c.mu.Unlock()
	return c.applyModelAndEffort(ctx, model, eff)
}

// Close ends the session cleanly once any in-flight turn finishes.
// Closing runs on promptWorker so Close never blocks the Out() consumer.
func (c *acpSession) Close() error {
	c.mu.Lock()
	c.inputClosed = true
	c.mu.Unlock()
	select {
	case c.qCh <- struct{}{}:
	default:
	}
	return nil
}

// Kill immediately terminates the underlying process and closes stdout.
func (c *acpSession) Kill() {
	c.mu.Lock()
	c.inputClosed = true
	c.killed = true
	c.mu.Unlock()
	select {
	case c.qCh <- struct{}{}:
	default:
	}
	c.pipe.Kill()
}

func (c *acpSession) signalWorker() {
	select {
	case c.qCh <- struct{}{}:
	default:
	}
}

// openToolLocked reports a tool of the current turn that has not finished:
// agy is waiting on it, not done (V0.agy-open-tool-settle).
func (c *acpSession) openToolLocked() bool {
	for id := range c.emittedTools {
		if !c.finishedTools[id] {
			return true
		}
	}
	return false
}

func (c *acpSession) armSettleLocked(d time.Duration) {
	c.settleGen++
	c.settleArmed = true
	c.settleDeadline = time.Now().Add(d)
}

func (c *acpSession) cancelSettleLocked() {
	if c.settleArmed {
		c.settleGen++
		c.settleArmed = false
		c.settleDeadline = time.Time{}
	}
}

func (c *acpSession) resetTurnMapsLocked() {
	c.emittedTools = map[string]bool{}
	c.finishedTools = map[string]bool{}
	c.rawNames = map[string]string{}
	c.toolNames = map[string]string{}
	c.toolDescs = map[string]bgTool{}
	for id, bt := range c.bgTools {
		c.emittedTools[id] = true
		c.toolDescs[id] = bt
	}
}

func (c *acpSession) promoteOpenToolsLocked() (added, all []bgTool) {
	for id := range c.emittedTools {
		if c.finishedTools[id] {
			continue
		}
		if _, exists := c.bgTools[id]; exists {
			continue
		}
		bt, ok := c.toolDescs[id]
		if !ok {
			name := c.toolNames[id]
			if name == "" {
				name = c.rawNames[id]
			}
			taskType := "local_bash"
			if name == "Agent" || name == "Task" {
				taskType = "local_agent"
			}
			bt = bgTool{id: id, taskType: taskType, desc: name}
		}
		c.bgTools[id] = bt
		added = append(added, bt)
	}
	if len(added) == 0 {
		return nil, nil
	}
	sort.Slice(added, func(i, j int) bool { return added[i].id < added[j].id })
	all = c.sortedBGToolsLocked()
	return added, all
}

func (c *acpSession) sortedBGToolsLocked() []bgTool {
	if len(c.bgTools) == 0 {
		return nil
	}
	all := make([]bgTool, 0, len(c.bgTools))
	for _, bt := range c.bgTools {
		all = append(all, bt)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].id < all[j].id })
	return all
}

func (c *acpSession) emitBackgroundChanged(all []bgTool) {
	tasks := make([]any, 0, len(all))
	for _, bt := range all {
		tasks = append(tasks, cli.Msg{
			"task_id":     bt.id,
			"task_type":   bt.taskType,
			"description": bt.desc,
		})
	}
	c.emit(cli.Msg{
		"type":    "system",
		"subtype": "background_tasks_changed",
		"tasks":   tasks,
	}, 0)
}

func (c *acpSession) emitBackgroundStarted(added, all []bgTool) {
	c.emitBackgroundChanged(all)
	for _, bt := range added {
		c.emit(cli.Msg{
			"type":            "system",
			"subtype":         "task_started",
			"task_id":         bt.id,
			"tool_use_id":     bt.id,
			"task_type":       bt.taskType,
			"description":     bt.desc,
			"is_backgrounded": true,
		}, 0)
	}
}

func makeBGTool(callID, mappedName string, mappedArgs map[string]any) bgTool {
	taskType := "local_bash"
	if mappedName == "Agent" || mappedName == "Task" {
		taskType = "local_agent"
	}
	desc := ""
	for _, key := range []string{"description", "command", "prompt", "file_path", "pattern", "query"} {
		if v, ok := mappedArgs[key].(string); ok {
			if line := firstNonEmptyLine(v); line != "" {
				desc = line
				break
			}
		}
	}
	if desc == "" {
		desc = mappedName
	}
	return bgTool{id: callID, taskType: taskType, desc: desc}
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			if r := []rune(line); len(r) > 120 {
				return string(r[:119]) + "…"
			}
			return line
		}
	}
	return ""
}

func (c *acpSession) promptWorker() {
	defer close(c.workerDone)
	var timer *time.Timer
	stopTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer = nil
		}
	}
	defer stopTimer()

	for {
		c.mu.Lock()
		if c.settleArmed {
			now := time.Now()
			if !now.Before(c.settleDeadline) {
				gen := c.settleGen
				c.mu.Unlock()
				stopTimer()
				c.finishUnsolicitedTurn(gen)
				continue
			}
			wait := time.Until(c.settleDeadline)
			c.mu.Unlock()
			stopTimer()
			timer = time.NewTimer(wait)
			select {
			case <-timer.C:
				continue
			case <-c.qCh:
				continue
			case <-c.readDone:
				return
			}
		}
		if c.reattachActive || c.unsolicitedActive {
			c.mu.Unlock()
			stopTimer()
			select {
			case <-c.qCh:
				continue
			case <-c.readDone:
				return
			}
		}
		if len(c.queue) == 0 {
			closed := c.inputClosed
			killed := c.killed
			sid := c.sessionID
			c.mu.Unlock()
			stopTimer()
			if closed {
				if !killed && sid != "" {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					_, _ = c.request(ctx, "session/close", cli.Msg{"sessionId": sid})
					cancel()
				}
				_ = c.pipe.CloseInput()
				return
			}
			select {
			case <-c.qCh:
				continue
			case <-c.readDone:
				return
			}
		}
		if isClosed(c.readDone) {
			// The process is gone: a queued prompt never starts, and it gets
			// no made-up result either.
			c.mu.Unlock()
			return
		}
		item := c.queue[0]
		c.queue = c.queue[1:]
		c.turnRunning = true
		c.reattachActive = false
		c.unsolicitedActive = false
		c.unsolicitedInterrupted = false
		c.cancelSettleLocked()
		c.textBuf.Reset()
		c.turnText.Reset()
		c.resetTurnMapsLocked()
		sid := c.sessionID
		model := c.currentModel
		needInit := !c.initEmitted
		c.initEmitted = true
		c.mu.Unlock()
		stopTimer()

		var startedOnce sync.Once
		started := func() {
			startedOnce.Do(func() {
				if item.uuid != "" {
					c.emit(cli.Msg{"type": "command_lifecycle", "command_uuid": item.uuid, "state": "started"}, 0)
				}
				if needInit && sid != "" {
					c.emit(cli.Msg{"type": "system", "subtype": "init", "session_id": sid, "model": model}, 0)
				}
			})
		}
		start := time.Now()
		res, respSeq, err := c.requestOn(context.Background(), "session/prompt", cli.Msg{
			"sessionId": sid,
			"prompt":    convertPromptBlocks(item.content),
		}, true, started)
		durMs := time.Since(start).Milliseconds()
		if err != nil && isClosed(c.readDone) {
			// The process exited: read() closes Out and reports Err. A
			// result here would be a made-up one, and read() waits for this
			// worker before it closes Out (V0.review-hangs (9)).
			c.mu.Lock()
			c.turnRunning = false
			c.mu.Unlock()
			return
		}

		// Flush any trailing assistant text, promote any still-open background
		// tools, and emit result atomically under emitMu so a subsequent
		// reactive-wakeup notification cannot interleave before result.
		c.emitMu.Lock()
		c.mu.Lock()
		trailingText := c.textBuf.String()
		c.textBuf.Reset()
		fullText := c.turnText.String()
		c.turnText.Reset()
		var addedBG, allBG []bgTool
		if err == nil {
			stopReason, _ := res["stopReason"].(string)
			if stopReason != "cancelled" && stopReason != "canceled" {
				addedBG, allBG = c.promoteOpenToolsLocked()
			}
		}
		c.turnRunning = false
		shouldClose := c.inputClosed && len(c.queue) == 0
		c.mu.Unlock()

		if strings.TrimSpace(trailingText) != "" {
			c.emitAssistantText(trailingText, 0)
		}
		if len(addedBG) > 0 {
			c.emitBackgroundStarted(addedBG, allBG)
		}
		if item.uuid != "" {
			c.emit(cli.Msg{"type": "command_lifecycle", "command_uuid": item.uuid, "state": "completed"}, 0)
		}
		if err != nil {
			c.emitMu.Unlock()
			c.emitHeld(cli.Msg{
				"type":        "result",
				"subtype":     "error",
				"is_error":    true,
				"result":      err.Error(),
				"duration_ms": float64(durMs),
			}, respSeq)
		} else {
			stopReason, _ := res["stopReason"].(string)
			subtype := "success"
			if stopReason == "cancelled" || stopReason == "canceled" {
				subtype = "interrupted"
			}
			outMsg := cli.Msg{
				"type":        "result",
				"subtype":     subtype,
				"is_error":    false,
				"result":      fullText,
				"duration_ms": float64(durMs),
			}
			if u, ok := res["usage"].(map[string]any); ok {
				outMsg["usage"] = u
			}
			c.emitHeld(outMsg, respSeq)
			c.emitMu.Unlock()
		}
		if shouldClose {
			if sid != "" {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_, _ = c.request(ctx, "session/close", cli.Msg{"sessionId": sid})
				cancel()
			}
			_ = c.pipe.CloseInput()
			return
		}
	}
}

func convertPromptBlocks(content []cli.Msg) []cli.Msg {
	out := make([]cli.Msg, 0, len(content))
	for _, blk := range content {
		switch blk["type"] {
		case "text":
			if t, _ := blk["text"].(string); t != "" {
				out = append(out, cli.Msg{"type": "text", "text": t})
			}
		case "image":
			if src, _ := blk["source"].(cli.Msg); src != nil {
				mt, _ := src["media_type"].(string)
				data, _ := src["data"].(string)
				if mt != "" && data != "" {
					out = append(out, cli.Msg{"type": "image", "mimeType": mt, "data": data})
				}
			} else if mt, _ := blk["mimeType"].(string); mt != "" {
				if data, _ := blk["data"].(string); data != "" {
					out = append(out, cli.Msg{"type": "image", "mimeType": mt, "data": data})
				}
			}
		}
	}
	if len(out) == 0 {
		out = append(out, cli.Msg{"type": "text", "text": ""})
	}
	return out
}

func (c *acpSession) emitAssistantText(text string, seq int64) {
	c.emit(cli.Msg{
		"type": "assistant",
		"message": cli.Msg{
			"role":    "assistant",
			"content": []any{cli.Msg{"type": "text", "text": text}},
		},
	}, seq)
}

func (c *acpSession) flushAssistantText(seq int64) {
	c.mu.Lock()
	text := c.textBuf.String()
	c.textBuf.Reset()
	c.mu.Unlock()
	if strings.TrimSpace(text) == "" {
		return
	}
	c.emitAssistantText(text, seq)
}

func (c *acpSession) read() {
	for {
		line, seq, err := c.pipe.ReadLine()
		if err != nil {
			break
		}
		c.logf("<<", line)
		var m cli.Msg
		if err := json.Unmarshal(line, &m); err != nil {
			c.note(seq, false)
			c.ack()
			continue
		}
		method, hasMethod := m["method"].(string)
		rawID, hasID := m["id"]
		switch {
		case !hasMethod && hasID:
			// JSON-RPC response to one of our requests.
			idKey := jsonIDKey(rawID)
			var resp rpcResp
			if errObj, _ := m["error"].(map[string]any); errObj != nil {
				code, _ := errObj["code"].(float64)
				msg, _ := errObj["message"].(string)
				resp.err = &rpcError{Code: int(code), Message: msg}
			} else if res, ok := m["result"].(map[string]any); ok {
				resp.result = res
			}
			c.pmu.Lock()
			pr, ok := c.pending[idKey]
			delete(c.pending, idKey)
			c.pmu.Unlock()
			if ok {
				if pr.holdSeq && seq > 0 {
					c.note(seq, true)
					resp.seq = seq
				}
				pr.ch <- resp
				if !pr.holdSeq {
					c.note(seq, false)
					c.ack()
				}
			} else if c.finishReattachedTurn(resp, seq) {
				// finishReattachedTurn queued a result message carrying seq.
			} else {
				c.note(seq, false)
				c.ack()
			}
		case hasMethod && hasID:
			// Server -> Client JSON-RPC request (e.g. session/request_permission).
			c.mu.Lock()
			c.cancelSettleLocked()
			c.mu.Unlock()
			idKey := jsonIDKey(rawID)
			c.opened(idKey, seq)
			go c.handleServerRequest(rawID, idKey, method, m["params"])
			c.note(seq, false)
			c.ack()
		case hasMethod:
			// Server -> Client notification (session/update).
			queued := c.handleNotification(method, m["params"], seq)
			if !queued {
				c.note(seq, false)
				c.ack()
			}
		default:
			c.note(seq, false)
			c.ack()
		}
	}
	c.err = c.pipe.Wait()
	close(c.readDone)
	c.pmu.Lock()
	for id, pr := range c.pending {
		close(pr.ch)
		delete(c.pending, id)
	}
	c.pmu.Unlock()
	// Cancel any open permission requests when the process exits.
	c.mu.Lock()
	c.inputClosed = true
	openIDs := make([]string, 0, len(c.openPerms))
	for id := range c.openPerms {
		openIDs = append(openIDs, id)
	}
	c.openPerms = map[string]bool{}
	c.mu.Unlock()
	if c.h.Cancel != nil {
		for _, id := range openIDs {
			c.h.Cancel(id)
		}
	}
	select {
	case c.qCh <- struct{}{}:
	default:
	}
	<-c.workerDone
	close(c.out)
	if c.cleanup != nil {
		c.cleanup()
	}
	close(c.done)
}

func jsonIDKey(id any) string {
	switch v := id.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	default:
		return fmt.Sprint(v)
	}
}

func (c *acpSession) handleNotification(method string, rawParams any, seq int64) bool {
	if method != "session/update" {
		return false
	}
	params, _ := rawParams.(map[string]any)
	if params == nil {
		return false
	}
	if sid, _ := params["sessionId"].(string); sid != "" {
		c.mu.Lock()
		if c.sessionID == "" {
			c.sessionID = sid
		}
		c.mu.Unlock()
	}
	upd, _ := params["update"].(map[string]any)
	if upd == nil {
		return false
	}
	kind, _ := upd["sessionUpdate"].(string)
	switch kind {
	case "agent_message_chunk":
		if content, _ := upd["content"].(map[string]any); content != nil {
			if t, _ := content["text"].(string); t != "" {
				wake := false
				c.mu.Lock()
				c.textBuf.WriteString(t)
				c.turnText.WriteString(t)
				if !c.turnRunning {
					if c.reattachActive {
						c.cancelSettleLocked()
					} else {
						c.unsolicitedActive = true
						// Fallback settle window in case agy omits usage_update
						// (e.g. quota warnings); usage_update tightens this to settleDelay.
						// Not while a tool is open: the model wrote this note and
						// then waited on the tool, it did not end its turn.
						if len(c.openPerms) == 0 && !c.openToolLocked() {
							c.armSettleLocked(3 * c.settleDelay)
							wake = true
						}
					}
				}
				c.mu.Unlock()
				if wake {
					c.signalWorker()
				}
			}
		}
		return false
	case "agent_thought_chunk":
		c.emitMu.Lock()
		c.mu.Lock()
		c.cancelSettleLocked()
		startUnsolicited := !c.turnRunning && !c.reattachActive && !c.unsolicitedActive
		if startUnsolicited {
			c.unsolicitedActive = true
		}
		c.mu.Unlock()
		if startUnsolicited {
			// V0.agy-reactive-wakeup: open the run immediately as "Thinking"
			// when a reactive wakeup starts thinking before its first tool call.
			c.emit(cli.Msg{
				"type": "assistant",
				"message": cli.Msg{
					"role":    "assistant",
					"content": []any{cli.Msg{"type": "thinking", "thinking": ""}},
				},
			}, seq)
			c.emitMu.Unlock()
			return true
		}
		c.emitMu.Unlock()
		return false
	case "usage_update":
		wake := false
		c.mu.Lock()
		if !c.turnRunning && (c.unsolicitedActive || c.reattachActive) &&
			strings.TrimSpace(c.textBuf.String()) != "" && len(c.openPerms) == 0 && !c.openToolLocked() {
			c.armSettleLocked(c.settleDelay)
			wake = true
		}
		c.mu.Unlock()
		if wake {
			c.signalWorker()
		}
		return false
	case "available_commands_update":
		if rawCmds, _ := upd["availableCommands"].([]any); len(rawCmds) > 0 {
			cmds := make([]cli.Msg, 0, len(rawCmds))
			for _, x := range rawCmds {
				if m, ok := x.(map[string]any); ok {
					cmds = append(cmds, m)
				}
			}
			c.mu.Lock()
			c.commands = cmds
			c.mu.Unlock()
		}
		return false
	case "config_option_update":
		c.mu.Lock()
		c.updateConfigOptionsLocked(upd["configOptions"])
		c.mu.Unlock()
		return false
	case "tool_call", "tool_call_update":
		return c.handleToolUpdate(upd, seq)
	}
	return false
}

func (c *acpSession) handleToolUpdate(upd map[string]any, seq int64) bool {
	callID, _ := upd["toolCallId"].(string)
	rawName, _ := upd["name"].(string)
	if callID == "" {
		return false
	}
	args, _ := upd["args"].(map[string]any)
	if args == nil {
		args, _ = upd["rawInput"].(map[string]any)
	}
	status := acpToolStatus(fmt.Sprint(upd["status"]))
	isTerminal := status == toolStatusCompleted || status == toolStatusFailed || status == toolStatusCancelled

	c.emitMu.Lock()
	defer c.emitMu.Unlock()

	c.mu.Lock()
	c.cancelSettleLocked()
	if rawName == "" {
		rawName = c.rawNames[callID]
	}
	if rawName == "" {
		rawName, _ = upd["title"].(string)
	}
	if rawName != "" {
		c.rawNames[callID] = rawName
	}
	alreadyEmitted := c.emittedTools[callID]
	alreadyFinished := c.finishedTools[callID]
	mcpNames := c.mcpNames
	if !c.turnRunning && !c.reattachActive && !isTerminal {
		c.unsolicitedActive = true
	}
	c.mu.Unlock()

	queued := false
	if !alreadyEmitted && rawName != "" {
		c.flushAssistantText(0)
		mappedName, mappedArgs := translateToolCall(rawName, args, mcpNames)
		bt := makeBGTool(callID, mappedName, mappedArgs)
		c.mu.Lock()
		c.emittedTools[callID] = true
		c.toolNames[callID] = mappedName
		c.toolDescs[callID] = bt
		c.mu.Unlock()
		c.emit(cli.Msg{
			"type": "assistant",
			"message": cli.Msg{
				"role": "assistant",
				"content": []any{cli.Msg{
					"type":  "tool_use",
					"id":    callID,
					"name":  mappedName,
					"input": mappedArgs,
				}},
			},
		}, seq)
		queued = true
	}

	if !alreadyFinished && isTerminal {
		if !queued {
			// Flush any interim assistant text written before this tool finished
			// (e.g. a background command completing right after a status note).
			c.flushAssistantText(0)
		}
		var wasBG bool
		var remainingBG []bgTool
		rearm := false
		c.mu.Lock()
		c.finishedTools[callID] = true
		if _, ok := c.bgTools[callID]; ok {
			wasBG = true
			delete(c.bgTools, callID)
			remainingBG = c.sortedBGToolsLocked()
		}
		// V0.agy-settle-final-text (amends V0.agy-open-tool-settle): a tool
		// ending is not a turn ending. The live streams complete a tool and
		// then think for seconds (sometimes minutes) before the next tool
		// batch or the final answer, so arming here closed a turn the model
		// was still answering — one continuous answer became dozens of runs.
		// Only an interrupted turn is armed here: its cancel already ended
		// the stream and the open tool is the last thing it waits on. A quiet
		// turn is settled by its last assistant text, which arms on arrival.
		if c.unsolicitedInterrupted && !c.turnRunning && !c.reattachActive &&
			len(c.openPerms) == 0 && !c.openToolLocked() {
			c.armSettleLocked(c.settleDelay)
			rearm = true
		}
		c.mu.Unlock()
		resultText := extractToolResult(upd)
		resultSeq := seq
		if queued {
			resultSeq = 0 // already tracked seq on the tool_use message
		}
		c.emit(cli.Msg{
			"type": "user",
			"message": cli.Msg{
				"role": "user",
				"content": []any{cli.Msg{
					"type":        "tool_result",
					"tool_use_id": callID,
					"content":     resultText,
					"is_error":    status == toolStatusFailed || status == toolStatusCancelled,
				}},
			},
		}, resultSeq)
		if wasBG {
			taskStat := "completed"
			switch status {
			case toolStatusFailed:
				taskStat = "failed"
			case toolStatusCancelled:
				taskStat = "stopped"
			}
			c.emit(cli.Msg{
				"type":    "system",
				"subtype": "task_notification",
				"task_id": callID,
				"status":  taskStat,
				"summary": firstNonEmptyLine(resultText),
			}, 0)
			c.emitBackgroundChanged(remainingBG)
		}
		if rearm {
			c.signalWorker()
		}
		queued = true
	}
	return queued
}

// finishUnsolicitedTurn closes a reactive-wakeup turn (or a reattached
// reactive-wakeup turn) after its settle timer fires with no further ACP
// notifications or JSON-RPC stopReason response (V0.agy-reactive-wakeup).
func (c *acpSession) finishUnsolicitedTurn(gen uint64) {
	c.emitMu.Lock()
	c.mu.Lock()
	if c.turnRunning || !c.settleArmed || c.settleGen != gen || len(c.openPerms) > 0 {
		c.mu.Unlock()
		c.emitMu.Unlock()
		return
	}
	c.settleArmed = false
	c.settleDeadline = time.Time{}
	interrupted := c.unsolicitedInterrupted
	c.unsolicitedActive = false
	c.unsolicitedInterrupted = false
	c.reattachActive = false
	text := c.textBuf.String()
	c.textBuf.Reset()
	fullText := c.turnText.String()
	c.turnText.Reset()
	var addedBG, allBG []bgTool
	if !interrupted {
		addedBG, allBG = c.promoteOpenToolsLocked()
	}
	c.resetTurnMapsLocked()
	c.mu.Unlock()

	if strings.TrimSpace(text) != "" {
		c.emitAssistantText(text, 0)
	}
	if len(addedBG) > 0 {
		c.emitBackgroundStarted(addedBG, allBG)
	}
	subtype := "success"
	if interrupted {
		subtype = "interrupted"
	}
	c.emit(cli.Msg{
		"type":     "result",
		"subtype":  subtype,
		"is_error": false,
		"result":   fullText,
	}, 0)
	c.emitMu.Unlock()
	c.signalWorker()
}

// finishReattachedTurn completes an in-flight session/prompt that was started
// before fyrd restarted (V0.90) when its JSON-RPC response arrives on a
// reattached pipe with no local waiter in c.pending.
func (c *acpSession) finishReattachedTurn(resp rpcResp, seq int64) bool {
	stopReason, hasStop := resp.result["stopReason"].(string)
	c.emitMu.Lock()
	c.mu.Lock()
	active := (c.reattachActive || c.unsolicitedActive) && !c.turnRunning
	if !hasStop && !(resp.err != nil && active) {
		c.mu.Unlock()
		c.emitMu.Unlock()
		return false
	}
	c.cancelSettleLocked()
	text := c.textBuf.String()
	c.textBuf.Reset()
	fullText := c.turnText.String()
	c.turnText.Reset()
	var addedBG, allBG []bgTool
	if resp.err == nil && stopReason != "cancelled" && stopReason != "canceled" {
		addedBG, allBG = c.promoteOpenToolsLocked()
	}
	c.resetTurnMapsLocked()
	c.unsolicitedActive = false
	c.unsolicitedInterrupted = false
	c.mu.Unlock()

	if strings.TrimSpace(text) != "" {
		c.emitAssistantText(text, 0)
	}
	if len(addedBG) > 0 {
		c.emitBackgroundStarted(addedBG, allBG)
	}
	if resp.err != nil {
		c.emit(cli.Msg{
			"type":     "result",
			"subtype":  "error",
			"is_error": true,
			"result":   resp.err.Error(),
		}, seq)
	} else {
		subtype := "success"
		if stopReason == "cancelled" || stopReason == "canceled" {
			subtype = "interrupted"
		}
		outMsg := cli.Msg{
			"type":     "result",
			"subtype":  subtype,
			"is_error": false,
			"result":   fullText,
		}
		if u, ok := resp.result["usage"].(map[string]any); ok {
			outMsg["usage"] = u
		}
		c.emit(outMsg, seq)
	}
	// Release promptWorker only after the result is on c.out, so any queued
	// follow-up turn starts strictly after it (V0.agy-reattach-queue).
	c.mu.Lock()
	c.reattachActive = false
	c.mu.Unlock()
	c.emitMu.Unlock()
	c.signalWorker()
	return true
}

func extractToolResult(upd map[string]any) string {
	if rawContent, _ := upd["content"].([]any); len(rawContent) > 0 {
		var sb strings.Builder
		for _, x := range rawContent {
			m, _ := x.(map[string]any)
			if m == nil {
				continue
			}
			if inner, _ := m["content"].(map[string]any); inner != nil {
				if t, _ := inner["text"].(string); t != "" {
					if sb.Len() > 0 {
						sb.WriteByte('\n')
					}
					sb.WriteString(t)
					continue
				}
			}
			if t, _ := m["text"].(string); t != "" {
				if sb.Len() > 0 {
					sb.WriteByte('\n')
				}
				sb.WriteString(t)
			}
		}
		if sb.Len() > 0 {
			return unwrapToolResult(sb.String())
		}
	}
	if rawOut, _ := upd["rawOutput"].(map[string]any); rawOut != nil {
		if s, _ := rawOut["output"].(string); s != "" {
			return unwrapToolResult(s)
		}
		if b, err := json.Marshal(rawOut); err == nil {
			return unwrapToolResult(string(b))
		}
	}
	if s, _ := upd["rawOutput"].(string); s != "" {
		return unwrapToolResult(s)
	}
	return ""
}

func (c *acpSession) handleServerRequest(rawID any, idKey, method string, rawParams any) {
	if method != "session/request_permission" {
		_ = c.write(cli.Msg{
			"jsonrpc": "2.0",
			"id":      rawID,
			"error":   cli.Msg{"code": -32601, "message": "unsupported method: " + method},
		})
		c.answered(idKey)
		return
	}
	params, _ := rawParams.(map[string]any)
	if sid, _ := params["sessionId"].(string); sid != "" {
		c.mu.Lock()
		if c.sessionID == "" {
			c.sessionID = sid
		}
		c.mu.Unlock()
	}
	tc, _ := params["toolCall"].(map[string]any)
	rawName, _ := tc["name"].(string)
	if rawName == "" {
		rawName, _ = tc["title"].(string)
	}
	callID, _ := tc["toolCallId"].(string)
	args, _ := tc["args"].(map[string]any)
	if args == nil {
		args, _ = tc["rawInput"].(map[string]any)
	}
	c.mu.Lock()
	mcpNames := c.mcpNames
	c.mu.Unlock()
	mappedName, mappedArgs := translateToolCall(rawName, args, mcpNames)
	options, _ := params["options"].([]any)

	allowID, denyID := pickPermissionOptions(options)

	// 1. Check Fyr's PreToolUse gate hook (merge gate, sudo gate, heavy-job slots).
	if mappedName == "Bash" && c.h.Hook != nil {
		gateResp := c.h.Hook("gate", cli.Msg{
			"tool_name":   mappedName,
			"tool_input":  mappedArgs,
			"tool_use_id": callID,
		})
		if gateDenied(gateResp) {
			c.replyPermission(rawID, idKey, denyID)
			return
		}
	}

	// 2. Enforce disabled CLI timers (schedule) when Fyr's watch tool is active.
	if isTimerBlocked(rawName, c.spec.DisallowedTools) {
		c.replyPermission(rawID, idKey, denyID)
		return
	}

	// 3. Auto-allow Fyr's own MCP tools (e.g. mcp__fyr__*).
	if matchesToolPattern(mappedName, c.spec.AllowedTools) {
		c.replyPermission(rawID, idKey, allowID)
		return
	}

	// 4. Ask the owner through Fyr's CanUseTool handler (blocks until answered).
	if c.h.CanUseTool == nil {
		c.replyPermission(rawID, idKey, allowID)
		return
	}
	c.mu.Lock()
	c.cancelSettleLocked()
	c.openPerms[idKey] = true
	c.mu.Unlock()
	ans := c.h.CanUseTool(cli.Msg{
		"request_id":  idKey,
		"tool_name":   mappedName,
		"input":       mappedArgs,
		"tool_use_id": callID,
	})
	c.mu.Lock()
	delete(c.openPerms, idKey)
	c.mu.Unlock()

	if ans["behavior"] == "allow" {
		c.replyPermission(rawID, idKey, allowID)
	} else {
		c.replyPermission(rawID, idKey, denyID)
	}
}

func (c *acpSession) replyPermission(rawID any, idKey, optionID string) {
	var outcome cli.Msg
	if optionID != "" {
		outcome = cli.Msg{"outcome": "selected", "optionId": optionID}
	} else {
		outcome = cli.Msg{"outcome": "cancelled"}
	}
	if c.write(cli.Msg{"jsonrpc": "2.0", "id": rawID, "result": cli.Msg{"outcome": outcome}}) == nil {
		c.answered(idKey)
	}
}

func pickPermissionOptions(options []any) (allowID, denyID string) {
	for _, x := range options {
		m, _ := x.(map[string]any)
		if m == nil {
			continue
		}
		id, _ := m["optionId"].(string)
		kind := acpPermKind(fmt.Sprint(m["kind"]))
		switch kind {
		case permAllowOnce:
			if allowID == "" {
				allowID = id
			}
		case permAllowAlways:
			if allowID == "" {
				allowID = id
			}
		case permRejectOnce:
			if denyID == "" {
				denyID = id
			}
		case permRejectAlways:
			if denyID == "" {
				denyID = id
			}
		}
	}
	return allowID, denyID
}

func gateDenied(resp cli.Msg) bool {
	if resp == nil {
		return false
	}
	inner, _ := resp["hookSpecificOutput"].(cli.Msg)
	if inner == nil {
		if m, ok := resp["hookSpecificOutput"].(map[string]any); ok {
			inner = m
		}
	}
	if inner == nil {
		return false
	}
	return inner["permissionDecision"] == "deny"
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
