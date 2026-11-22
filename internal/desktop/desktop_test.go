package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExternalLinksLeaveForTheBrowser(t *testing.T) {
	o := "http://fyr.localhost:7777"
	for link, want := range map[string]bool{
		"http://fyr.localhost:7777/?open=a":        false,
		"http://FYR.localhost:7777/api/previews/x": false,
		"about:blank":                         false,
		"blob:http://fyr.localhost:7777/123":  false,
		"https://github.com/meros/fyr/pull/1": true,
		"http://fyr.localhost:8888/":          true, // another install's port is another app
		"mailto:a@example.com":                true,
	} {
		if got := External(o, link); got != want {
			t.Errorf("External(%q) = %v, want %v", link, got, want)
		}
	}
	if OriginOf("http://fyr.localhost:7777/auth/once#tok") != o {
		t.Fatal("origin of the once link")
	}
}

// V0.desktop-popup-links: a window the page asks for is Fyr's own when its
// link is a Fyr page or an empty, about:, data: or blob: one; every other
// link leaves for the default browser.
func TestChildWindowsAreFyrPagesOnly(t *testing.T) {
	o := "http://fyr.localhost:7777"
	for link, want := range map[string]bool{
		"http://fyr.localhost:7777/?preview=x":     true,
		"http://FYR.localhost:7777/api/previews/x": true,
		"about:blank":                         true,
		"blob:http://fyr.localhost:7777/123":  true,
		"https://github.com/meros/fyr/pull/1": false,
		"http://localhost:9999/":              false,
		"mailto:a@example.com":                false,
		"javascript:void(0)":                  false,
	} {
		if got := ChildWindow(o, link); got != want {
			t.Errorf("ChildWindow(%q) = %v, want %v", link, got, want)
		}
	}
}

func TestSigninAsksAtMostOncePerGap(t *testing.T) {
	var s Signin
	t0 := time.Now()
	if !s.Allow(t0) || s.Allow(t0.Add(10*time.Second)) || !s.Allow(t0.Add(31*time.Second)) {
		t.Fatal("one link, then none within 30 s, then one again")
	}
}

func TestPrefsKeepTheWindowSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fyr", "desktop.json")
	if p := LoadPrefs(path); p.Window != DefaultGeometry {
		t.Fatalf("no file: %+v", p)
	}
	if err := (Prefs{Window: Geometry{Width: 900, Height: 700, Maximized: true}, Origin: "http://fyr.localhost:7777"}).Save(path); err != nil {
		t.Fatal(err)
	}
	if p := LoadPrefs(path); p.Window.Width != 900 || !p.Window.Maximized || p.Origin != "http://fyr.localhost:7777" {
		t.Fatalf("read back %+v", p)
	}
	(Prefs{Window: Geometry{Width: 10, Height: 10}}).Save(path)
	if p := LoadPrefs(path); p.Window != DefaultGeometry {
		t.Fatalf("a window too small to use: %+v", p)
	}
}

func TestOpenPathOpensTheThread(t *testing.T) {
	if p := OpenPath("IDBB/t_1"); p != "/?open=IDBB%2Ft_1" {
		t.Fatalf("path %s", p)
	}
	if OpenPath("") != "/" {
		t.Fatal("a push about no thread opens the inbox")
	}
}

func TestNotificationsReplacePerThreadAndMapClicks(t *testing.T) {
	var ns Notifications
	a := ns.Plan(Notice{Title: "Add login", Body: "1 question · Which?", State: "asking", Ref: "H/a"})
	if a.Replaces != 0 || a.Urgency != 1 || a.Summary != "Add login" {
		t.Fatalf("first %+v", a)
	}
	ns.Posted("H/a", "asking", 7)
	if b := ns.Plan(Notice{Title: "Add login", Body: "Done", State: "done", Ref: "H/a"}); b.Replaces != 7 || b.Urgency != 0 {
		t.Fatalf("second %+v", b)
	}
	ns.Posted("H/a", "done", 7)
	if st := ns.Stale("a", "done"); len(st) != 0 {
		t.Fatalf("a notification that fits its thread stays: %v", st)
	}
	if st := ns.Stale("a", "working"); len(st) != 1 || st[0] != 7 {
		t.Fatalf("stale %v", st)
	}
	if ref, ok := ns.Clicked(7); !ok || ref != "H/a" {
		t.Fatalf("click %q %v", ref, ok)
	}
	if _, ok := ns.Clicked(8); ok {
		t.Fatal("another app's notification")
	}
	ns.Closed(7)
	if c := ns.Plan(Notice{Title: "x", Ref: "H/a"}); c.Replaces != 0 {
		t.Fatal("a closed notification is not replaced")
	}
}

