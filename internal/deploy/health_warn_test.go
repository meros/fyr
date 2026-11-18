package deploy

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// SPEC V0.180: a Warning is said on /healthz and keeps it green; a real failure
// after it still turns it red.
func TestHealthWarningStaysGreen(t *testing.T) {
	get := func(checks ...Check) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		Health(http.NotFoundHandler(), checks...).ServeHTTP(rec, httptest.NewRequest("GET", HealthPath, nil))
		return rec
	}
	warn := func() error { return Warning("config changed; restart fyrd to apply") }
	if r := get(warn); r.Code != 200 || r.Body.String() != "healthy\nwarning: config changed; restart fyrd to apply\n" {
		t.Errorf("%d %q", r.Code, r.Body.String())
	}
	if r := get(warn, func() error { return errors.New("the database does not answer") }); r.Code != 503 || !strings.Contains(r.Body.String(), "database") {
		t.Errorf("%d %q", r.Code, r.Body.String())
	}
	if r := get(); r.Body.String() != "healthy\n" {
		t.Errorf("%q", r.Body.String())
	}
}
