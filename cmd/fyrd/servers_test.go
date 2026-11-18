// SPEC 0.5 V0.182: fyrd's listeners go through one helper; the old host
// listeners still come and go with the peer way in's mode (decision 0030 5.1).
package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
)

func listen(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func get(addr string) (string, error) {
	r, err := http.Get("http://" + addr + "/")
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	return string(b), err
}

var hello = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "hi") })

func TestV182ServersServeAndShutDown(t *testing.T) {
	s := &servers{}
	l := listen(t)
	s.serve(l, hello)
	if b, err := get(l.Addr().String()); err != nil || b != "hi" {
		t.Fatalf("%q %v", b, err)
	}
	s.shutdown(context.Background())
	if _, err := get(l.Addr().String()); err == nil {
		t.Fatal("still serving after shutdown")
	}
}

func TestV182ServersReportAServerThatStops(t *testing.T) {
	errs := make(chan error, 1)
	s := &servers{onErr: func(err error) { errs <- err }}
	l := listen(t)
	l.Close() // Serve fails at once
	s.serve(l, hello)
	if err := <-errs; err == nil {
		t.Fatal("no error")
	}
}

func TestV182OldListenersFollowTheMode(t *testing.T) {
	var addr string
	opened := 0
	o := &oldListeners{}
	o.open = func() *servers {
		opened++
		s := &servers{}
		l := listen(t)
		addr = l.Addr().String()
		s.serve(l, hello)
		return s
	}
	o.Set(true)
	o.Set(true) // already open: not again
	if opened != 1 {
		t.Fatalf("opened %d times", opened)
	}
	if b, err := get(addr); err != nil || b != "hi" {
		t.Fatalf("%q %v", b, err)
	}
	o.Set(false)
	if _, err := get(addr); err == nil {
		t.Fatal("enforce left the old listener open")
	}
	o.Set(true)
	if opened != 2 {
		t.Fatalf("observe did not bring them back (%d)", opened)
	}
	o.Close(context.Background())
}
