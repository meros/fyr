package authtest

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
)

func unixDialer(path string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}
}

// CleanEnv is env without any FYR_* variable (#167, V0.131).
func CleanEnv(env []string) []string { return cleanEnv(env) }

func newRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, err
}

// OSUID is the user the test runs as.
func OSUID() int { return os.Getuid() }
