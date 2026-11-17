package runner

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/meros/fyr/internal/proc"
)

// The runner knows nothing of what its processes say (SPEC 0.5 V0.90 (2)):
// it stores their output lines by number until fyrd acks them, and it has
// exactly three rules of its own. (1) One live process per key. (2) A
// process that no fyrd has linked to for Abandon ends: stdin closes, and a
// kill follows after Grace. (3) An exited process that no fyrd collected
// is dropped after Abandon.

const (
	// DefaultBuffer is how many bytes of output the runner keeps per
	// process. Past it the oldest lines go.
	DefaultBuffer = 64 << 20
	// DefaultAbandon is how long a process may run with no fyrd linked.
	DefaultAbandon = time.Hour
	// DefaultGrace is the time between closing stdin and the kill.
	DefaultGrace = 10 * time.Second
)

// Server holds the processes. The launcher runs one for its whole life.
type Server struct {
	Logf    func(format string, args ...any)
	Buffer  int           // bytes per process; 0 is DefaultBuffer
	Abandon time.Duration // 0 is DefaultAbandon
	Grace   time.Duration // 0 is DefaultGrace
	Tick    time.Duration // how often rules (2) and (3) are checked; 0 is a minute
	Now     func() time.Time
	// Allow says whether the process with this pid may use the runner
	// (#167): the launcher allows only the fyrd it started, so a fyrd that
	// an agent or a test starts with the live install's variables cannot
	// list, take over or end its processes. nil allows every process of
	// this user (the socket is 0600). Where the OS gives no peer pid, every
	// connection is allowed.
	Allow func(pid int) bool

	mu    sync.Mutex
	procs map[string]*rproc
	ln    net.Listener
	stop  chan struct{}
}

type line struct {
	seq  int64
	text string
}

type rproc struct {
	id      string
	key     string
	meta    json.RawMessage
	started time.Time
	cmd     *exec.Cmd
	in      io.WriteCloser
	out     io.ReadCloser
	wmu     sync.Mutex

	mu    sync.Mutex
	cond  *sync.Cond
	lines []line // stored: not acked, or kept by fyrd's last ack
	size  int
	seq   int64
	// att is the current link's generation: a new link, or the link's end,
	// makes the old link's sender stop.
	att    int
	linked bool
	// alone: when the last link ended (rule 2 and 3 count from it).
	alone   time.Time
	closing time.Time // rule 2: when stdin was closed
	// inClosed: the CLI's stdin is closed (V0.142); a write after it
	// reaches nothing, and fyrd learns it from the process's info.
	inClosed bool
	exited   bool
	err      string
	stderr   *tail
}

func (s *Server) logf(f string, a ...any) {
	if s.Logf != nil {
		s.Logf(f, a...)
	}
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func orDefault(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}

// Listen opens the socket at path, readable only by this user. A socket
// left by a runner that is gone is removed first.
func (s *Server) Listen(path string) error {
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		return fmt.Errorf("a runner already listens on %s", path)
	}
	os.Remove(path)
	old := syscall.Umask(0o077)
	ln, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.ln = ln
	s.stop = make(chan struct{})
	if s.procs == nil {
		s.procs = map[string]*rproc{}
	}
	s.mu.Unlock()
	go s.janitor()
	return nil
}

// Serve accepts links until Close.
func (s *Server) Serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.conn(c)
	}
}

// Close stops listening and ends every process: stdin closes, and what is
// still running after grace is killed.
func (s *Server) Close(grace time.Duration) {
	s.mu.Lock()
	if s.ln != nil {
		s.ln.Close()
	}
	if s.stop != nil {
		close(s.stop)
		s.stop = nil
	}
	ps := make([]*rproc, 0, len(s.procs))
	for _, p := range s.procs {
		ps = append(ps, p)
	}
	s.mu.Unlock()
	for _, p := range ps {
		p.in.Close()
	}
	deadline := time.Now().Add(grace)
	for _, p := range ps {
		for !p.isExited() && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if !p.isExited() {
			p.kill()
		}
	}
}

// janitor applies rules (2) and (3).
func (s *Server) janitor() {
	s.mu.Lock()
	stop := s.stop
	s.mu.Unlock()
	t := time.NewTicker(orDefault(s.Tick, time.Minute))
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.sweep()
		}
	}
}

func (s *Server) sweep() {
	abandon, grace, now := orDefault(s.Abandon, DefaultAbandon), orDefault(s.Grace, DefaultGrace), s.now()
	s.mu.Lock()
	ps := make([]*rproc, 0, len(s.procs))
	for _, p := range s.procs {
		ps = append(ps, p)
	}
	s.mu.Unlock()
	for _, p := range ps {
		p.mu.Lock()
		alone := !p.linked && now.Sub(p.alone) >= abandon
		exited, closing := p.exited, p.closing
		if alone && !exited && closing.IsZero() {
			p.closing = now
		}
		p.mu.Unlock()
		switch {
		case !alone:
		case exited:
			s.mu.Lock()
			delete(s.procs, p.id)
			s.mu.Unlock()
			s.logf("runner: %s: no fyrd collected it; dropped", p.id)
		case closing.IsZero():
			s.logf("runner: %s: no fyrd for %s; ending it", p.id, abandon)
			p.closeInput()
		case now.Sub(closing) >= grace:
			p.kill()
		}
	}
}

