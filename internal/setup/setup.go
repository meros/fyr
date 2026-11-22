// Package setup derives a host's setup steps from its real state (decision
// 0037, #434): never a stored "done" flag. One read (GET /api/setup) reports
// each step as ok, fixable or missing with a reason, and each fix runs as an
// owner-only host command (host.setupFix). It serves both macOS (Fyr.app) and
// Linux installs.
package setup

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/meros/fyr/internal/fsutil"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Status is a step's state as read from the host.
type Status string

const (
	OK      Status = "ok"
	Fixable Status = "fixable"
	Missing Status = "missing"
)

// Fix is one button the setup wizard offers for a step.
type Fix struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Step is one row of the setup wizard.
type Step struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   Status `json:"status"`
	Optional bool   `json:"optional,omitempty"`
	Reason   string `json:"reason"`
	Fixes    []Fix  `json:"fixes,omitempty"`
	Detail   any    `json:"detail,omitempty"`
}

// Repo is a local git repository discovered for Step 4 (First project).
type Repo struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Remote string `json:"remote,omitempty"`
	GitHub bool   `json:"github,omitempty"`
}

// GHAuthState reports an in-progress `gh auth login` device flow so the web
// page can show the one-time code and link without a terminal.
type GHAuthState struct {
	Running bool   `json:"running"`
	URL     string `json:"url,omitempty"`
	Code    string `json:"code,omitempty"`
	Error   string `json:"error,omitempty"`
	Done    bool   `json:"done,omitempty"`
}

// Report is the answer of GET /api/setup.
type Report struct {
	OS     string       `json:"os"`
	Host   string       `json:"host"`
	Done   int          `json:"done"`
	Total  int          `json:"total"`
	Ready  bool         `json:"ready"`
	Steps  []Step       `json:"steps"`
	GHAuth *GHAuthState `json:"ghAuth,omitempty"`
}

// ProviderInfo is the small slice of provider state the Claude step reads.
type ProviderInfo struct {
	ID     string
	Name   string
	Status string // "ready" | "needs-login" | "needs-key"
}

// TailscaleInfo is what tailscaled reports to the Phone step.
type TailscaleInfo struct {
	Available bool
	DNSName   string
}

// PeerInfo is what the peer layer reports to the Other installs step.
type PeerInfo struct {
	Paired     int
	Candidates int
}

// Checker reads real host state and runs setup fixes. Every system interaction
// can be injected in tests.
type Checker struct {
	OS          string
	Arch        string
	Host        string
	Home        string
	Data        string
	ToolsDir    string
	ServeTarget string // "http://127.0.0.1:PORT" on macOS, "unix:<data>/serve.sock" on Linux
	TLSOn       bool

	LookPath  func(file string) (string, error)
	Exec      func(ctx context.Context, name string, args ...string) (stdout, stderr string, err error)
	Start     func(name string, args ...string) error
	FreeBytes func(path string) (uint64, error)
	Providers func() []ProviderInfo
	Projects  func() int
	ScanRepos func(roots []string) []Repo
	Tailscale func() TailscaleInfo
	Peers     func(ctx context.Context) PeerInfo
	HTTP      *http.Client

	mu     sync.Mutex
	ghAuth *ghLoginSession
}

type ghLoginSession struct {
	state  GHAuthState
	cancel context.CancelFunc
}

const minFreeBytes uint64 = 2 << 30 // 2 GiB

var (
	ghCodeRe = regexp.MustCompile(`\b([A-Z0-9]{4}-[A-Z0-9]{4})\b`)
	ghURLRe  = regexp.MustCompile(`https://github\.com/login/device\S*`)
	repoRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)
)

func (c *Checker) osName() string {
	if c.OS != "" {
		return c.OS
	}
	return runtime.GOOS
}

func (c *Checker) archName() string {
	if c.Arch != "" {
		return c.Arch
	}
	return runtime.GOARCH
}

func (c *Checker) toolsDir() string {
	if c.ToolsDir != "" {
		return c.ToolsDir
	}
	if c.Data != "" {
		return filepath.Join(c.Data, "tools")
	}
	return ""
}

