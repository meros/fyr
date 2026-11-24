package setup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAllReadyMac(t *testing.T) {
	c := &Checker{
		OS:          "darwin",
		Host:        "macbook",
		Home:        t.TempDir(),
		ServeTarget: "http://127.0.0.1:7778",
		LookPath:    func(file string) (string, error) { return "/usr/local/bin/" + file, nil },
		FreeBytes:   func(string) (uint64, error) { return 50 << 30, nil },
		Providers:   func() []ProviderInfo { return []ProviderInfo{{ID: "claude", Name: "Claude", Status: "ready"}} },
		Projects:    func() int { return 2 },
		Tailscale:   func() TailscaleInfo { return TailscaleInfo{Available: true, DNSName: "mac.tail.ts.net"} },
		Peers:       func(context.Context) PeerInfo { return PeerInfo{Paired: 1} },
		Exec: func(_ context.Context, name string, args ...string) (string, string, error) {
			switch {
			case name == "sw_vers":
				return "15.2\n", "", nil
			case name == "xcode-select":
				return "/Library/Developer/CommandLineTools\n", "", nil
			case strings.HasSuffix(name, "gh"):
				return "Logged in to github.com account alice (keyring)\n  - Token scopes: 'gist', 'read:org', 'repo'\n", "", nil
			case strings.HasSuffix(name, "tailscale"):
				return `{"Web":{"mac.tail.ts.net:7443":{}}}`, "", nil
			}
			return "", "", nil
		},
	}

	r := c.Check(context.Background())
	if !r.Ready || r.Done != 6 || r.Total != 6 {
		t.Fatalf("got ready=%v done=%d/%d steps=%+v", r.Ready, r.Done, r.Total, r.Steps)
	}
	if r.Steps[0].Title != "This Mac" {
		t.Fatalf("step 0 title = %q", r.Steps[0].Title)
	}
	if !strings.Contains(r.Steps[2].Reason, "alice") {
		t.Fatalf("github reason = %q", r.Steps[2].Reason)
	}
	// One word for the thing Settings calls a host: no step says "install" or "machine" for it.
	if p := r.Steps[5]; p.Title != "Other hosts" || p.Reason != "Paired with 1 other host" {
		t.Fatalf("peers step = %q %q", p.Title, p.Reason)
	}
}

func TestCheckFreshMacReportsFixablesAndDiscoversRepos(t *testing.T) {
	home := t.TempDir()
	repoDir := filepath.Join(home, "git", "demo")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Checker{
		OS:          "darwin",
		Host:        "fresh-mac",
		Home:        home,
		ServeTarget: "http://127.0.0.1:7778",
		LookPath: func(file string) (string, error) {
			if file == "claude" || file == "tailscale" {
				return "/opt/homebrew/bin/" + file, nil
			}
			return "", errors.New("not found")
		},
		FreeBytes: func(string) (uint64, error) { return 40 << 30, nil },
		Providers: func() []ProviderInfo { return []ProviderInfo{{ID: "claude", Name: "Claude", Status: "needs-login"}} },
		Projects:  func() int { return 0 },
		Tailscale: func() TailscaleInfo { return TailscaleInfo{Available: true, DNSName: "fresh.tail.ts.net"} },
		Peers:     func(context.Context) PeerInfo { return PeerInfo{} },
		Exec: func(_ context.Context, name string, args ...string) (string, string, error) {
			switch {
			case name == "sw_vers":
				return "15.0\n", "", nil
			case name == "xcode-select":
				return "", "no developer tools", errors.New("exit 2")
			case name == "git":
				return "git@github.com:alice/demo.git\n", "", nil
			case strings.HasSuffix(name, "tailscale"):
				return `{}`, "", nil
			}
			return "", "", errors.New("unexpected")
		},
	}

	r := c.Check(context.Background())
	if r.Ready || r.Done != 0 || r.Total != 6 {
		t.Fatalf("got ready=%v done=%d/%d", r.Ready, r.Done, r.Total)
	}
	if r.Steps[0].Status != Fixable || len(r.Steps[0].Fixes) != 1 || r.Steps[0].Fixes[0].ID != "clt" {
		t.Fatalf("machine step = %+v", r.Steps[0])
	}
	if r.Steps[1].Status != Fixable {
		t.Fatalf("claude step = %+v", r.Steps[1])
	}
	if r.Steps[2].Status != Fixable || r.Steps[2].Fixes[0].ID != "gh.install" {
		t.Fatalf("github step = %+v", r.Steps[2])
	}
	if r.Steps[3].Status != Fixable {
		t.Fatalf("project step = %+v", r.Steps[3])
	}
	if r.Steps[4].Status != Fixable || r.Steps[4].Fixes[0].ID != "tailscale.serve" {
		t.Fatalf("phone step = %+v", r.Steps[4])
	}
	if !r.Steps[4].Optional || !r.Steps[5].Optional {
		t.Fatalf("steps 5 and 6 should be optional")
	}
}

