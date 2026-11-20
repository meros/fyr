package agycli

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/meros/fyr/internal/proc"
)

// child is the pipe to an `agy` process running directly as fyrd's child
// (tests and runs without a launcher runner).
type child struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    io.ReadCloser
	sc     *bufio.Scanner
	stderr *tail
}

func startChild(bin string, args []string, dir string, env []string, cgroup string) (*child, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir, cmd.Env = dir, env
	proc.Group(cmd) // Kill stops the CLI and everything it started (#39)
	closeCg, err := proc.IntoCgroup(cmd, cgroup)
	if err != nil {
		return nil, fmt.Errorf("cgroup: %w", err)
	}
	defer closeCg()
	c := &child{cmd: cmd, stderr: &tail{max: 4096}}
	cmd.Stderr = c.stderr
	if c.in, err = cmd.StdinPipe(); err != nil {
		return nil, err
	}
	if c.out, err = cmd.StdoutPipe(); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c.sc = bufio.NewScanner(c.out)
	c.sc.Buffer(make([]byte, 1<<20), 64<<20)
	return c, nil
}

func (c *child) WriteLine(b []byte) error {
	_, err := c.in.Write(append(b, '\n'))
	return err
}

func (c *child) ReadLine() ([]byte, int64, error) {
	if c.sc.Scan() {
		return append([]byte(nil), c.sc.Bytes()...), 0, nil
	}
	return nil, 0, io.EOF
}

func (c *child) Wait() error {
	err := c.cmd.Wait()
	if err != nil {
		if s := c.stderr.String(); s != "" {
			err = fmt.Errorf("%w: %s", err, s)
		}
	}
	return err
}

func (c *child) CloseInput() error { return c.in.Close() }

func (c *child) Kill() {
	proc.Kill(c.cmd)
	c.out.Close()
}

func (c *child) Ack(int64, []int64) {}
func (c *child) Pid() int           { return c.cmd.Process.Pid }

type tail struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
