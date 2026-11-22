package front

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/hostfs"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/quota"
	"github.com/meros/fyr/internal/secrets"
	"github.com/meros/fyr/internal/server"
	"github.com/meros/fyr/internal/store"
)

// settingsHost is a host as fyrd wires its settings (cmd/fyrd/host.go): a
// provider registry on its own store and secrets, its system agents and its
// update setting.
type settingsHost struct {
	srv *server.Server
	*host
	reg *provider.Registry
	// home is the host's home folder, the root of its folder picker (V0.132).
	home string
	// noLink: the host answers project.link as one from before decision 0034.
	noLink atomic.Bool
}

func newSettingsHost(t *testing.T, name string) *settingsHost {
	return newSettingsHostID(t, name, testID(name))
}

// newSettingsHostID is a settings host that reports an install id.
func newSettingsHostID(t *testing.T, name, id string) *settingsHost {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	reg := &provider.Registry{DB: st, Secrets: secrets.Files(filepath.Join(dir, "secrets")), Home: dir, Getenv: func(string) string { return "" }}
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	h := &host{eng: engine.New(st)}
	h.eng.Providers = reg
	h.eng.ProfileExists = func(id string) bool {
		for _, p := range reg.Profiles() {
			if p.ID == id {
				return true
			}
		}
		return false
	}
	h.eng.SystemAgentProviders = func() []engine.SystemProvider {
		var out []engine.SystemProvider
		for _, p := range reg.Profiles() {
			out = append(out, engine.SystemProvider{ID: p.ID, Name: p.Name, Models: p.Models})
		}
		return out
	}
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, "only-on-"+name), 0o700); err != nil {
		t.Fatal(err)
	}
	s := &server.Server{Engine: h.eng, BuildID: "host-build", Host: name, ID: id, Providers: reg, ProfileList: reg.Profiles,
		FS: &hostfs.FS{Home: home, Folders: h.eng.ProjectFolders, Recent: h.eng.RecentFolders},
		// Every host has a provider "claude"; its quota says whose it is.
		Quota: func(context.Context) []quota.Status {
			return []quota.Status{{Profile: "claude", Kind: "anthropic-oauth", Error: "on " + name}}
		}}
	inner := s.Handler()
	sh := &settingsHost{host: h, reg: reg, home: home, srv: s}
	h.hs = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.down.Load() {
			panic(http.ErrAbortHandler)
		}
		if sh.noLink.Load() && r.URL.Path == "/api/command" { // a host from before decision 0034
			b, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(b))
			if bytes.Contains(b, []byte(`"project.link"`)) {
				w.WriteHeader(400)
				io.WriteString(w, `{"error":"unknown action project.link"}`)
				return
			}
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { h.hs.CloseClientConnections(); h.hs.Close(); st.Close() })
	return sh
}

func providerNamed(r *provider.Registry, name string) *provider.Provider {
	for _, p := range r.Providers() {
		if p.Name == name {
			return &p
		}
	}
	return nil
}

func read(t *testing.T, url string) (int, http.Header, string) {
	t.Helper()
	r, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return r.StatusCode, r.Header, string(b)
}

