package runner

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"

	"github.com/meros/fyr/internal/proc"
)

// Start starts s.Binary with s.Args under wrap (a command line that puts it
// in a scope of its own, V0.agents-unit; nil for none): in the runner rc
// when it is set, so the process outlives fyrd (V0.90), else as fyrd's own
// child (tests, a run without the launcher). Every agent CLI starts here.
func Start(rc *Client, s Spawn, wrap []string) (Pipe, error) {
	if len(wrap) > 0 {
		s.Args = append(append(append([]string{}, wrap[1:]...), s.Binary), s.Args...)
		s.Binary = wrap[0]
	}
	if rc != nil {
		p, err := rc.Spawn(s)
		if err != nil {
			return nil, err // not a nil *Conn in a non-nil Pipe
		}
		return p, nil
	}
	return StartLocal(s.Binary, s.Args, s.Dir, s.Env, s.Cgroup)
}

// StartLocal starts a CLI as fyrd's own child and returns the pipe to it:
// the way before V0.90, and the way without a runner. A line carries no
// sequence number and Ack does nothing.
func StartLocal(bin string, args []string, dir string, env []string, cgroup string) (Pipe, error) {
	cmd := command(bin, args, env) // the agent's PATH (V0.runner-lookpath)
	cmd.Dir, cmd.Env = dir, env
	proc.Group(cmd) // Kill stops the CLI and what it started (#39)
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

// child is the pipe to a CLI that runs as fyrd's own child.
type child struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    io.ReadCloser
	sc     *bufio.Scanner
	stderr *tail
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

// Kill also closes the read end of stdout, so ReadLine ends even when a
// child that left the process group still holds the pipe open (#39).
func (c *child) Kill() {
	proc.Kill(c.cmd)
	c.out.Close()
}

func (c *child) Ack(int64, []int64) {}
func (c *child) Pid() int           { return c.cmd.Process.Pid }
