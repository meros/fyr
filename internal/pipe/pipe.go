// Package pipe is an in-process listener: a front reaches the host in its
// own fyrd through it, so no loopback port is open for any local process
// to drive (SPEC V0.129 (3), decision 0028).
package pipe

import (
	"context"
	"errors"
	"net"
	"sync"
)

// Listener hands each Dial's other end to Accept.
type Listener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func New() *Listener {
	return &Listener{conns: make(chan net.Conn), done: make(chan struct{})}
}

func (l *Listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *Listener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *Listener) Addr() net.Addr { return addr{} }

// Dial connects to the listener; the network and address are ignored, so
// it fits http.Transport.DialContext.
func (l *Listener) Dial(ctx context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-l.done:
		client.Close()
		server.Close()
		return nil, errors.New("pipe: listener closed")
	case <-ctx.Done():
		client.Close()
		server.Close()
		return nil, ctx.Err()
	}
}

type addr struct{}

func (addr) Network() string { return "pipe" }
func (addr) String() string  { return "in-process" }
