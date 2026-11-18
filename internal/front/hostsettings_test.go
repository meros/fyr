package front

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/engine"
	"github.com/meros/fyr/internal/hostfs"
	"github.com/meros/fyr/internal/provider"
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
}

func newSettingsHost(t *testing.T, name string) *settingsHost {
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
	s := &server.Server{Engine: h.eng, BuildID: "host-build", Host: name, Providers: registryAPI{reg}, ProfileList: reg.Profiles,
		FS: &hostfs.FS{Home: home, Folders: h.eng.ProjectFolders, Recent: h.eng.RecentFolders},
		// Every host has a provider "claude"; its quota says whose it is.
		Quota: func(context.Context) any {
			return []map[string]any{{"profile": "claude", "kind": "anthropic-oauth", "error": "on " + name}}
		}}
	inner := s.Handler()
	h.hs = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.down.Load() {
			panic(http.ErrAbortHandler)
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { h.hs.CloseClientConnections(); h.hs.Close(); st.Close() })
	return &settingsHost{host: h, reg: reg, home: home, srv: s}
}

// registryAPI is fyrd's providerAPI: the registry as the server reads it.
type registryAPI struct{ *provider.Registry }

func (r registryAPI) List() any  { return r.Registry.List() }
func (r registryAPI) Found() any { return r.Registry.Found() }

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

	// Providers: create on b; a retry with the same commandId answers the same and adds nothing.
	create := `{"commandId":"p1","action":"provider.create","args":{"host":"b","kind":"endpoint","name":"Lab Z","baseURL":"https://z.example","models":["glm-5"]}}`
	if code, rep := post(t, fs.URL+"/api/command", create); code != 200 || rep["ok"] != true {
		t.Fatalf("provider.create on b: %d %v", code, rep)
	}
	if code, rep := post(t, fs.URL+"/api/command", create); code != 200 || rep["ok"] != true {
		t.Fatalf("retry: %d %v", code, rep)
	}
	if n := len(b.reg.Providers()); n != 1 {
		t.Fatalf("b has %d providers after a retried create, want 1", n)
	}
	if len(a.reg.Providers()) != 0 {
		t.Fatal("a got b's provider")
	}
	id := providerNamed(b.reg, "Lab Z").ID
	if code, rep := post(t, fs.URL+"/api/command", `{"commandId":"p2","action":"provider.update","args":{"host":"b","id":"`+id+`","name":"Lab Z2"}}`); code != 200 {
		t.Fatalf("provider.update on b: %d %v", code, rep)
	}
	if providerNamed(b.reg, "Lab Z2") == nil {
		t.Fatal("b's provider was not renamed")
	}
	// The reads follow ?host=.
	if _, _, body := read(t, fs.URL+"/api/providers?host=b"); !strings.Contains(body, "Lab Z2") {
		t.Fatalf("providers?host=b: %s", body)
	}
	if _, _, body := read(t, fs.URL+"/api/providers?host=a"); strings.Contains(body, "Lab Z2") {
		t.Fatalf("providers?host=a shows b's: %s", body)
	}

	// System agents on b.
	if code, rep := post(t, fs.URL+"/api/command", `{"commandId":"s1","action":"host.systemAgents","args":{"host":"b","profile":"`+id+`","model":"glm-5"}}`); code != 200 {
		t.Fatalf("host.systemAgents on b: %d %v", code, rep)
	}
	if got := b.eng.SystemAgents().Default; got.Profile != id || got.Model != "glm-5" {
		t.Fatalf("b's system agents: %+v", got)
	}
	if got := a.eng.SystemAgents().Default; got.Profile != "" {
		t.Fatalf("a's system agents changed: %+v", got)
	}

	// Automation on b.
	if code, rep := post(t, fs.URL+"/api/command", `{"commandId":"d1","action":"host.autoDeploy","args":{"host":"b","on":false}}`); code != 200 {
		t.Fatalf("host.autoDeploy on b: %d %v", code, rep)
	}
	if b.eng.AutoDeploy().On || !a.eng.AutoDeploy().On {
		t.Fatalf("auto-deploy: a %v b %v", a.eng.AutoDeploy().On, b.eng.AutoDeploy().On)
	}
	var dep struct{ On bool }
	if _, _, body := read(t, fs.URL+"/api/autoDeploy?host=b"); json.Unmarshal([]byte(body), &dep) != nil || dep.On {
		t.Fatalf("autoDeploy?host=b: %s", body)
	}

	// A project on b: made there, then changed by its id alone.
	dir := t.TempDir()
	code, rep := post(t, fs.URL+"/api/command", `{"commandId":"j1","action":"project.create","args":{"host":"b","name":"Lab","folder":"`+dir+`"}}`)
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
	if code, rep := post(t, fs.URL+"/api/command", `{"commandId":"j2","action":"project.update","args":{"id":"`+pid+`","name":"Lab 2","defaultProfile":"`+id+`"}}`); code != 200 {
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

// SPEC V0.126 (2): a host name the front does not follow is refused, for a
// command and for a read; nothing goes to the first host instead.
func TestFrontRefusesAnUnknownHost(t *testing.T) {
	a := newSettingsHost(t, "a")
	fs := newFront(t, HostConfig{Name: "a", URL: a.hs.URL})
	online(t, fs.URL, 1)
	code, rep := post(t, fs.URL+"/api/command", `{"commandId":"x1","action":"provider.create","args":{"host":"lab","kind":"endpoint","name":"Z","baseURL":"https://z.example"}}`)
	if code != http.StatusNotFound || !strings.Contains(rep["error"].(string), "lab") {
		t.Fatalf("command to an unknown host: %d %v", code, rep)
	}
	if len(a.reg.Providers()) != 0 {
		t.Fatal("the command for lab ran on a")
	}
	if code, _, body := read(t, fs.URL+"/api/providers?host=lab"); code != http.StatusNotFound || !strings.Contains(body, "lab") {
		t.Fatalf("read of an unknown host: %d %s", code, body)
	}
}

// SPEC V0.126 (3): the front keeps the last settings it read from each host;
// while the host is offline it serves them marked with when they were read
// (X-Fyr-Read-At), also after the front restarts; a change is refused.
func TestFrontServesLastKnownSettingsOfAnOfflineHost(t *testing.T) {
	a, b := newSettingsHost(t, "a"), newSettingsHost(t, "b")
	cache := t.TempDir()
	hosts := []HostConfig{{Name: "a", URL: a.hs.URL}, {Name: "b", URL: b.hs.URL}}
	fs := startFront(t, Options{CacheDir: cache}, hosts...)
	online(t, fs.URL, 2)
	if code, rep := post(t, fs.URL+"/api/command", `{"commandId":"p1","action":"provider.create","args":{"host":"b","kind":"endpoint","name":"Lab Z","baseURL":"https://z.example"}}`); code != 200 {
		t.Fatalf("create: %d %v", code, rep)
	}
	paths := []string{"/api/providers", "/api/providers/found", "/api/systemAgents", "/api/autoDeploy", "/api/autoMerge", "/api/updates"}
	live := map[string]string{}
	for _, p := range paths {
		code, h, body := read(t, fs.URL+p+"?host=b")
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
			code, h, body := read(t, fs.URL+p+"?host=b")
			at, err := time.Parse(time.RFC3339, h.Get("X-Fyr-Read-At"))
			if code != 200 || err != nil || time.Since(at) > time.Minute || body != live[p] {
				t.Fatalf("%s %s: %d %q %s", when, p, code, h.Get("X-Fyr-Read-At"), body)
			}
		}
	}
	check("offline")
	if code, rep := post(t, fs.URL+"/api/command", `{"commandId":"p2","action":"provider.create","args":{"host":"b","kind":"endpoint","name":"Lab Y","baseURL":"https://y.example"}}`); code != http.StatusServiceUnavailable {
		t.Fatalf("a change on offline b: %d %v", code, rep)
	}
	// A path never read while b was online has nothing to show.
	if code, _, _ := read(t, fs.URL+"/api/load?host=b"); code != http.StatusServiceUnavailable {
		t.Fatalf("an unread path: %d", code)
	}
	fs.stop()
	fs2 := startFront(t, Options{CacheDir: cache}, hosts...)
	eventually(t, "a online after the restart", func() bool { return getState(t, fs2.URL).Hosts[0]["online"] == true })
	fs = fs2
	check("after a restart")
}

