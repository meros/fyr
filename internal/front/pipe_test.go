package front

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/pipe"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/store"
)

// SPEC V0.129 (3), decision 0028 slice 0: in the both role the front reaches
// its own host in-process. The link's URL names no listening port; the
// front dials the pipe, and commands and state go through it as over TCP.
func TestV129FrontFollowsAHostOverAPipe(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	eng := engine.New(st)
	ln := pipe.New()
	hs := &http.Server{Handler: (&server.Server{Engine: eng, BuildID: "host-build", Host: "desk", ID: testID("desk")}).Handler()}
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Shutdown(context.Background()) })

	fs := newFront(t, HostConfig{Name: "desk", URL: LocalURL, Dial: ln.Dial})
	online(t, fs.URL, 1)
	code, rep := post(t, hostURL(t, fs.URL, "desk")+"/command", `{"commandId":"c1","action":"thread.create","args":{"cwd":"/tmp","text":"hi"}}`)
	id, _ := rep["threadId"].(string)
	if code != 200 || id == "" {
		t.Fatalf("create through the pipe: %d %v", code, rep)
	}
	if _, ok := eng.Thread(id); !ok {
		t.Fatalf("the host has no thread %s", id)
	}
	// The front learns the new thread from the host's stream, a moment after the command's reply: wait for it.
	eventually(t, "the thread in the front's state through the pipe", func() bool {
		got := getState(t, fs.URL)
		return len(got.Threads) == 1 && got.Threads[0]["host"] == "desk"
	})
}