func TestFixesCLTAndTailscaleAndClone(t *testing.T) {
	home := t.TempDir()
	var started []string
	var ran []string
	c := &Checker{
		OS:          "darwin",
		Home:        home,
		ServeTarget: "http://127.0.0.1:7778",
		LookPath:    func(file string) (string, error) { return "/usr/local/bin/" + file, nil },
		Start: func(name string, args ...string) error {
			started = append(started, name+" "+strings.Join(args, " "))
			return nil
		},
		Exec: func(_ context.Context, name string, args ...string) (string, string, error) {
			ran = append(ran, name+" "+strings.Join(args, " "))
			return "", "", nil
		},
	}

	if _, err := c.Fix(context.Background(), "clt", nil); err != nil {
		t.Fatalf("clt: %v", err)
	}
	if len(started) != 1 || started[0] != "/usr/bin/xcode-select --install" {
		t.Fatalf("started = %v", started)
	}
	if _, err := c.Fix(context.Background(), "tailscale.serve", nil); err != nil {
		t.Fatalf("tailscale.serve: %v", err)
	}
	if len(ran) != 1 || !strings.Contains(ran[0], "serve --bg --https=7443 http://127.0.0.1:7778") {
		t.Fatalf("ran = %v", ran)
	}
	if _, err := c.Fix(context.Background(), "project.clone", map[string]any{"repo": "../evil"}); err == nil {
		t.Fatalf("expected invalid repo to fail")
	}
	res, err := c.Fix(context.Background(), "project.clone", map[string]any{"repo": "meros/fyr"})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	m := res.(map[string]any)
	wantPath := filepath.Join(home, "git", "fyr")
	if m["path"] != wantPath {
		t.Fatalf("clone path = %v, want %s", m["path"], wantPath)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestInstallGHDownloadsIntoToolsDir(t *testing.T) {
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	fw, _ := zw.Create("gh_2.70.0_macOS_arm64/bin/gh")
	fw.Write([]byte("#!/bin/sh\necho fake-gh\n"))
	zw.Close()

	tools := filepath.Join(t.TempDir(), "tools")
	sum := sha256.Sum256(zbuf.Bytes())
	sums := hex.EncodeToString(sum[:]) + "  gh_2.70.0_macOS_arm64.zip\n"
	c := ghReleaseChecker(tools, zbuf.Bytes(), sums)

	got, err := c.installGH(context.Background())
	if err != nil {
		t.Fatalf("installGH: %v", err)
	}
	if got != filepath.Join(tools, "gh") {
		t.Fatalf("path = %q", got)
	}
	b, err := os.ReadFile(got)
	if err != nil || !strings.Contains(string(b), "fake-gh") {
		t.Fatalf("read installed gh: %q %v", string(b), err)
	}
}

// ghReleaseChecker serves a gh release whose macOS arm64 asset is archive
// and whose checksums file is sums.
func ghReleaseChecker(tools string, archive []byte, sums string) *Checker {
	return &Checker{
		OS:       "darwin",
		Arch:     "arm64",
		ToolsDir: tools,
		HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body := io.NopCloser(bytes.NewReader(archive))
			switch {
			case strings.Contains(r.URL.Host, "api.github.com"):
				body = io.NopCloser(strings.NewReader(`{"assets":[` +
					`{"name":"gh_2.70.0_checksums.txt","browser_download_url":"https://dl.example/sums.txt"},` +
					`{"name":"gh_2.70.0_macOS_arm64.zip","browser_download_url":"https://dl.example/gh.zip"}]}`))
			case strings.HasSuffix(r.URL.Path, "sums.txt"):
				body = io.NopCloser(strings.NewReader(sums))
			}
			return &http.Response{StatusCode: 200, Body: body, Header: http.Header{}}, nil
		})},
	}
}

