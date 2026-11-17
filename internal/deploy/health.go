package deploy

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
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

// Warning is a check's answer that is said on /healthz and keeps it green
// (V0.180: a config that changed since fyrd read it). The launcher trusts a build
// by the status only.
type Warning string

func (w Warning) Error() string { return string(w) }

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
		var warns []string
		for _, c := range checks {
			err := c()
			if wn, ok := err.(Warning); ok {
				warns = append(warns, string(wn))
				continue
			}
			if err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprintln(w, err)
				return
			}
		}
		fmt.Fprintln(w, "healthy")
		for _, wn := range warns {
			fmt.Fprintln(w, "warning:", wn)
		}
	})
}

// readyAddr reads the address MarkReady wrote after the pid, or "".
func readyAddr(path string, pid int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	f := strings.Fields(string(b))
	if len(f) < 2 || f[0] != strconv.Itoa(pid) {
		return "" // #167: another process's address is not this build's
	}
	return f[1]
}

// readyPid is the pid a ready file names; ok is false while it does not
// exist or is half written.
func readyPid(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0, false
	}
	pid, err := strconv.Atoi(f[0])
	return pid, err == nil
}

// probeHealth asks http://addr/healthz until it answers 200 or ctx ends; it
// returns the last reason it was not healthy.
func probeHealth(ctx context.Context, addr string) error {
	c := &http.Client{Timeout: 3 * time.Second}
	last := fmt.Errorf("no answer from %s%s", addr, HealthPath)
	answered := false // an answer names the cause; a later connection error does not replace it
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+HealthPath, nil)
		if resp, err := c.Do(req); err == nil {
			var b [512]byte
			n, _ := resp.Body.Read(b[:])
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			last, answered = fmt.Errorf("%s answered %d: %s", HealthPath, resp.StatusCode, strings.TrimSpace(string(b[:n]))), true
		} else if !answered {
			last = fmt.Errorf("%s: %v", HealthPath, err)
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(200 * time.Millisecond):
		}
	}
}
