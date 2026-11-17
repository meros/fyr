package authtest_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/meros/fyr/internal/authtest"
)

func withIP(n authtest.Node, ip string) authtest.Node { n.IP = ip; return n }

// newLoopbackServer serves h on a loopback port and returns its URL.
func newLoopbackServer(t *testing.T, h http.Handler) string {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s.URL
}
