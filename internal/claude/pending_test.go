// review 2026-11-15: a control request that times out or fails to write
// leaves no entry in pending.
package claude

import (
	"context"
	"errors"
	"io"
	"testing"
)

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestRequestForgetsPending(t *testing.T) {
	c := &Client{stdin: nopCloser{io.Discard}, pending: map[string]chan Msg{}, done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Request(ctx, Msg{"subtype": "interrupt"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if n := len(c.pending); n != 0 {
		t.Fatalf("timed-out request left %d pending", n)
	}
	c.stdin = nopCloser{failWriter{}}
	if _, err := c.Request(context.Background(), Msg{"subtype": "interrupt"}); err == nil {
		t.Fatal("write error not returned")
	}
	if n := len(c.pending); n != 0 {
		t.Fatalf("failed write left %d pending", n)
	}
}
