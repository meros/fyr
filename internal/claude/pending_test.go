// review 2026-11-15: a control request that times out or fails to write
// leaves no entry in pending.
package claude

import (
	"context"
	"errors"
	"io"
	"testing"
)

// linePipe writes to w and reads nothing.
type linePipe struct{ w io.Writer }

func (p linePipe) WriteLine(b []byte) error       { _, err := p.w.Write(b); return err }
func (linePipe) ReadLine() ([]byte, int64, error) { return nil, 0, io.EOF }
func (linePipe) Wait() error                      { return nil }
func (linePipe) CloseInput() error                { return nil }
func (linePipe) Kill()                            {}
func (linePipe) Ack(int64, []int64)               {}
func (linePipe) Pid() int                         { return 0 }

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestRequestForgetsPending(t *testing.T) {
	c := &Client{pipe: linePipe{io.Discard}, pending: map[string]chan Msg{}, done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Request(ctx, Msg{"subtype": "interrupt"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if n := len(c.pending); n != 0 {
		t.Fatalf("timed-out request left %d pending", n)
	}
	c.pipe = linePipe{failWriter{}}
	if _, err := c.Request(context.Background(), Msg{"subtype": "interrupt"}); err == nil {
		t.Fatal("write error not returned")
	}
	if n := len(c.pending); n != 0 {
		t.Fatalf("failed write left %d pending", n)
	}
}