func (c *Checker) lookPath(file string) (string, error) {
	if dir := c.toolsDir(); dir != "" {
		p := filepath.Join(dir, file)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	if c.LookPath != nil {
		return c.LookPath(file)
	}
	return exec.LookPath(file)
}

func (c *Checker) run(ctx context.Context, name string, args ...string) (string, string, error) {
	if c.Exec != nil {
		return c.Exec(ctx, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	return out.String(), errb.String(), err
}

func (c *Checker) start(name string, args ...string) error {
	if c.Start != nil {
		return c.Start(name, args...)
	}
	cmd := exec.Command(name, args...)
	return cmd.Start()
}

func (c *Checker) freeBytes(path string) (uint64, error) {
	if c.FreeBytes != nil {
		return c.FreeBytes(path)
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

func (c *Checker) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// Check computes the current setup report from real host state.
func (c *Checker) Check(ctx context.Context) Report {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	s1 := c.checkMachine(ctx)
	s2 := c.checkClaude()
	s3, ghOK := c.checkGitHub(ctx)
	s4 := c.checkProject(ctx, ghOK)
	s5 := c.checkPhone(ctx)
	s6 := c.checkPeers(ctx)

	steps := []Step{s1, s2, s3, s4, s5, s6}
	done := 0
	ready := true
	for _, s := range steps {
		if s.Status == OK {
			done++
		} else if !s.Optional {
			ready = false
		}
	}

	r := Report{
		OS:    c.osName(),
		Host:  c.Host,
		Done:  done,
		Total: len(steps),
		Ready: ready,
		Steps: steps,
	}
	c.mu.Lock()
	if c.ghAuth != nil {
		st := c.ghAuth.state
		if ghOK && st.Running {
			st.Running = false
			st.Done = true
		}
		r.GHAuth = &st
	}
	c.mu.Unlock()
	return r
}

func (c *Checker) checkMachine(ctx context.Context) Step {
	isMac := c.osName() == "darwin"
	title := "This machine"
	if isMac {
		title = "This Mac"
	}
	var issues []string
	var fixes []Fix
	var detail = map[string]any{"os": c.osName()}

	if isMac {
		if out, _, err := c.run(ctx, "sw_vers", "-productVersion"); err == nil {
			v := strings.TrimSpace(out)
			detail["version"] = v
			if major, err := parseMajor(v); err == nil && major < 13 {
				issues = append(issues, fmt.Sprintf("macOS %s is older than 13", v))
			}
		}
		if _, _, err := c.run(ctx, "xcode-select", "-p"); err != nil {
			issues = append(issues, "Xcode Command Line Tools are not installed")
			fixes = append(fixes, Fix{ID: "clt", Label: "Install Command Line Tools"})
		} else {
			detail["clt"] = true
		}
	}

	if _, err := c.lookPath("git"); err != nil {
		issues = append(issues, "git is not installed")
		if isMac && len(fixes) == 0 {
			fixes = append(fixes, Fix{ID: "clt", Label: "Install Command Line Tools"})
		}
	} else {
		detail["git"] = true
	}

	diskPath := c.Home
	if diskPath == "" {
		diskPath = "/"
	}
	if free, err := c.freeBytes(diskPath); err == nil {
		gb := free / (1 << 30)
		detail["freeGB"] = gb
		if free < minFreeBytes {
			issues = append(issues, fmt.Sprintf("only %d GB free disk space (need at least 2 GB)", gb))
		}
	}

	if len(issues) == 0 {
		reason := "git and disk space look good"
		if isMac {
			if v, ok := detail["version"].(string); ok && v != "" {
				reason = fmt.Sprintf("macOS %s, Command Line Tools and disk space are ready", v)
			} else {
				reason = "Command Line Tools and disk space are ready"
			}
		}
		return Step{ID: "machine", Title: title, Status: OK, Reason: reason, Detail: detail}
	}
	st := Missing
	if len(fixes) > 0 {
		st = Fixable
	}
	return Step{ID: "machine", Title: title, Status: st, Reason: strings.Join(issues, "; "), Fixes: fixes, Detail: detail}
}

func (c *Checker) checkClaude() Step {
	_, cliErr := c.lookPath("claude")
	hasCLI := cliErr == nil
	var provs []ProviderInfo
	if c.Providers != nil {
		provs = c.Providers()
	}
	ready := 0
	var needsLoginID string
	for _, p := range provs {
		if p.Status == "ready" {
			ready++
		} else if needsLoginID == "" && p.Status == "needs-login" {
			needsLoginID = p.ID
		}
	}
	detail := map[string]any{
		"hasCLI":        hasCLI,
		"providerCount": len(provs),
		"readyCount":    ready,
	}
	if needsLoginID != "" {
		detail["needsLoginId"] = needsLoginID
	}

	if ready > 0 && hasCLI {
		return Step{ID: "claude", Title: "Claude", Status: OK, Reason: fmt.Sprintf("Claude Code is ready (%d provider%s)", ready, plural(ready)), Detail: detail}
	}
	if !hasCLI {
		return Step{ID: "claude", Title: "Claude", Status: Missing, Reason: "Install Claude Code (claude) on this machine, then sign in under Providers", Detail: detail}
	}
	if needsLoginID != "" {
		return Step{ID: "claude", Title: "Claude", Status: Fixable, Reason: "Claude Code is installed; sign in under Settings → Providers", Detail: detail}
	}
	return Step{ID: "claude", Title: "Claude", Status: Fixable, Reason: "Claude Code is installed; add or sign in to a provider under Settings → Providers", Detail: detail}
}

func (c *Checker) checkGitHub(ctx context.Context) (Step, bool) {
	ghPath, err := c.lookPath("gh")
	if err != nil {
		return Step{
			ID:     "github",
			Title:  "GitHub",
			Status: Fixable,
			Reason: "gh is not installed yet",
			Fixes:  []Fix{{ID: "gh.install", Label: "Download gh"}},
		}, false
	}
	out, errOut, err := c.run(ctx, ghPath, "auth", "status")
	combined := out + "\n" + errOut
	if err != nil {
		return Step{
			ID:     "github",
			Title:  "GitHub",
			Status: Fixable,
			Reason: "gh is installed, not signed in",
			Fixes:  []Fix{{ID: "gh.login", Label: "Sign in to GitHub"}},
			Detail: map[string]any{"ghPath": ghPath, "signedIn": false},
		}, false
	}
	user := parseGHUser(combined)
	hasRepo := strings.Contains(combined, "repo")
	reason := "Signed in to GitHub with gh"
	if user != "" {
		reason = fmt.Sprintf("Signed in as %s", user)
	}
	if !hasRepo {
		return Step{
			ID:     "github",
			Title:  "GitHub",
			Status: Fixable,
			Reason: reason + " (missing repo scope)",
			Fixes:  []Fix{{ID: "gh.login", Label: "Sign in with repo scope"}},
			Detail: map[string]any{"ghPath": ghPath, "signedIn": true, "user": user, "repoScope": false},
		}, false
	}
	return Step{
		ID:     "github",
		Title:  "GitHub",
		Status: OK,
		Reason: reason,
		Detail: map[string]any{"ghPath": ghPath, "signedIn": true, "user": user, "repoScope": true},
	}, true
}

func (c *Checker) checkProject(ctx context.Context, ghOK bool) Step {
	n := 0
	if c.Projects != nil {
		n = c.Projects()
	}
	var repos []Repo
	if n == 0 {
		repos = c.discoverRepos(ctx)
	}
	detail := map[string]any{
		"projects": n,
		"canClone": ghOK,
		"repos":    repos,
	}
	if n > 0 {
		return Step{ID: "project", Title: "First project", Status: OK, Reason: fmt.Sprintf("%d project%s set up", n, plural(n)), Detail: detail}
	}
	reason := "Pick a local repository or clone one from GitHub"
	if len(repos) > 0 {
		reason = fmt.Sprintf("Found %d local repo%s — pick one to start your first thread", len(repos), plural(len(repos)))
	}
	return Step{ID: "project", Title: "First project", Status: Fixable, Reason: reason, Detail: detail}
}

func (c *Checker) discoverRepos(ctx context.Context) []Repo {
	if c.Home == "" {
		return nil
	}
	roots := []string{
		filepath.Join(c.Home, "git"),
		filepath.Join(c.Home, "code"),
		filepath.Join(c.Home, "src"),
		filepath.Join(c.Home, "Developer"),
	}
	var repos []Repo
	if c.ScanRepos != nil {
		repos = c.ScanRepos(roots)
	} else {
		repos = scanRoots(roots)
	}
	if len(repos) > 20 {
		repos = repos[:20]
	}
	for i := range repos {
		if repos[i].Remote == "" {
			if out, _, err := c.run(ctx, "git", "-C", repos[i].Path, "remote", "get-url", "origin"); err == nil {
				rem := strings.TrimSpace(out)
				repos[i].Remote = rem
				repos[i].GitHub = strings.Contains(strings.ToLower(rem), "github.com")
			}
		}
	}
	return repos
}

func (c *Checker) checkPhone(ctx context.Context) Step {
	var ts TailscaleInfo
	if c.Tailscale != nil {
		ts = c.Tailscale()
	}
	detail := map[string]any{"tailscale": ts.Available, "tls": c.TLSOn}
	if ts.DNSName != "" {
		detail["url"] = "https://" + ts.DNSName + ":7443"
	}
	if ts.Available {
		serveOn := c.tailscaleServeActive(ctx)
		detail["serveOn"] = serveOn
		if serveOn {
			reason := "Tailscale Serve is active on port 7443"
			if ts.DNSName != "" {
				reason = fmt.Sprintf("Reachable on https://%s:7443", ts.DNSName)
			}
			return Step{ID: "phone", Title: "Phone", Status: OK, Optional: true, Reason: reason, Detail: detail}
		}
		if c.ServeTarget != "" {
			return Step{
				ID:       "phone",
				Title:    "Phone",
				Status:   Fixable,
				Optional: true,
				Reason:   "Tailscale is running; turn on Tailscale Serve to pair your phone",
				Fixes:    []Fix{{ID: "tailscale.serve", Label: "Turn on Tailscale Serve"}},
				Detail:   detail,
			}
		}
	}
	if c.TLSOn {
		return Step{ID: "phone", Title: "Phone", Status: OK, Optional: true, Reason: "LAN TLS is configured; show a QR code to pair your phone", Detail: detail}
	}
	return Step{
		ID:       "phone",
		Title:    "Phone",
		Status:   Missing,
		Optional: true,
		Reason:   "Install Tailscale or configure LAN TLS to open Fyr on your phone",
		Detail:   detail,
	}
}

func (c *Checker) checkPeers(ctx context.Context) Step {
	var pi PeerInfo
	if c.Peers != nil {
		pi = c.Peers(ctx)
	}
	detail := map[string]any{"paired": pi.Paired, "candidates": pi.Candidates}
	if pi.Paired > 0 {
		return Step{ID: "peers", Title: "Other installs", Status: OK, Optional: true, Reason: fmt.Sprintf("Paired with %d other install%s", pi.Paired, plural(pi.Paired)), Detail: detail}
	}
	if pi.Candidates > 0 {
		return Step{ID: "peers", Title: "Other installs", Status: Fixable, Optional: true, Reason: fmt.Sprintf("Found %d other install%s to pair in Settings → Hosts", pi.Candidates, plural(pi.Candidates)), Detail: detail}
	}
	return Step{ID: "peers", Title: "Other installs", Status: Missing, Optional: true, Reason: "Optional: pair another Mac or Linux install under Settings → Hosts", Detail: detail}
}

func (c *Checker) tailscaleBinary() string {
	if p, err := c.lookPath("tailscale"); err == nil {
		return p
	}
	for _, p := range []string{"/opt/homebrew/bin/tailscale", "/usr/local/bin/tailscale", "/Applications/Tailscale.app/Contents/MacOS/Tailscale"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

func (c *Checker) tailscaleServeActive(ctx context.Context) bool {
	ts := c.tailscaleBinary()
	if ts == "" {
		return false
	}
	out, _, err := c.run(ctx, ts, "serve", "status", "--json")
	if err != nil {
		return false
	}
	return strings.Contains(out, ":7443") || strings.Contains(out, `"7443"`)
}

// Fix runs one setup fix action from host.setupFix.
func (c *Checker) Fix(ctx context.Context, action string, args map[string]any) (any, error) {
	switch action {
	case "clt":
		if c.osName() != "darwin" {
			return nil, errors.New("Command Line Tools are only for macOS")
		}
		if err := c.start("/usr/bin/xcode-select", "--install"); err != nil {
			return nil, fmt.Errorf("xcode-select --install: %w", err)
		}
		return map[string]any{"started": true}, nil

	case "gh.install":
		p, err := c.installGH(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"path": p}, nil

	case "gh.login":
		return c.startGHLogin()

	case "gh.loginCancel":
		c.cancelGHLogin()
		return map[string]any{"cancelled": true}, nil

	case "gh.repos":
		ghPath, err := c.lookPath("gh")
		if err != nil {
			return nil, errors.New("gh is not installed")
		}
		out, errOut, err := c.run(ctx, ghPath, "repo", "list", "--limit", "25", "--json", "nameWithOwner,description,url")
		if err != nil {
			msg := strings.TrimSpace(errOut)
			if msg == "" {
				msg = err.Error()
			}
			return nil, fmt.Errorf("gh repo list: %s", msg)
		}
		var repos []map[string]any
		if err := json.Unmarshal([]byte(out), &repos); err != nil {
			return nil, err
		}
		return map[string]any{"repos": repos}, nil

	case "project.clone":
		repo, _ := args["repo"].(string)
		return c.cloneRepo(ctx, strings.TrimSpace(repo))

	case "tailscale.serve":
		if c.ServeTarget == "" {
			return nil, errors.New("Tailscale Serve has no target configured on this host")
		}
		ts := c.tailscaleBinary()
		if ts == "" {
			return nil, errors.New("tailscale CLI not found")
		}
		_, errOut, err := c.run(ctx, ts, "serve", "--bg", "--https=7443", c.ServeTarget)
		if err != nil {
			msg := strings.TrimSpace(errOut)
			if msg == "" {
				msg = err.Error()
			}
			return nil, fmt.Errorf("tailscale serve: %s", msg)
		}
		return map[string]any{"target": c.ServeTarget}, nil

	default:
		return nil, fmt.Errorf("unknown setup action %q", action)
	}
}

func (c *Checker) cloneRepo(ctx context.Context, repo string) (map[string]any, error) {
	if !repoRe.MatchString(repo) {
		return nil, fmt.Errorf("invalid repository %q (expected owner/name)", repo)
	}
	if c.Home == "" {
		return nil, errors.New("home directory unknown")
	}
	ghPath, err := c.lookPath("gh")
	if err != nil {
		return nil, errors.New("gh is not installed")
	}
	name := strings.SplitN(repo, "/", 2)[1]
	if name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return nil, errors.New("invalid repository name")
	}
	base := filepath.Join(c.Home, "git")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, err
	}
	target := filepath.Join(base, name)
	if _, err := os.Stat(target); err == nil {
		return map[string]any{"path": target, "existed": true}, nil
	}
	_, errOut, err := c.run(ctx, ghPath, "repo", "clone", repo, target)
	if err != nil {
		msg := strings.TrimSpace(errOut)
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("gh repo clone: %s", msg)
	}
	return map[string]any{"path": target}, nil
}

// installGH downloads the official GitHub CLI binary into <data>/tools/gh.
func (c *Checker) installGH(ctx context.Context) (string, error) {
	dir := c.toolsDir()
	if dir == "" {
		return "", errors.New("tools directory is not configured")
	}
	osName := c.osName()
	arch := c.archName()
	var suffix string
	switch osName {
	case "darwin":
		if arch != "arm64" && arch != "amd64" {
			return "", fmt.Errorf("unsupported macOS arch %s", arch)
		}
		suffix = fmt.Sprintf("macOS_%s.zip", arch)
	case "linux":
		if arch != "arm64" && arch != "amd64" {
			return "", fmt.Errorf("unsupported Linux arch %s", arch)
		}
		suffix = fmt.Sprintf("linux_%s.tar.gz", arch)
	default:
		return "", fmt.Errorf("unsupported OS %s", osName)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/cli/cli/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch gh release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("fetch gh release: HTTP %d", resp.StatusCode)
	}
	var rel struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	var dlURL string
	for _, a := range rel.Assets {
		if strings.HasSuffix(a.Name, suffix) {
			dlURL = a.URL
			break
		}
	}
	if dlURL == "" {
		return "", fmt.Errorf("no gh asset ending in %s", suffix)
	}

	dlReq, _ := http.NewRequestWithContext(ctx, "GET", dlURL, nil)
	dlResp, err := c.httpClient().Do(dlReq)
	if err != nil {
		return "", fmt.Errorf("download gh: %w", err)
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != 200 {
		return "", fmt.Errorf("download gh: HTTP %d", dlResp.StatusCode)
	}
	archive, err := io.ReadAll(io.LimitReader(dlResp.Body, 64<<20))
	if err != nil {
		return "", err
	}

	var bin []byte
	if strings.HasSuffix(suffix, ".zip") {
		bin, err = extractFromZip(archive, "/bin/gh")
	} else {
		bin, err = extractFromTarGz(archive, "/bin/gh")
	}
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "gh")
	if err := fsutil.WriteAtomic(dst, bin, 0o755); err != nil {
		return "", err
	}
	EnsureToolsOnPath(dir)
	return dst, nil
}

// EnsureToolsOnPath appends dir to the process PATH if it is not already there.
func EnsureToolsOnPath(dir string) {
	if dir == "" {
		return
	}
	cur := os.Getenv("PATH")
	for _, p := range filepath.SplitList(cur) {
		if p == dir {
			return
		}
	}
	if cur == "" {
		os.Setenv("PATH", dir)
	} else {
		os.Setenv("PATH", cur+string(os.PathListSeparator)+dir)
	}
}

func (c *Checker) startGHLogin() (GHAuthState, error) {
	ghPath, err := c.lookPath("gh")
	if err != nil {
		return GHAuthState{}, errors.New("gh is not installed")
	}
	c.cancelGHLogin()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	cmd := exec.CommandContext(ctx, ghPath, "auth", "login", "-h", "github.com", "-p", "https", "-w", "-s", "repo,read:org")
	cmd.Env = append(os.Environ(), "BROWSER=true", "GH_PROMPT_DISABLED=0")
	// Send a newline so gh passes "Press Enter to open github.com in your browser..."
	cmd.Stdin = strings.NewReader("\n")
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		cancel()
		pw.Close()
		pr.Close()
		return GHAuthState{}, fmt.Errorf("gh auth login: %w", err)
	}

	sess := &ghLoginSession{
		state:  GHAuthState{Running: true, URL: "https://github.com/login/device"},
		cancel: cancel,
	}
	c.mu.Lock()
	c.ghAuth = sess
	c.mu.Unlock()

	gotCode := make(chan struct{})
	var once sync.Once
	signalCode := func() { once.Do(func() { close(gotCode) }) }

	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			line := sc.Text()
			c.mu.Lock()
			if m := ghCodeRe.FindStringSubmatch(line); len(m) == 2 && sess.state.Code == "" {
				sess.state.Code = m[1]
				signalCode()
			}
			if u := ghURLRe.FindString(line); u != "" {
				sess.state.URL = u
			}
			c.mu.Unlock()
		}
	}()

	go func() {
		err := cmd.Wait()
		pw.Close()
		c.mu.Lock()
		sess.state.Running = false
		if err != nil && ctx.Err() == nil {
			sess.state.Error = err.Error()
		} else if err == nil {
			sess.state.Done = true
		}
		c.mu.Unlock()
		signalCode()
	}()

	select {
	case <-gotCode:
	case <-time.After(5 * time.Second):
	}
	c.mu.Lock()
	st := sess.state
	c.mu.Unlock()
	return st, nil
}

func (c *Checker) cancelGHLogin() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ghAuth != nil {
		if c.ghAuth.cancel != nil {
			c.ghAuth.cancel()
		}
		c.ghAuth = nil
	}
}

func extractFromZip(data []byte, suffix string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, suffix) {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, 64<<20))
		}
	}
	return nil, fmt.Errorf("%s not found in zip", suffix)
}

func extractFromTarGz(data []byte, suffix string) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(h.Name, suffix) {
			return io.ReadAll(io.LimitReader(tr, 64<<20))
		}
	}
	return nil, fmt.Errorf("%s not found in tar.gz", suffix)
}

func scanRoots(roots []string) []Repo {
	var out []Repo
	seen := map[string]bool{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			p := filepath.Join(root, e.Name())
			if seen[p] {
				continue
			}
			if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
				seen[p] = true
				out = append(out, Repo{Name: e.Name(), Path: p})
			}
		}
	}
	return out
}

func parseMajor(v string) (int, error) {
	parts := strings.SplitN(strings.TrimSpace(v), ".", 2)
	return strconv.Atoi(parts[0])
}

func parseGHUser(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if i := strings.Index(line, "account "); i >= 0 {
			rest := strings.TrimSpace(line[i+len("account "):])
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				return strings.TrimRight(fields[0], "(),:")
			}
		}
	}
	return ""
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
