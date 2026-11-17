package deploy

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// #148 (fixed for decision 0029 S2's flake): a build that answered /healthz with an error and then stopped answering
// is reported by its answer, not by the connection error that came after it.
func TestProbeHealthReportsTheLastAnswer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "the database does not answer", http.StatusServiceUnavailable)
	})}
	go srv.Serve(ln)
	go func() { time.Sleep(300 * time.Millisecond); srv.Close() }() // then: connection refused
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	err = probeHealth(ctx, addr)
	if err == nil || !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "database") {
		t.Fatalf("%v", err)
	}
}
