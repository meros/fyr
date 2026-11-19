package front

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/meros/fyr/internal/store"
)

// V0.client-refs, decision 0031 test 6: a host that comes back with a new id
// is listed with idWas, and /api/h/<idWas>/… still reaches it while it is,
// so a client that has not yet moved its keys is not cut off. An id no host
// has, or had, is still 404 no-host.
func TestOldIDReachesTheHostDuringARotation(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "front.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	const was = "OLDD-0000-OLDD-0000"
	if err := st.SetKV(hostIDsKey, `{"one":"`+was+`"}`); err != nil {
		t.Fatal(err)
	}
	one, two := newFakeHost(t, "one", idOne), newFakeHost(t, "two", idTwo)
	f := New(Options{ID: "FRNT-0000-0000-0000", Name: "front", BuildID: "front-build", Store: st, MinBackoff: 50 * time.Millisecond,
		Web: fstest.MapFS{"index.html": {Data: []byte("<title>Fyr</title>")}}},
		[]HostConfig{{Name: "one", URL: one.srv.URL}, {Name: "two", URL: two.srv.URL}})
	ctx, cancel := context.WithCancel(context.Background())
	go f.Run(ctx)
	fs := httptest.NewServer(f.Handler())
	t.Cleanup(func() { cancel(); fs.CloseClientConnections(); fs.Close() })
	eventually(t, "one listed with its new id and idWas", func() bool {
		h := hostRow(t, fs.URL, "one")
		return h != nil && h["online"] == true && h["id"] == idOne && h["idWas"] == was
	})

	for _, id := range []string{idOne, was} {
		code, _, b := probeGet(t, fs.URL+"/api/h/"+id+"/threads/t_same/events")
		var out struct{ Host string }
		json.Unmarshal(b, &out)
		if code != 200 || out.Host != "one" {
			t.Errorf("through %s: %d %s", id, code, b)
		}
	}
	if n := len(two.probed()); n != 0 {
		t.Errorf("the old id reached the other host %d times", n)
	}
	if code, _, _ := probeGet(t, fs.URL+"/api/h/ZZZZ-9999-ZZZZ-9999/state"); code != 404 {
		t.Errorf("an id no host had: %d", code)
	}
}