func (s *Server) conn(c net.Conn) {
	defer c.Close()
	r := bufio.NewReaderSize(c, 64<<10)
	enc := &encoder{w: c}
	if pid, ok := s.allowed(c); !ok {
		s.logf("runner: refused pid %d: not the fyrd this launcher started (#167)", pid)
		enc.send(frame{Error: fmt.Sprintf("this runner serves only its launcher's fyrd, not pid %d", pid)})
		return
	}
	var f frame
	if err := readFrame(r, &f); err != nil {
		return
	}
	switch f.Op {
	case "list":
		s.mu.Lock()
		ps := make([]Proc, 0, len(s.procs))
		for _, p := range s.procs {
			ps = append(ps, p.info())
		}
		s.mu.Unlock()
		sort.Slice(ps, func(i, j int) bool { return ps[i].ID < ps[j].ID })
		enc.send(frame{OK: true, Version: Version, Procs: ps})
	case "spawn":
		if f.Spawn == nil {
			enc.send(frame{Error: "spawn without a spec"})
			return
		}
		p, err := s.spawn(*f.Spawn)
		if err != nil {
			var ex *ExistsError
			if errors.As(err, &ex) {
				enc.send(frame{Error: err.Error(), Exists: ex.ID})
			} else {
				enc.send(frame{Error: err.Error()})
			}
			return
		}
		s.link(p, r, enc)
	case "attach":
		s.mu.Lock()
		p := s.procs[f.ID]
		s.mu.Unlock()
		if p == nil {
			enc.send(frame{Error: "no process " + f.ID})
			return
		}
		s.link(p, r, enc)
	default:
		enc.send(frame{Error: "unknown op " + f.Op})
	}
}

// allowed applies Allow to the connecting process. A fyrd may connect a
// moment before the launcher has noted its pid, so a refusal is asked
// again for up to a second.
func (s *Server) allowed(c net.Conn) (int, bool) {
	if s.Allow == nil {
		return 0, true
	}
	pid, err := connPeerPid(c)
	if err != nil {
		return 0, true
	}
	for i := 0; i < 10; i++ {
		if s.Allow(pid) {
			return pid, true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return pid, false
}

func (p *rproc) info() Proc {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Proc{ID: p.id, Key: p.key, Pid: p.cmd.Process.Pid, Meta: p.meta, Started: p.started, Exited: p.exited, Linked: p.linked, InputClosed: p.inClosed}
}

// closeInput closes the CLI's stdin and remembers it (V0.142).
func (p *rproc) closeInput() {
	p.wmu.Lock()
	p.in.Close()
	p.wmu.Unlock()
	p.mu.Lock()
	p.inClosed = true
	p.mu.Unlock()
}

func (p *rproc) isExited() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exited
}

func (s *Server) spawn(sp Spawn) (*rproc, error) {
	if sp.ID == "" || sp.Binary == "" {
		return nil, errors.New("spawn needs an id and a binary")
	}
	// Rule (1): the check and the insert happen under one lock, so two
	// spawns for one key cannot both pass.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.procs[sp.ID] != nil {
		return nil, fmt.Errorf("process %s exists", sp.ID)
	}
	if sp.Key != "" {
		for _, o := range s.procs {
			if o.key == sp.Key && !o.isExited() {
				return nil, &ExistsError{ID: o.id, Key: sp.Key}
			}
		}
	}
	cmd := exec.Command(sp.Binary, sp.Args...)
	cmd.Dir, cmd.Env = sp.Dir, sp.Env
	proc.Group(cmd) // kill stops the CLI and what it started (#39)
	closeCg, err := proc.IntoCgroup(cmd, sp.Cgroup)
	if err != nil {
		return nil, fmt.Errorf("cgroup: %w", err)
	}
	defer closeCg()
	now := s.now()
	p := &rproc{id: sp.ID, key: sp.Key, meta: sp.Meta, started: now, alone: now, cmd: cmd, stderr: &tail{max: 4096}}
	p.cond = sync.NewCond(&p.mu)
	cmd.Stderr = p.stderr
	if p.in, err = cmd.StdinPipe(); err != nil {
		return nil, err
	}
	if p.out, err = cmd.StdoutPipe(); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s.procs[sp.ID] = p
	s.logf("runner: started %s (pid %d)", sp.ID, cmd.Process.Pid)
	go s.read(p)
	return p, nil
}

