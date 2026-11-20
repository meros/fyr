package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Decision 0007: agents in Fyr see the user's profile, like a login shell.
// On NixOS the unit's PATH comes first and the profile folders that exist
// follow.
func TestAgentPathNixOS(t *testing.T) {
	exists := func(p string) bool { return p != "/home/u/.nix-profile/bin" }
	got := AgentPath(PathInput{Unit: "/nix/store/x/bin:/run/current-system/sw/bin", Home: "/home/u", User: "u", Exists: exists})
	want := "/nix/store/x/bin:/run/current-system/sw/bin:/run/wrappers/bin:/home/u/.local/bin:/etc/profiles/per-user/u/bin"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// V0.linux-without-nix (2): on a Linux without Nix the login shell's PATH
// joins systemd's default, and no NixOS folder is added when it is missing.
func TestAgentPathLogin(t *testing.T) {
	home := "/home/u"
	exists := func(p string) bool { return p == "/home/u/.local/bin" }
	got := AgentPath(PathInput{
		Unit:  "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		Login: "/home/u/.nvm/versions/node/v22/bin:/home/u/.local/bin:/usr/local/bin:/usr/bin:/bin:/home/u/go/bin",
		Home:  home, User: "u", Exists: exists,
	})
	want := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/home/u/.nvm/versions/node/v22/bin:/home/u/.local/bin:/home/u/go/bin"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// V0.linux-without-nix (2): the config's agentPath comes first, `~/` is the
// home folder, and a folder already there is not repeated.
func TestAgentPathConfig(t *testing.T) {
	got := AgentPath(PathInput{
		Config: []string{"~/bin", "/opt/claude/bin", "/usr/bin"},
		Unit:   "/usr/bin:/bin",
		Login:  "/home/u/bin:/usr/bin",
		Home:   "/home/u", User: "u", Exists: func(string) bool { return false },
	})
	want := "/home/u/bin:/opt/claude/bin:/usr/bin:/bin"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// fakeShell writes a login shell that prints body and then what a real one
// prints for LoginPath's command.
func fakeShell(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// A profile's own output does not end up in the PATH; the line after the
// mark does.
func TestLoginPathSkipsProfileOutput(t *testing.T) {
	sh := fakeShell(t, `echo "Welcome"; PATH=/login/bin:/usr/bin; shift 2; eval "$1"; echo bye`)
	got, err := LoginPath(sh, 5*time.Second)
	if err != nil || got != "/login/bin:/usr/bin" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestLoginPathFails(t *testing.T) {
	if _, err := LoginPath("", time.Second); err == nil {
		t.Fatal("no shell: want an error")
	}
	if _, err := LoginPath(fakeShell(t, "echo no path"), 5*time.Second); err == nil {
		t.Fatal("no mark: want an error")
	}
	if _, err := LoginPath(fakeShell(t, "exec sleep 30"), 200*time.Millisecond); err == nil {
		t.Fatal("a profile that waits: want an error")
	}
}

func TestPasswdShell(t *testing.T) {
	passwd := "root:x:0:0:root:/root:/bin/bash\nu:x:1000:1000:U:/home/u:/usr/bin/zsh\n"
	if got := passwdShell(passwd, "u"); got != "/usr/bin/zsh" {
		t.Fatalf("got %q", got)
	}
	if got := passwdShell(passwd, "nobody"); got != "" {
		t.Fatalf("got %q", got)
	}
}
