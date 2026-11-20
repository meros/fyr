package setup

import (
	"archive/zip"
	"bytes"
	"context"
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
	c := &Checker{
		OS:       "darwin",
		Arch:     "arm64",
		ToolsDir: tools,
		HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if strings.Contains(r.URL.Host, "api.github.com") {
				body := `{"assets":[{"name":"gh_2.70.0_macOS_arm64.zip","browser_download_url":"https://dl.example/gh.zip"}]}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(zbuf.Bytes())), Header: http.Header{}}, nil
		})},
	}

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
