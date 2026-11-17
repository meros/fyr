// Package runner holds the agent CLI processes outside fyrd (SPEC 0.5
// V0.90, decision 0023). The launcher runs a Server, which never restarts
// with fyrd: it starts each CLI, keeps its stdin and stdout, and buffers
// what the CLI writes while no fyrd listens. A new fyrd (a deploy, a crash)
// lists the processes, attaches to each, and reads on from what the last
// fyrd had not handled.
//
// The runner has no knowledge of the CLI's protocol: every rule about what
// a line means lives in fyrd. It numbers each stdout line and stores it
// until fyrd acks it; an ack may name lines to keep (fyrd keeps a question
// the CLI still waits on, so the next fyrd gets it again).
//
// The protocol is one JSON object per line on a Unix socket. The first
// frame of a connection names its op: "list" answers once; "spawn" and
// "attach" make the connection the link to one process. On that link the
// runner sends the stored lines with their numbers, then each new line,
// then the exit; fyrd sends lines to write, "ack", "close", "kill" and
// "forget". Keep the protocol stable: the launcher changes only when the
// machine's Fyr package does, and every fyrd build talks to it.
package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Version is the protocol version. A fyrd that gets another one from list
// starts its processes as its own children, as before V0.90.
const Version = 1

// Spawn is how to start one process.
type Spawn struct {
	// ID names the process in the runner; fyrd chooses it, unique.
	ID string `json:"id"`
	// Key: at most one live process per key (the thread id). A spawn for a
	// key that has one fails with ExistsError naming it.
	Key    string   `json:"key,omitempty"`
	Binary string   `json:"binary"`
	Args   []string `json:"args,omitempty"`
	Dir    string   `json:"dir,omitempty"`
	// Env is the whole environment: the runner adds nothing of its own.
	Env []string `json:"env,omitempty"`
	// Cgroup is the directory the process starts in (V0.80); "" is the
	// runner's.
	Cgroup string `json:"cgroup,omitempty"`
	// Meta is fyrd's data about the process; list gives it back.
	Meta json.RawMessage `json:"meta,omitempty"`
}

// Proc is one process the runner holds.
type Proc struct {
	ID      string          `json:"id"`
	Key     string          `json:"key,omitempty"`
	Pid     int             `json:"pid"`
	Meta    json.RawMessage `json:"meta,omitempty"`
	Started time.Time       `json:"started"`
	Exited  bool            `json:"exited,omitempty"`
	// Linked: a fyrd holds a link to it now.
	Linked bool `json:"linked,omitempty"`
}

// ExistsError: a spawn for a key that has a live process.
type ExistsError struct{ ID, Key string }

func (e *ExistsError) Error() string {
	return fmt.Sprintf("process %s runs for %s", e.ID, e.Key)
}

// frame is every message on the socket, both ways.
type frame struct {
	Op      string `json:"op,omitempty"`
	Version int    `json:"version,omitempty"`
	Spawn   *Spawn `json:"spawn,omitempty"`
	ID      string `json:"id,omitempty"`
	// Text is one line without its newline: to the CLI's stdin (write),
	// or from its stdout with Seq.
	Text string `json:"text,omitempty"`
	Seq  int64  `json:"seq,omitempty"`
	// Keep (ack): lines up to Seq that the runner stores on.
	Keep []int64 `json:"keep,omitempty"`
	// Exists (a refused spawn): the live process of the key.
	Exists string `json:"exists,omitempty"`

	OK    bool   `json:"ok,omitempty"`
	Error string `json:"error,omitempty"`
	Pid   int    `json:"pid,omitempty"`
	Procs []Proc `json:"procs,omitempty"`
	// Exit: the process ended; Err is its exit error with the stderr tail.
	Exit bool   `json:"exit,omitempty"`
	Err  string `json:"err,omitempty"`
}

// Pipe is the line link to one CLI process: a child of fyrd, or a process
// the runner holds. The claude client reads and writes through it.
type Pipe interface {
	// WriteLine writes one line to the process's stdin; it adds the newline.
	WriteLine(b []byte) error
	// ReadLine returns the next stdout line and its sequence number (0 for
	// a child of fyrd), and io.EOF when the output ended.
	ReadLine() ([]byte, int64, error)
	// Wait returns the exit error after ReadLine returned io.EOF.
	Wait() error
	// CloseInput closes stdin; the CLI ends its session and exits.
	CloseInput() error
	// Kill stops the process and everything it started, and ends ReadLine.
	Kill()
	// Ack says that fyrd is done with every line up to seq except keep: a
	// later fyrd gets only the kept lines and the newer ones. A no-op for a
	// child of fyrd.
	Ack(seq int64, keep []int64)
	Pid() int
}

var _ io.Closer = (*Conn)(nil)

// SocketPath is where the launcher with data folder data listens:
// <data>/runner.sock, or a short path in the runtime folder when that one
// is longer than a Unix socket path may be (108 bytes on Linux).
func SocketPath(data string) string {
	p := filepath.Join(data, "runner.sock")
	if len(p) < 100 {
		return p
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	sum := sha256.Sum256([]byte(data))
	return filepath.Join(dir, "fyr-runner-"+hex.EncodeToString(sum[:6])+".sock")
}

// PeerPid is the pid of the process that listens on the Unix socket at
// path: for an install's runner, its launcher (#167). errors.ErrUnsupported
// where the OS does not say.
func PeerPid(path string) (int, error) {
	c, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return 0, err
	}
	defer c.Close()
	return connPeerPid(c)
}

// connPeerPid is the pid of the process at the other end of a Unix socket
// connection, as of its connect or listen.
func connPeerPid(c net.Conn) (int, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return 0, errors.ErrUnsupported
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, err
	}
	pid, perr := 0, error(nil)
	if err := raw.Control(func(fd uintptr) { pid, perr = peerPidFd(int(fd)) }); err != nil {
		return 0, err
	}
	return pid, perr
}