// SPEC V0.126 (4): the quota of the same provider id on two hosts is two
// quotas, each stamped with its host, the first host's first.
func TestFrontQuotaPerHost(t *testing.T) {
	a, b := newSettingsHost(t, "a"), newSettingsHost(t, "b")
	fs := newFront(t, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	online(t, fs.URL, 2)
	_, _, body := read(t, fs.URL+"/api/quota")
	var qs []map[string]any
	if err := json.Unmarshal([]byte(body), &qs); err != nil {
		t.Fatal(err)
	}
	if len(qs) != 2 || qs[0]["host"] != "a" || qs[0]["error"] != "on a" || qs[1]["host"] != "b" || qs[1]["error"] != "on b" {
		t.Fatalf("quota: %s", body)
	}
}

// SPEC V0.126 (3): a host that stops answering before the front has marked
// it offline still shows what was last read, not an error.
func TestFrontServesLastKnownSettingsWhenTheReadFails(t *testing.T) {
	a, b := newSettingsHost(t, "a"), newSettingsHost(t, "b")
	fs := newFront(t, HostConfig{Name: "a", URL: a.hs.URL}, HostConfig{Name: "b", URL: b.hs.URL})
	online(t, fs.URL, 2)
	_, _, live := read(t, fs.URL+"/api/autoDeploy?host=b")
	b.setDown(true)
	code, h, body := read(t, fs.URL+"/api/autoDeploy?host=b") // at once: the front may not know yet
	if code != 200 || h.Get("X-Fyr-Read-At") == "" || body != live {
		t.Fatalf("a read that fails: %d %q %s", code, h.Get("X-Fyr-Read-At"), body)
	}
}
