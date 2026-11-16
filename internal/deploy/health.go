package deploy

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// #148: a new build counts as good when it serves, not only when it starts.
// fyrd answers GET /healthz on its main (loopback) listener; the launcher
// asks it after the ready file appears and rolls back when it stays red.

// HealthPath is the health endpoint.
const HealthPath = "/healthz"

// Check is one health check: nil is healthy.
type Check func() error

// Health serves HealthPath from checks and passes everything else to next.
// It sits outside the owner guard: it says only "ok" or why not.
func Health(next http.Handler, checks ...Check) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != HealthPath {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		for _, c := range checks {
			if err := c(); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprintln(w, err)
				return
			}
		}
		fmt.Fprintln(w, "ok")
	})
}

// readyAddr reads the address MarkReady wrote after the pid, or "".
func readyAddr(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	f := strings.Fields(string(b))
	if len(f) < 2 {
		return ""
	}
	return f[1]
}

// probeHealth asks http://addr/healthz until it answers 200 or ctx ends; it
// returns the last reason it was not healthy.
func probeHealth(ctx context.Context, addr string) error {
	c := &http.Client{Timeout: 3 * time.Second}
	last := fmt.Errorf("no answer from %s%s", addr, HealthPath)
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+HealthPath, nil)
		if resp, err := c.Do(req); err == nil {
			var b [512]byte
			n, _ := resp.Body.Read(b[:])
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			last = fmt.Errorf("%s answered %d: %s", HealthPath, resp.StatusCode, strings.TrimSpace(string(b[:n])))
		} else {
			last = fmt.Errorf("%s: %v", HealthPath, err)
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(200 * time.Millisecond):
		}
	}
}