func (s *Server) read(p *rproc) {
	max := s.Buffer
	if max <= 0 {
		max = DefaultBuffer
	}
	sc := bufio.NewScanner(p.out)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		text := sc.Text()
		p.mu.Lock()
		p.seq++
		p.lines = append(p.lines, line{p.seq, text})
		p.size += len(text)
		for p.size > max && len(p.lines) > 1 {
			p.size -= len(p.lines[0].text)
			p.lines = p.lines[1:]
		}
		p.cond.Broadcast()
		p.mu.Unlock()
	}
	err := p.cmd.Wait()
	msg := ""
	if err != nil {
		msg = err.Error()
		if t := strings.TrimSpace(p.stderr.String()); t != "" {
			msg += ": " + t
		}
	}
	p.mu.Lock()
	p.exited, p.err = true, msg
	p.cond.Broadcast()
	p.mu.Unlock()
	s.logf("runner: %s exited %s", p.id, msg)
}

func (p *rproc) kill() {
	if p.cmd.Process != nil {
		syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	}
	// A child that left the group may hold stdout; the reader must end.
	p.out.Close()
}

// link makes the connection the process's one link: the runner sends every
// stored line, then each new line, then the exit; fyrd's frames come in on r.
func (s *Server) link(p *rproc, r *bufio.Reader, enc *encoder) {
	p.mu.Lock()
	p.att++
	gen := p.att
	p.linked = true
	p.closing = time.Time{} // a fyrd is back: rule (2) starts over
	p.cond.Broadcast()      // an older link's sender stops
	pid := p.cmd.Process.Pid
	p.mu.Unlock()
	if err := enc.send(frame{OK: true, Version: Version, Pid: pid, ID: p.id}); err != nil {
		s.unlink(p, gen)
		return
	}
	go s.ops(p, r, gen)

	var cursor int64
	exitSent := false
	for {
		p.mu.Lock()
		for p.att == gen && p.seq <= cursor && (!p.exited || exitSent) {
			p.cond.Wait()
		}
		if p.att != gen {
			p.mu.Unlock()
			return
		}
		var batch []line
		for _, l := range p.lines {
			if l.seq > cursor {
				batch = append(batch, l)
			}
		}
		if len(batch) == 0 && p.seq > cursor {
			cursor = p.seq // acked or dropped by the buffer limit
		}
		exited, errText := p.exited && len(batch) == 0, p.err
		p.mu.Unlock()
		for _, l := range batch {
			if enc.send(frame{Seq: l.seq, Text: l.text}) != nil {
				return
			}
			cursor = l.seq
		}
		if exited && !exitSent {
			exitSent = true
			if enc.send(frame{Exit: true, Err: errText}) != nil {
				return
			}
		}
	}
}

func (s *Server) unlink(p *rproc, gen int) {
	p.mu.Lock()
	if p.att == gen {
		p.att++ // the link is gone; its sender stops
		p.linked = false
		p.alone = s.now()
		p.cond.Broadcast()
	}
	p.mu.Unlock()
}

// ops reads fyrd's frames on a link until it ends.
func (s *Server) ops(p *rproc, r *bufio.Reader, gen int) {
	defer s.unlink(p, gen)
	for {
		var f frame
		if err := readFrame(r, &f); err != nil {
			return
		}
		p.mu.Lock()
		current := p.att == gen
		p.mu.Unlock()
		if !current && f.Op != "ack" {
			return // a newer link took over; a late ack is still true
		}
		switch f.Op {
		case "write":
			p.wmu.Lock()
			_, err := p.in.Write([]byte(f.Text + "\n"))
			p.wmu.Unlock()
			if err != nil {
				s.logf("runner: %s: a write to its input was lost: %v", p.id, err) // V0.142
			}
		case "ack":
			// Drop every line up to Seq that fyrd does not ask to keep.
			keep := make(map[int64]bool, len(f.Keep))
			for _, k := range f.Keep {
				keep[k] = true
			}
			p.mu.Lock()
			kept := p.lines[:0]
			for _, l := range p.lines {
				if l.seq > f.Seq || keep[l.seq] {
					kept = append(kept, l)
				} else {
					p.size -= len(l.text)
				}
			}
			p.lines = kept
			p.mu.Unlock()
		case "close":
			p.closeInput()
		case "kill":
			p.kill()
		case "forget":
			if !p.isExited() {
				p.kill()
			}
			s.mu.Lock()
			delete(s.procs, p.id)
			s.mu.Unlock()
			return
		}
	}
}

type encoder struct {
	mu sync.Mutex
	w  io.Writer
}

func (e *encoder) send(f frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, err = e.w.Write(append(b, '\n'))
	return err
}

func readFrame(r *bufio.Reader, f *frame) error {
	b, err := r.ReadBytes('\n')
	if err != nil {
		return err
	}
	return json.Unmarshal(b, f)
}

// tail keeps the last max bytes written to it.
type tail struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (t *tail) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, b...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(b), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
