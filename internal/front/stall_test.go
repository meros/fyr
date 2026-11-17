package front

// SPEC 0.5 V0.181 (#267): a stalled host is an offline host. When the host in
// this process answers a call with the deadline's 503 (V0.147), or sends a
// stall frame and then does not answer, the front marks it offline and serves
// its cached state, as for a remote host that went away. The other hosts, the
// browsers' streams and the process stay up; the host is back when it answers.

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/lockwatch"
	"github.com/meros/fyr/internal/pipe"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/store"
)

// stuckHost is a real host reached over the in-process pipe whose engine can
// be made to hang as with its lock held: every call but the stream (which
// takes no engine lock) waits and then gets the deadline's 503.
type stuckHost struct {
	eng   *engine.Engine
	ln    *pipe.Listener
	stuck atomic.Bool
}

func newStuckHost(t *testing.T, name, id string) *stuckHost {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := &stuckHost{eng: engine.New(st), ln: pipe.New()}
	inner := (&server.Server{Engine: h.eng, BuildID: "host-build", Host: name, ID: id}).Handler()
	hs := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.stuck.Load() && r.URL.Path != "/api/stream" {
			time.Sleep(30 * time.Millisecond) // the deadline, shortened
			server.WriteDeadline503(w)
			return
		}
		inner.ServeHTTP(w, r)
	})}
	go hs.Serve(h.ln)
	t.Cleanup(func() { hs.Shutdown(context.Background()) })
	return h
}

type stallHostRow struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Online bool           `json:"online"`
	Error  string         `json:"error"`
}

func frontHosts(t *testing.T, base string) map[string]stallHostRow {
	var st struct{ Hosts []stallHostRow }
	getJSON(t, base+"/api/front/state", &st)
	out := map[string]stallHostRow{}
	for _, h := range st.Hosts {
		out[h.Name] = h
	}
	return out
}