// V0.review-hangs (6): a gh download that does not match the release's
// checksum, or that the release does not list, or that is larger than the
// cap, is refused and nothing is installed.
func TestInstallGHRefusesBadDownload(t *testing.T) {
	archive := []byte("not the archive the checksum names")
	other := sha256.Sum256([]byte("something else"))
	good := sha256.Sum256(archive)
	for name, sums := range map[string]string{
		"mismatch": hex.EncodeToString(other[:]) + "  gh_2.70.0_macOS_arm64.zip\n",
		"unlisted": hex.EncodeToString(good[:]) + "  gh_2.70.0_linux_amd64.tar.gz\n",
	} {
		tools := filepath.Join(t.TempDir(), "tools")
		if _, err := ghReleaseChecker(tools, archive, sums).installGH(context.Background()); err == nil {
			t.Fatalf("%s: installed", name)
		}
		if _, err := os.Stat(filepath.Join(tools, "gh")); err == nil {
			t.Fatalf("%s: a gh binary was written", name)
		}
	}
	if _, err := readCapped(bytes.NewReader(make([]byte, 11)), 10); err == nil {
		t.Fatal("a body over the cap was cut short, not refused")
	}
	if b, err := readCapped(bytes.NewReader(make([]byte, 10)), 10); err != nil || len(b) != 10 {
		t.Fatalf("a body at the cap: %d %v", len(b), err)
	}
}

// V0.forwarded-ports: a front whose forward ports Tailscale Serve does not
// publish says so on the Phone step, and the fix publishes each to its socket.
func TestPhoneStepPublishesTheForwardPorts(t *testing.T) {
	status := `{"Web":{"desk.tail.ts.net:7443":{}}}`
	var ran []string
	c := &Checker{
		OS: "linux", Host: "desk", Home: t.TempDir(), ServeTarget: "unix:/data/serve.sock",
		ForwardTargets: map[int]string{7451: "unix:/data/serve-7451.sock", 7450: "unix:/data/serve-7450.sock"},
		LookPath:       func(file string) (string, error) { return "/usr/bin/" + file, nil },
		FreeBytes:      func(string) (uint64, error) { return 50 << 30, nil },
		Tailscale:      func() TailscaleInfo { return TailscaleInfo{Available: true, DNSName: "desk.tail.ts.net"} },
		Exec: func(_ context.Context, name string, args ...string) (string, string, error) {
			if strings.HasSuffix(name, "tailscale") {
				if len(args) > 1 && args[1] == "status" {
					return status, "", nil
				}
				ran = append(ran, strings.Join(args, " "))
			}
			return "", "", nil
		},
	}
	phone := func() Step {
		for _, s := range c.Check(context.Background()).Steps {
			if s.ID == "phone" {
				return s
			}
		}
		t.Fatal("no phone step")
		return Step{}
	}
	s := phone()
	if s.Status != Fixable || len(s.Fixes) != 1 || s.Fixes[0].ID != "tailscale.serve" || !strings.Contains(s.Reason, "7450, 7451") {
		t.Fatalf("with only 7443 published: %+v", s)
	}
	if _, err := c.Fix(context.Background(), "tailscale.serve", nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"serve --bg --https=7443 unix:/data/serve.sock", "serve --bg --https=7450 unix:/data/serve-7450.sock", "serve --bg --https=7451 unix:/data/serve-7451.sock"}
	if strings.Join(ran, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the fix ran %q", ran)
	}
	status = `{"Web":{"desk.tail.ts.net:7443":{},"desk.tail.ts.net:7450":{},"desk.tail.ts.net:7451":{}}}`
	if s := phone(); s.Status != OK {
		t.Fatalf("with every port published: %+v", s)
	}
	// A front that forwards nothing asks for nothing more.
	c.ForwardTargets, status = nil, `{"Web":{"desk.tail.ts.net:7443":{}}}`
	if s := phone(); s.Status != OK {
		t.Fatalf("without forward ports: %+v", s)
	}
}
