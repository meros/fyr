package authtest

import (
	"context"
	"net"
)

func unixDialer(path string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}
}

// CleanEnv is env without any FYR_* variable (#167, V0.131).
func CleanEnv(env []string) []string { return cleanEnv(env) }
