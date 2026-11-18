package peerwire

// SPEC V0.180 (#261): the owner route passes the inviter's status through; a
// failure of this install stays 409.

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/peer"
)

func TestJoinStatusPassesTheInvitersThrough(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{&peer.JoinRefused{Status: 403, Msg: "x"}, 403},
		{fmt.Errorf("wrapped: %w", &peer.JoinRefused{Status: 400, Msg: "x"}), 400},
		{&peer.JoinRefused{Status: 500, Msg: "x"}, 502}, // the inviter's fault is not this install's
		{errors.New("no address to try"), 409},
	} {
		if got := joinStatus(tc.err); got != tc.want {
			t.Errorf("%v: %d, want %d", tc.err, got, tc.want)
		}
	}
}

// SPEC V0.180 (#264): fyr host list carries the config note.
func TestHostListCarriesTheConfigNote(t *testing.T) {
	r := newRig(t)
	mux := http.NewServeMux()
	OwnerRoutes(Owner{Env: r.env, Key: r.key, Data: r.dir, ConfigNote: func() string { return "config changed; restart fyrd to apply" }})(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/host/list", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"configChanged":"config changed; restart fyrd to apply"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