func TestV181StalledLocalHostIsOfflineAndTheFrontStaysUp(t *testing.T) {
	old := stallProbeEvery
	stallProbeEvery = 50 * time.Millisecond
	t.Cleanup(func() { stallProbeEvery = old })
	const idDesk = "DESK-1111-DESK-1111"
	desk := newStuckHost(t, "desk", idDesk)
	srv := newHost(t, "srv")
	fs := newFront(t, HostConfig{Name: "desk", URL: LocalURL, Dial: desk.ln.Dial}, HostConfig{Name: "srv", URL: srv.hs.URL})
	online(t, fs.URL, 2)
	code, rep := post(t, fs.URL+"/api/command", `{"commandId":"c1","action":"thread.create","args":{"cwd":"/tmp","text":"hi","host":"desk"}}`)
	deskThread, _ := rep["threadId"].(string)
	if code != 200 || deskThread == "" {
		t.Fatalf("create on desk: %d %v", code, rep)
	}
	eventually(t, "desk's thread in the front", func() bool { return threadOf(getState(t, fs.URL), deskThread) != nil })

	// A browser's stream, open across the stall.
	req, _ := http.NewRequest("GET", fs.URL+"/api/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	frames := make(chan string, 256)
	go func() {
		defer close(frames)
		rd := bufio.NewReader(resp.Body)
		ev := ""
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\n")
			switch {
			case strings.HasPrefix(line, "event: "):
				ev = line[7:]
			case strings.HasPrefix(line, "data: ") && ev != "":
				frames <- ev + " " + line[6:]
			}
		}
	}()

	offlineAndServed := func(why string) {
		t.Helper()
		eventually(t, "desk offline ("+why+")", func() bool {
			hs := frontHosts(t, fs.URL)
			return !hs["desk"].Online && hs["srv"].Online
		})
		d := threadOf(getState(t, fs.URL), deskThread)
		if d == nil || d["hostOffline"] == nil {
			t.Fatalf("desk's thread is not marked offline (%s): %v", why, d)
		}
		// the pipe is up; the host is what is stuck, and says so
		if h := frontHosts(t, fs.URL)["desk"]; !strings.HasPrefix(h.Error, "stalled: ") {
			t.Fatalf("desk's error (%s): %+v", why, h)
		}
		// desk's state comes from the front's cache, at once
		code, hdr, b := probeGet(t, fs.URL+"/api/h/"+idDesk+"/state")
		if code != 200 || hdr.Get("X-Fyr-Read-At") == "" || !strings.Contains(string(b), deskThread) {
			t.Fatalf("desk's cached state (%s): %d %v %.300s", why, code, hdr, b)
		}
		// a command for desk is refused as for any offline host
		code, b = probePost(t, fs.URL+"/api/h/"+idDesk+"/command", `{"commandId":"c-`+why+`","action":"thread.send","threadId":"`+deskThread+`","args":{"text":"x"}}`)
		var r struct{ Code string }
		json.Unmarshal(b, &r)
		if code != 503 || r.Code != "host-offline" {
			t.Fatalf("a command for the stalled desk (%s): %d %s", why, code, b)
		}
		// the other host is served as before
		code, rep := post(t, fs.URL+"/api/command", `{"commandId":"s-`+why+`","action":"thread.create","args":{"cwd":"/tmp","text":"hi","host":"srv"}}`)
		if code != 200 || rep["threadId"] == nil {
			t.Fatalf("srv while desk is stalled (%s): %d %v", why, code, rep)
		}
	}
	back := func(why string) {
		t.Helper()
		desk.stuck.Store(false)
		eventually(t, "desk back ("+why+")", func() bool { return frontHosts(t, fs.URL)["desk"].Online })
		eventually(t, "desk's thread unmarked ("+why+")", func() bool {
			d := threadOf(getState(t, fs.URL), deskThread)
			return d != nil && d["hostOffline"] == nil
		})
	}

	// (1) A call answered with the deadline's 503.
	desk.stuck.Store(true)
	code, b := probePost(t, fs.URL+"/api/h/"+idDesk+"/command", `{"commandId":"c2","action":"thread.send","threadId":"`+deskThread+`","args":{"text":"x"}}`)
	if code != 503 {
		t.Fatalf("the stuck call: %d %s", code, b)
	}
	offlineAndServed("503")
	back("503")

	// (2) The engine reports a stall and its host does not answer.
	desk.stuck.Store(true)
	desk.eng.SetStall(lockwatch.Stall{At: time.Now().UTC(), What: "lock engine.mu held 10s"})
	offlineAndServed("stall")
	back("stall")

	// (3) A stall the host recovers from at once does not mark it offline.
	desk.eng.SetStall(lockwatch.Stall{At: time.Now().UTC(), What: "operation GET /api/x held 10s"})
	time.Sleep(200 * time.Millisecond)
	if !frontHosts(t, fs.URL)["desk"].Online {
		t.Fatal("an answering host was marked offline after a stall frame")
	}

	// The browser's stream stayed open throughout and saw desk go and come back.
	sawOff, sawOn := false, false
	for drained := false; !drained; {
		select {
		case fr, ok := <-frames:
			if !ok {
				t.Fatal("the browser's stream ended during the stall")
			}
			// a thread or a patch of it: "hostOffline":"<since>" when it goes, null when it is back
			if strings.HasPrefix(fr, "thread") && strings.Contains(fr, deskThread) {
				switch {
				case strings.Contains(fr, `"hostOffline":"`):
					sawOff = true
				case sawOff && (strings.Contains(fr, `"hostOffline":null`) || !strings.Contains(fr, `"hostOffline"`)):
					sawOn = true
				}
			}
		default:
			drained = true
		}
	}
	if !sawOff || !sawOn {
		t.Fatalf("the stream did not carry desk's offline and back: off %v on %v", sawOff, sawOn)
	}
}