// SPEC V0.126 (2): from any front the owner changes a provider, the system
// agents, the update setting and a project on host b exactly as on host a;
// each lands on b and nowhere else, and a retried commandId runs once.
func TestFrontHostSettingsReachTheirHost(t *testing.T) {
	a, b := newSettingsHost(t, "a"), newSettingsHost(t, "b")
	fs := newFront(t, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	online(t, fs.URL, 2)
	au, bu := hostURL(t, fs.URL, "a"), hostURL(t, fs.URL, "b")

	// Providers: create on b; a retry with the same commandId answers the same and adds nothing.
	create := `{"commandId":"p1","action":"provider.create","args":{"kind":"endpoint","name":"Lab Z","baseURL":"https://z.example","models":["glm-5"]}}`
	if code, rep := post(t, bu+"/command", create); code != 200 || rep["ok"] != true {
		t.Fatalf("provider.create on b: %d %v", code, rep)
	}
	if code, rep := post(t, bu+"/command", create); code != 200 || rep["ok"] != true {
		t.Fatalf("retry: %d %v", code, rep)
	}
	if n := len(b.reg.Providers()); n != 1 {
		t.Fatalf("b has %d providers after a retried create, want 1", n)
	}
	if len(a.reg.Providers()) != 0 {
		t.Fatal("a got b's provider")
	}
	id := providerNamed(b.reg, "Lab Z").ID
	if code, rep := post(t, bu+"/command", `{"commandId":"p2","action":"provider.update","args":{"id":"`+id+`","name":"Lab Z2"}}`); code != 200 {
		t.Fatalf("provider.update on b: %d %v", code, rep)
	}
	if providerNamed(b.reg, "Lab Z2") == nil {
		t.Fatal("b's provider was not renamed")
	}
	// The reads follow the prefix.
	if _, _, body := read(t, bu+"/providers"); !strings.Contains(body, "Lab Z2") {
		t.Fatalf("b's providers: %s", body)
	}
	if _, _, body := read(t, au+"/providers"); strings.Contains(body, "Lab Z2") {
		t.Fatalf("a's providers show b's: %s", body)
	}

	// System agents on b.
	if code, rep := post(t, bu+"/command", `{"commandId":"s1","action":"host.systemAgents","args":{"profile":"`+id+`","model":"glm-5"}}`); code != 200 {
		t.Fatalf("host.systemAgents on b: %d %v", code, rep)
	}
	if got := b.eng.SystemAgents().Default; got.Profile != id || got.Model != "glm-5" {
		t.Fatalf("b's system agents: %+v", got)
	}
	if got := a.eng.SystemAgents().Default; got.Profile != "" {
		t.Fatalf("a's system agents changed: %+v", got)
	}

	// Automation on b.
	if code, rep := post(t, bu+"/command", `{"commandId":"d1","action":"host.autoDeploy","args":{"on":false}}`); code != 200 {
		t.Fatalf("host.autoDeploy on b: %d %v", code, rep)
	}
	if b.eng.AutoDeploy().On || !a.eng.AutoDeploy().On {
		t.Fatalf("auto-deploy: a %v b %v", a.eng.AutoDeploy().On, b.eng.AutoDeploy().On)
	}
	var dep struct{ On bool }
	if _, _, body := read(t, bu+"/autoDeploy"); json.Unmarshal([]byte(body), &dep) != nil || dep.On {
		t.Fatalf("b's autoDeploy: %s", body)
	}

	// A project on b: made there, then changed by its id alone.
	dir := t.TempDir()
	code, rep := post(t, bu+"/command", `{"commandId":"j1","action":"project.create","args":{"name":"Lab","folder":"`+dir+`"}}`)
	if code != 200 {
		t.Fatalf("project.create on b: %d %v", code, rep)
	}
	var pid string
	eventually(t, "b's project on the front", func() bool {
		for _, p := range getState(t, fs.URL).Projects {
			if p["name"] == "Lab" && p["host"] == "b" {
				pid, _ = p["id"].(string)
			}
		}
		return pid != ""
	})
	if code, rep := post(t, bu+"/command", `{"commandId":"j2","action":"project.update","args":{"id":"`+pid+`","name":"Lab 2","defaultProfile":"`+id+`"}}`); code != 200 {
		t.Fatalf("project.update on b: %d %v", code, rep)
	}
	var on *string
	for _, p := range b.eng.Projects() {
		if p.ID == pid {
			on = &p.Name
		}
	}
	if on == nil || *on != "Lab 2" {
		t.Fatalf("b's project: %v", on)
	}
	if len(a.eng.Projects()) != 0 {
		t.Fatal("a got b's project")
	}
}

// SPEC V0.126 (2), decision 0031 2.2: a host id the front does not follow is
// refused, for a command and for a read; nothing goes to the first host
// instead, and a host's name is not its id.
func TestFrontRefusesAnUnknownHost(t *testing.T) {
	a := newSettingsHost(t, "a")
	fs := newFront(t, HostConfig{Name: "a", URL: a.hs.URL})
	online(t, fs.URL, 1)
	code, rep := post(t, fs.URL+"/api/h/lab/command", `{"commandId":"x1","action":"provider.create","args":{"kind":"endpoint","name":"Z","baseURL":"https://z.example"}}`)
	if code != http.StatusNotFound || !strings.Contains(rep["error"].(string), "lab") {
		t.Fatalf("command to an unknown host: %d %v", code, rep)
	}
	if len(a.reg.Providers()) != 0 {
		t.Fatal("the command for lab ran on a")
	}
	if code, _, body := read(t, fs.URL+"/api/h/lab/providers"); code != http.StatusNotFound || !strings.Contains(body, "lab") {
		t.Fatalf("read of an unknown host: %d %s", code, body)
	}
	if code, _, _ := read(t, fs.URL+"/api/h/a/providers"); code != http.StatusNotFound {
		t.Fatalf("a host's name taken for its id: %d", code)
	}
}

// SPEC V0.126 (3): the front keeps the last settings it read from each host;
// while the host is offline it serves them marked with when they were read
// (X-Fyr-Read-At), also after the front restarts; a change is refused.
func TestFrontServesLastKnownSettingsOfAnOfflineHost(t *testing.T) {
	a, b := newSettingsHost(t, "a"), newSettingsHost(t, "b")
	cache := t.TempDir()
	pins := &memKV{} // the host ids the front pinned, which name its cache files (decision 0031 4.3)
	hosts := []HostConfig{{Name: "a", URL: a.hs.URL}, {Name: "b", URL: b.hs.URL}}
	fs := startFront(t, Options{CacheDir: cache, Store: pins}, hosts...)
	online(t, fs.URL, 2)
	bu := hostURL(t, fs.URL, "b")
	if code, rep := post(t, bu+"/command", `{"commandId":"p1","action":"provider.create","args":{"kind":"endpoint","name":"Lab Z","baseURL":"https://z.example"}}`); code != 200 {
		t.Fatalf("create: %d %v", code, rep)
	}
	paths := []string{"/providers", "/providers/found", "/systemAgents", "/autoDeploy", "/autoMerge", "/updates"}
	live := map[string]string{}
	for _, p := range paths {
		code, h, body := read(t, bu+p)
		if code != 200 || h.Get("X-Fyr-Read-At") != "" {
			t.Fatalf("%s online: %d %q", p, code, h.Get("X-Fyr-Read-At"))
		}
		live[p] = body
	}
	b.setDown(true)
	eventually(t, "b offline", func() bool { return getState(t, fs.URL).Hosts[1]["online"] == false })
	check := func(when string) {
		t.Helper()
		for _, p := range paths {
			code, h, body := read(t, bu+p)
			at, err := time.Parse(time.RFC3339, h.Get("X-Fyr-Read-At"))
			if code != 200 || err != nil || time.Since(at) > time.Minute || body != live[p] {
				t.Fatalf("%s %s: %d %q %s", when, p, code, h.Get("X-Fyr-Read-At"), body)
			}
		}
	}
	check("offline")
	if code, rep := post(t, bu+"/command", `{"commandId":"p2","action":"provider.create","args":{"kind":"endpoint","name":"Lab Y","baseURL":"https://y.example"}}`); code != http.StatusServiceUnavailable {
		t.Fatalf("a change on offline b: %d %v", code, rep)
	}
	// A path never read while b was online has nothing to show.
	if code, _, _ := read(t, bu+"/load"); code != http.StatusServiceUnavailable {
		t.Fatalf("an unread path: %d", code)
	}
	fs.stop()
	fs2 := startFront(t, Options{CacheDir: cache, Store: pins}, hosts...)
	eventually(t, "a online after the restart", func() bool { return getState(t, fs2.URL).Hosts[0]["online"] == true })
	fs, bu = fs2, hostURL(t, fs2.URL, "b")
	check("after a restart")
}

// SPEC V0.126 (4): the quota of the same provider id on two hosts is two
// quotas, each read from its own host by its prefix (the client stamps each
// with its host; the front merges nothing).
func TestFrontQuotaPerHost(t *testing.T) {
	a, b := newSettingsHost(t, "a"), newSettingsHost(t, "b")
	fs := newFront(t, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	online(t, fs.URL, 2)
	for _, name := range []string{"a", "b"} {
		_, _, body := read(t, hostURL(t, fs.URL, name)+"/quota")
		var qs []map[string]any
		if err := json.Unmarshal([]byte(body), &qs); err != nil {
			t.Fatal(err)
		}
		if len(qs) != 1 || qs[0]["error"] != "on "+name {
			t.Fatalf("%s's quota: %s", name, body)
		}
	}
}

// SPEC V0.126 (3): a host that stops answering before the front has marked
// it offline still shows what was last read, not an error.
func TestFrontServesLastKnownSettingsWhenTheReadFails(t *testing.T) {
	a, b := newSettingsHost(t, "a"), newSettingsHost(t, "b")
	fs := newFront(t, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	online(t, fs.URL, 2)
	bu := hostURL(t, fs.URL, "b")
	_, _, live := read(t, bu+"/autoDeploy")
	b.setDown(true)
	code, h, body := read(t, bu+"/autoDeploy") // at once: the front may not know yet
	if code != 200 || h.Get("X-Fyr-Read-At") == "" || body != live {
		t.Fatalf("a read that fails: %d %q %s", code, h.Get("X-Fyr-Read-At"), body)
	}
}
