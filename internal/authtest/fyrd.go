package authtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/meros/fyr/internal/auth"
)

// Binaries are built once per process, into one folder that Cleanup (called
// from Run, the TestMain helper) removes.
var (
	buildMu  sync.Mutex
	buildDir string
	built    = map[string]string{}
)

// moduleRoot is the repository root: the folder of the go.mod that the
// current directory belongs to (a test runs in its package's folder). It is
// asked of `go env` because a -trimpath build has no source path to look at.
func moduleRoot() (string, error) {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOMOD: %v", err)
	}
	mod := strings.TrimSpace(string(out))
	if mod == "" || mod == os.DevNull {
		return "", errors.New("not inside the fyr module")
	}
	return filepath.Dir(mod), nil
}

// Binary builds ./cmd/<name> once and returns its path.
func Binary(name string) (string, error) {
	buildMu.Lock()
	defer buildMu.Unlock()
	if p, ok := built[name]; ok {
		return p, nil
	}
	if buildDir == "" {
		d, err := os.MkdirTemp("", "fyrbin")
		if err != nil {
			return "", err
		}
		buildDir = d
	}
	out := filepath.Join(buildDir, name)
	var err error
	cmd := exec.Command("go", "build", "-o", out, "./cmd/"+name)
	if cmd.Dir, err = moduleRoot(); err != nil {
		return "", err
	}
	cmd.Env = append(cleanEnv(os.Environ()), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build ./cmd/%s: %v\n%s", name, err, b)
	}
	built[name] = out
	return out, nil
}

// Cleanup removes the built binaries.
func Cleanup() {
	buildMu.Lock()
	defer buildMu.Unlock()
	if buildDir != "" {
		os.RemoveAll(buildDir)
		buildDir = ""
		built = map[string]string{}
	}
}

// cleanEnv is env without any FYR_* variable (#167, V0.131): a scratch fyrd
// that inherited the live install's FYR_RUNNER once killed the live threads.
func cleanEnv(env []string) []string {
	var out []string
	for _, e := range env {
		if !strings.HasPrefix(e, "FYR_") {
			out = append(out, e)
		}
	}
	return out
}

// HostEntry is a front's static `hosts` entry.
type HostEntry struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Options start a scratch fyrd.
type Options struct {
	Role  string // host, front or both (default both)
	Name  string
	Hosts []HostEntry
	// Tailscale points the fyrd at this fake tailscaled (config
	// tailscaleSocket). Nil leaves the default socket, which a scratch fyrd
	// must not talk to: the harness then points it at a path that is not
	// there, so the fyrd serves loopback only.
	Tailscale *Tailscaled
	// Extra is merged into the config file (config keys).
	Extra map[string]any
	// Data is an existing data folder (a fixture); default a new one.
	Data string
}

// Fyrd is a real fyrd process in a temp folder.
type Fyrd struct {
	Data string // the data folder
	Addr string // 127.0.0.1:PORT, once ready
	Name string

	cmd  *exec.Cmd
	logb *syncBuf
	done chan struct{}
	root string // the folder that holds Data, removed on Stop when this harness made it
	own  bool
	opts Options
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

var readyRe = regexp.MustCompile(`listening on http://(127\.0\.0\.1:\d+)`)

// NewDataDir makes a short data folder (a Unix socket path is limited to
// about 100 bytes) and returns it with a remover.
func NewDataDir() (string, func(), error) {
	root, err := os.MkdirTemp("", "fa")
	if err != nil {
		return "", nil, err
	}
	d := filepath.Join(root, "d")
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", nil, err
	}
	return d, func() { os.RemoveAll(root) }, nil
}

// Start runs fyrd with the options on port 0 and waits for its ready line.
func Start(o Options) (*Fyrd, error) {
	bin, err := Binary("fyrd")
	if err != nil {
		return nil, err
	}
	f := &Fyrd{Name: o.Name, Data: o.Data, logb: &syncBuf{}, done: make(chan struct{}), opts: o}
	if f.Data == "" {
		d, rm, err := NewDataDir()
		if err != nil {
			return nil, err
		}
		f.Data = d
		f.root = filepath.Dir(d)
		f.own = true
		_ = rm // Stop removes the folder
	}
	root := filepath.Dir(f.Data)
	cfg := map[string]any{"listen": "127.0.0.1:0", "role": orDefault(o.Role, "both"), "stepUpMinutes": 0, "discovery": map[string]any{"mdns": false}} // a test names the step-up it wants
	if o.Name != "" {
		cfg["name"] = o.Name
	}
	if len(o.Hosts) > 0 {
		cfg["hosts"] = o.Hosts
	}
	if o.Tailscale != nil {
		cfg["tailscaleSocket"] = o.Tailscale.Socket
	} else {
		cfg["tailscaleSocket"] = filepath.Join(root, "no-tailscaled.sock")
	}
	for k, v := range o.Extra {
		cfg[k] = v
	}
	cfgPath := filepath.Join(root, fmt.Sprintf("config-%s.json", orDefault(o.Name, "fyrd")))
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, b, 0o600); err != nil {
		return nil, err
	}
	t3 := filepath.Join(root, "t3")
	os.MkdirAll(t3, 0o700)
	cmd := exec.Command(bin, "serve", "-data", f.Data, "-config", cfgPath, "-t3", t3)
	// Only what a process needs; every FYR_* variable out (#167).
	cmd.Env = append(cleanEnv(os.Environ()), "HOME="+root)
	cmd.Stdout, cmd.Stderr = f.logb, f.logb
	dieWithTest(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	f.cmd = cmd
	go func() { cmd.Wait(); close(f.done) }()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if m := readyRe.FindStringSubmatch(f.logb.String()); m != nil {
			f.Addr = m[1]
			return f, nil
		}
		select {
		case <-f.done:
			return nil, fmt.Errorf("fyrd exited before it was ready:\n%s", f.logb.String())
		default:
		}
		if time.Now().After(deadline) {
			f.Stop()
			return nil, fmt.Errorf("fyrd not ready within 30 s:\n%s", f.logb.String())
		}
		time.Sleep(20 * time.Millisecond) // a bounded poll, not a fixed wait
	}
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// Stop ends the process (SIGTERM, then SIGKILL after 5 s) and removes the
// folder this harness made.
func (f *Fyrd) Stop() {
	if f.cmd != nil && f.cmd.Process != nil {
		select {
		case <-f.done:
		default:
			f.cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-f.done:
			case <-time.After(5 * time.Second):
				f.cmd.Process.Kill()
				<-f.done
			}
		}
	}
	if f.own && f.root != "" {
		os.RemoveAll(f.root)
	}
}

