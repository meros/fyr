package front

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/store"
)

// V0.preview-host-prefix: a folder preview on a host that is not the front's
// first (a phone reads the laptop's thread through the server's front) opens:
// the redirect to its page stays under the host's prefix, the page's files
// are read there, and the CSP names that folder. Before, the redirect went to
// /api/previews/…, which the front sent to its first host: 404.
func TestFolderPreviewOpensOnAHostThatIsNotTheFirst(t *testing.T) {
	type host struct {
		id  string
		att *attach.Store
		url string
	}
	start := func(name, id string) host {
		st, err := store.Open(filepath.Join(t.TempDir(), "db"))
		if err != nil {
			t.Fatal(err)
		}
		att := &attach.Store{Dir: t.TempDir()}
		s := &server.Server{Engine: engine.New(st), BuildID: "host-build", Host: name, ID: id, Attach: att}
		hs := httptest.NewServer(s.PipeHandler())
		t.Cleanup(func() { hs.Close(); st.Close() })
		return host{id, att, hs.URL}
	}
	first, laptop := start("server", idOne), start("laptop", idTwo)

	root := t.TempDir()
	for p, c := range map[string]string{"index.html": `<script src="app.js"></script>`, "app.js": "js of the laptop"} {
		os.WriteFile(filepath.Join(root, p), []byte(c), 0o644)
	}
	list, _, err := attach.ScanTree(root)
	if err != nil {
		t.Fatal(err)
	}
	m, err := laptop.att.SaveTree("site", "index.html", list)
	if err != nil {
		t.Fatal(err)
	}

	f := New(Options{ID: "FRNT-0000-0000-0000", Name: "front", BuildID: "front-build", MinBackoff: 50 * time.Millisecond,
		Web: fstest.MapFS{"index.html": {Data: []byte("<title>Fyr</title>")}}},
		[]HostConfig{{Name: "server", URL: first.url}, {Name: "laptop", URL: laptop.url}})
	ctx, cancel := context.WithCancel(context.Background())
	go f.Run(ctx)
	fs := httptest.NewServer(f.Handler())
	t.Cleanup(func() { cancel(); fs.CloseClientConnections(); fs.Close() })
	online(t, fs.URL, 2)
	eventually(t, "the laptop is known by its id", func() bool { return f.byID(idTwo) != nil })

	// What the client's iframe loads, redirects followed as a browser does.
	page := fs.URL + "/api/h/" + idTwo + "/previews/" + m.ID
	r, err := http.Get(page)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	folder := "/api/h/" + idTwo + "/previews/" + m.ID + "/"
	if r.StatusCode != 200 || r.Request.URL.Path != folder+"index.html" || !strings.Contains(string(body), `src="app.js"`) {
		t.Fatalf("the page: %d at %s: %q", r.StatusCode, r.Request.URL.Path, body)
	}
	if csp := r.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src "+fs.URL+folder+" ") {
		t.Fatalf("the CSP does not name the folder the browser reads: %q", csp)
	}
	r, err = http.Get(fs.URL + folder + "app.js")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 200 || string(body) != "js of the laptop" {
		t.Fatalf("the page's file: %d %q", r.StatusCode, body)
	}

	// A browser cannot name a prefix of its own: the listener refuses it.
	req, _ := http.NewRequest("GET", page, nil)
	req.Header.Set("Fyr-Prefix", "/api/h/EVIL")
	r, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 400 {
		t.Fatalf("Fyr-Prefix from a browser: %d", r.StatusCode)
	}
}