func TestServiceStateAndEnsure(t *testing.T) {
	state := "LoadState=loaded\nActiveState=inactive\nUnitFileState=disabled"
	var calls []string
	s := Service{Run: func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		switch args[0] {
		case "show":
			return state, nil
		case "enable":
			state = strings.Replace(state, "disabled", "enabled", 1)
		case "start":
			state = strings.Replace(state, "inactive", "active", 1)
		}
		return "", nil
	}}
	if st := s.State(); !st.Installed || st.Running() || st.Words() != "Fyr is paused" {
		t.Fatalf("state %+v", st)
	}
	// NixOS: the module owns the unit; the app starts nothing.
	calls = nil
	if _, err := s.Ensure(true); err != nil || len(calls) != 1 {
		t.Fatalf("nixos %v %v", err, calls)
	}
	calls = nil
	st, err := s.Ensure(false)
	if err != nil || !st.Running() || !st.Enabled || st.Words() != "Fyr is running" {
		t.Fatalf("ensure %+v %v", st, err)
	}
	if strings.Join(calls, ";") != "show fyr.service -p LoadState,ActiveState,UnitFileState;enable fyr.service;start fyr.service;show fyr.service -p LoadState,ActiveState,UnitFileState" {
		t.Fatalf("calls %q", calls)
	}
	// Not installed: left to the install.
	state = "LoadState=not-found\nActiveState=inactive\nUnitFileState="
	calls = nil
	if st, _ := s.Ensure(false); st.Installed || len(calls) != 1 || st.Words() != "Fyr is not installed as a service" {
		t.Fatalf("not installed %+v %v", st, calls)
	}
	s.Run = func(...string) (string, error) { return "Access denied", errors.New("exit 1") }
	if err := s.Pause(); err == nil || !strings.Contains(err.Error(), "Access denied") {
		t.Fatalf("pause error %v", err)
	}
}

// The native device (V0.mac-notify): it stores itself, reads its stream
// (empty lines are signs of life), and answers the page's bridge.
func TestDeviceFollowsItsStream(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "owner.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	known := map[string]bool{}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/push/native":
			var b struct{ Endpoint, Device string }
			json.NewDecoder(r.Body).Decode(&b)
			if !known[b.Endpoint] {
				b.Endpoint = "native:e1"
				known[b.Endpoint] = true
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "endpoint": b.Endpoint})
		case "/push/native/stream":
			if !known[r.URL.Query().Get("endpoint")] {
				w.WriteHeader(404)
				return
			}
			w.Write([]byte("\n" + `{"title":"Add login","body":"1 question · Which?","threadId":"t","host":"H","state":"asking"}` + "\n"))
		case "/session/once":
			w.Write([]byte(`{"url":"http://fyr.localhost:7777/auth/once#tok"}`))
		}
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	o := Owner{Socket: sock}

	if !errors.Is(o.Follow(context.Background(), "native:gone", func() {}, func(Notice) {}), ErrUnknown) {
		t.Fatal("an unknown device is ErrUnknown")
	}
	var ep string
	var off bool
	var got []Notice
	d := &Device{Owner: o, Name: "Fyr app on test",
		Load: func() (string, bool) { mu.Lock(); defer mu.Unlock(); return ep, off },
		Save: func(e string, o bool) { mu.Lock(); defer mu.Unlock(); ep, off = e, o },
		Show: func(n Notice) { mu.Lock(); defer mu.Unlock(); got = append(got, n) },
		Up:   func(bool) {}}
	if r, _ := d.Bridge([]byte(`{"op":"status"}`), nil); r.On {
		t.Fatal("not stored yet: off")
	}
	r, err := d.Bridge([]byte(`{"op":"on"}`), nil)
	if err != nil || !r.On || r.Endpoint != "native:e1" {
		t.Fatalf("on %+v %v", r, err)
	}
	if err := o.Follow(context.Background(), "native:e1", func() {}, d.Show); err == nil || len(got) != 1 || got[0].Ref != "H/t" || got[0].State != "asking" {
		t.Fatalf("follow %v %+v", err, got)
	}
	if r, _ := d.Bridge([]byte(`{"op":"off"}`), nil); r.On || r.Endpoint != "native:e1" || !off {
		t.Fatalf("off %+v", r)
	}
	if r, _ := d.Bridge([]byte(`{"op":"status"}`), nil); r.On {
		t.Fatal("off stays off")
	}
	var closed string
	d.Bridge([]byte(`{"op":"clear","threadId":"t","state":"working"}`), func(id, st string) { closed = id + " " + st })
	if closed != "t working" {
		t.Fatalf("clear %q", closed)
	}
	if _, err := d.Bridge([]byte(`{"op":"nope"}`), nil); err == nil {
		t.Fatal("an unknown op")
	}
	if u, err := o.Once(); err != nil || u != "http://fyr.localhost:7777/auth/once#tok" {
		t.Fatalf("once %q %v", u, err)
	}
	if _, err := (Owner{Socket: filepath.Join(t.TempDir(), "none.sock")}).Once(); err == nil || !strings.Contains(err.Error(), "does not answer") {
		t.Fatalf("no fyrd: %v", err)
	}
}
