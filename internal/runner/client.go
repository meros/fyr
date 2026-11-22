package runner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Client is fyrd's side: it lists, starts and attaches to processes.
type Client struct {
	Path string // the runner's socket
}

func (c *Client) dial() (net.Conn, error) {
	return net.DialTimeout("unix", c.Path, 5*time.Second)
}

// ListWait is List, retried for up to wait while the runner does not
// answer: the launcher starts it before fyrd, but a fyrd must never miss
// the processes it holds (V0.90 (3)).
func (c *Client) ListWait(wait time.Duration) ([]Proc, error) {
	return c.ListWaitContext(context.Background(), wait)
}

// ListWaitContext is ListWait that also stops when ctx ends.
func (c *Client) ListWaitContext(ctx context.Context, wait time.Duration) ([]Proc, error) {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	retry := time.NewTicker(200 * time.Millisecond)
	defer retry.Stop()
	for {
		ps, err := c.List()
		var ve *VersionError
		if err == nil || errors.As(err, &ve) {
			return ps, err
		}
		select {
		case <-ctx.Done():
			return ps, err
		case <-retry.C:
		}
	}
}

// VersionError: the runner speaks another protocol version.
type VersionError struct{ Runner, Fyrd int }

func (e *VersionError) Error() string {
	return fmt.Sprintf("runner speaks protocol %d, this fyrd %d", e.Runner, e.Fyrd)
}

// List returns the processes the runner holds. An error, or a runner with
// another protocol version, means fyrd must not use it.
func (c *Client) List() ([]Proc, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err := (&encoder{w: conn}).send(frame{Op: "list", Version: Version}); err != nil {
		return nil, err
	}
	var f frame
	if err := readFrame(bufio.NewReader(conn), &f); err != nil {
		return nil, err
	}
	if f.Error != "" {
		return nil, errors.New(f.Error)
	}
	if f.Version != Version {
		return nil, &VersionError{Runner: f.Version, Fyrd: Version}
	}
	return f.Procs, nil
}

// Spawn starts a process in the runner and links to it.
func (c *Client) Spawn(sp Spawn) (*Conn, error) {
	return c.open(frame{Op: "spawn", Version: Version, Spawn: &sp}, sp.ID)
}

// Attach links to a process the runner holds. It replaces any other link.
func (c *Client) Attach(id string) (*Conn, error) {
	return c.open(frame{Op: "attach", Version: Version, ID: id}, id)
}

func (c *Client) open(first frame, id string) (*Conn, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}
	enc := &encoder{w: conn}
	r := bufio.NewReaderSize(conn, 1<<20)
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err := enc.send(first); err != nil {
		conn.Close()
		return nil, err
	}
	var f frame
	if err := readFrame(r, &f); err != nil {
		conn.Close()
		return nil, err
	}
	if f.Exists != "" {
		conn.Close()
		return nil, &ExistsError{ID: f.Exists, Key: first.Spawn.Key}
	}
	if f.Error != "" {
		conn.Close()
		return nil, errors.New(f.Error)
	}
	conn.SetDeadline(time.Time{})
	return &Conn{id: id, conn: conn, r: r, enc: enc, pid: f.Pid}, nil
}

// Conn is the link to one process the runner holds. It is a Pipe.
type Conn struct {
	id   string
	conn net.Conn
	r    *bufio.Reader
	enc  *encoder
	pid  int

	mu       sync.Mutex
	exitErr  error
	detached atomic.Bool
}

func (c *Conn) ID() string { return c.id }
func (c *Conn) Pid() int   { return c.pid }

func (c *Conn) WriteLine(b []byte) error { return c.enc.send(frame{Op: "write", Text: string(b)}) }

// ReadLine returns the next line. After Detach it never returns: the
// process lives on for the next fyrd.
func (c *Conn) ReadLine() ([]byte, int64, error) {
	for {
		var f frame
		if err := readFrame(c.r, &f); err != nil {
			if c.detached.Load() {
				select {}
			}
			c.mu.Lock()
			c.exitErr = fmt.Errorf("lost the link to the runner: %w", err)
			c.mu.Unlock()
			return nil, 0, io.EOF
		}
		if f.Exit {
			c.mu.Lock()
			if f.Err != "" {
				c.exitErr = errors.New(f.Err)
			}
			c.mu.Unlock()
			return nil, 0, io.EOF
		}
		if f.Seq > 0 {
			return []byte(f.Text), f.Seq, nil
		}
	}
}

func (c *Conn) Wait() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exitErr
}

func (c *Conn) CloseInput() error { return c.enc.send(frame{Op: "close"}) }
func (c *Conn) Kill()             { c.enc.send(frame{Op: "kill"}) }
func (c *Conn) Ack(seq int64, keep []int64) {
	if seq > 0 {
		c.enc.send(frame{Op: "ack", Seq: seq, Keep: keep})
	}
}

// Forget tells the runner that fyrd saw the exit: it drops the process.
func (c *Conn) Forget() {
	c.enc.send(frame{Op: "forget"})
	c.conn.Close()
}

// Detach ends the link and leaves the process running, for fyrd's
// shutdown: the next fyrd attaches.
func (c *Conn) Detach() {
	c.detached.Store(true)
	c.conn.Close()
}

func (c *Conn) Close() error { return c.conn.Close() }