// Log is everything the process wrote.
func (f *Fyrd) Log() string { return f.logb.String() }

// WaitLog polls the log for substr until the deadline.
func (f *Fyrd) WaitLog(substr string, d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if strings.Contains(f.Log(), substr) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return strings.Contains(f.Log(), substr)
}

// URL is the loopback URL.
func (f *Fyrd) URL() string { return "http://" + f.Addr }

// Sock is the path of one of the install's sockets (serve.sock, owner.sock).
func (f *Fyrd) Sock(name string) string { return auth.SocketPath(f.Data, name) }

// Request is a call made to a fyrd or an emulator.
type Request struct {
	Method string
	Path   string
	Header map[string]string
	Body   io.Reader
	Host   string // the Host header; "" keeps the URL's
	Cookie string // a fyr_session value
}

// Response is what came back, the body read.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Session is the fyr_session cookie a response set, "" when none.
func (r *Response) Session() string {
	for _, c := range (&http.Response{Header: r.Header}).Cookies() {
		if c.Name == auth.Cookie {
			return c.Value
		}
	}
	return ""
}

var client = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// Do sends a request to base (http://addr) and reads the whole answer.
func Do(base string, r Request) (*Response, error) {
	m := orDefault(r.Method, "GET")
	req, err := http.NewRequest(m, base+r.Path, r.Body)
	if err != nil {
		return nil, err
	}
	for k, v := range r.Header {
		req.Header.Set(k, v)
	}
	if r.Host != "" {
		req.Host = r.Host
	}
	if r.Cookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.Cookie, Value: r.Cookie})
	}
	if m == http.MethodPost && req.Header.Get("X-Fyr") == "" {
		req.Header.Set("X-Fyr", "1")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: b}, err
}

// Get is Do with a GET.
func (f *Fyrd) Get(path, cookie string) (*Response, error) {
	return Do(f.URL(), Request{Path: path, Cookie: cookie})
}

// Unix is an HTTP client that dials a Unix socket.
func Unix(sock string) *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DialContext: unixDialer(sock), DisableKeepAlives: true}}
}

// OwnerOnce asks the owner socket for a one-time link, as fyr open does.
func (f *Fyrd) OwnerOnce() (string, error) {
	resp, err := Unix(f.Sock(auth.OwnerSocket)).Post("http://owner/session/once", "application/json", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct{ URL, Error string }
	json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != 200 || out.URL == "" {
		return "", errors.New("owner socket: " + resp.Status + " " + out.Error)
	}
	return out.URL, nil
}

// Redeem posts a one-time link's token to /auth/once on base and returns the
// session cookie it minted.
func Redeem(base, link string) (string, error) {
	_, tok, _ := strings.Cut(link, "#")
	r, err := Do(base, Request{Method: "POST", Path: "/auth/once", Header: map[string]string{"Content-Type": "application/json"}, Body: strings.NewReader(`{"token":"` + tok + `"}`)})
	if err != nil {
		return "", err
	}
	if r.Status != 200 || r.Session() == "" {
		return "", fmt.Errorf("redeem: %d %s", r.Status, r.Body)
	}
	return r.Session(), nil
}

// Command is exec.Command with the environment of a scratch process: no
// FYR_* variable, HOME in a temp folder. A test adds what it needs on purpose.
func Command(bin string, args ...string) *exec.Cmd {
	cmd := exec.Command(bin, args...)
	cmd.Env = append(cleanEnv(os.Environ()), "HOME="+os.TempDir())
	return cmd
}

// UnixServer serves h on a Unix socket in a short temp folder (a stand-in
// for serve.sock) and returns its path and a stop func.
func UnixServer(t testing.TB, h http.Handler) (string, func()) {
	t.Helper()
	dir, rm, err := NewDataDir()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", p)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(ln)
	return p, func() { srv.Close(); rm() }
}

// Restart stops the process and starts it again on the same data folder and
// configuration, as a release restarts an install. The address changes unless the
// config fixes the port.
func (f *Fyrd) Restart() error {
	f.StopKeep()
	o := f.opts
	o.Data = f.Data
	n, err := Start(o)
	if err != nil {
		return err
	}
	n.own, n.root = f.own, f.root
	*f = *n
	return nil
}

// StopKeep ends the process and keeps the data folder.
func (f *Fyrd) StopKeep() {
	own := f.own
	f.own = false
	f.Stop()
	f.own = own
}

// WaitExit waits for the process to end by itself (fyr rotate-key restarts it).
func (f *Fyrd) WaitExit(d time.Duration) bool {
	select {
	case <-f.done:
		return true
	case <-time.After(d):
		return false
	}
}
