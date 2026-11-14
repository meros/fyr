package claudecli

import (
	"strings"
	"testing"
)

// SPEC 0.5 V0.53: the login's output as setup-token draws it (an OSC 8
// link whose visible text wraps, spaces as cursor moves).
func TestParseLogin(t *testing.T) {
	link := "https://claude.com/cai/oauth/authorize?code=true&client_id=x&scope=user%3Ainference&state=abc"
	out := "\x1b7\x1b8Welcome\x1b[1Cto\x1b[1CClaude\r\n" +
		"\x1b]8;id=a;" + link + "\x07" + link[:40] + "\x1b]8;;\x07\r\n" +
		"\x1b]8;id=a;" + link + "\x07" + link[40:] + "\x1b]8;;\x07\r\n\r\nPaste\x1b[1Ccode\x1b[1Chere\x1b[1Cif\x1b[1Cprompted\x1b[1C>\r\n"
	o := ParseLogin([]byte(out), false)
	if o.URL != link || !o.Prompt || o.Token != "" || o.Line != "" {
		t.Fatalf("%+v", o)
	}
	// Plain pipes (auth login).
	o = ParseLogin([]byte("Opening browser to sign in…\nIf the browser didn't open, visit: "+link+"\nPaste code here if prompted > "), false)
	if o.URL != link || !o.Prompt {
		t.Fatalf("%+v", o)
	}
	tok := "sk-ant-oat01-ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-abc"
	// A token at the very end of the output may still be arriving.
	if o := ParseLogin([]byte(out+"Your OAuth token:\r\n\r\n"+tok[:30]), false); o.Token != "" {
		t.Fatalf("a partial token was taken: %q", o.Token)
	}
	if o := ParseLogin([]byte(out+"Your OAuth token:\r\n\r\n"+tok+"\r\n"), false); o.Token != tok {
		t.Fatalf("token %q", o.Token)
	}
	if o := ParseLogin([]byte(out+tok), true); o.Token != tok {
		t.Fatalf("token at the end of a finished login %q", o.Token)
	}
	o = ParseLogin([]byte("Paste code here if prompted > abc\r\nError: invalid grant for "+tok+"\r\n"), true)
	if !strings.Contains(o.Line, "invalid grant") || strings.Contains(o.Line, "sk-ant") {
		t.Fatalf("line %q", o.Line)
	}
}

// As claude 2.1.322 draws it on a terminal: columns by cursor moves, an
// error and a retry.
func TestParseLoginColumns(t *testing.T) {
	out := "\x1b[2GPaste\x1b[8Gcode\x1b[13Ghere\x1b[18Gif\x1b[21Gprompted\x1b[30G>\r\r\n\x1b[31C\x1b[1A***********code#x\r\r\n" +
		"\x1b[2K\x1b[1A\r\x1b[1C\x1b[5A\x1b[38;5;211mOAuth error: That code wasn't accepted. It may be expired.\r\x1b[1B\x1b[39m\x1b[K\r\x1b[1B Press \x1b[1mEnter\x1b[22m to retry. \x1b[39m\x1b[K"
	o := ParseLogin([]byte(out), false)
	if !o.Prompt || !o.Retry || o.Line != "OAuth error: That code wasn't accepted. It may be expired." {
		t.Fatalf("%+v", o)
	}
	if p := Plain([]byte("Welcome\x1b[9Gto")); p != "Welcome to" {
		t.Fatalf("%q", p)
	}
}

func TestLoginEnvDropsCredentials(t *testing.T) {
	env := LoginEnv([]string{"PATH=/bin", "CLAUDE_CODE_OAUTH_TOKEN=x", "ANTHROPIC_API_KEY=y", "CLAUDE_CONFIG_DIR=/a", "DISPLAY=:0"}, "/cfg")
	got := strings.Join(env, " ")
	if got != "PATH=/bin CLAUDE_CONFIG_DIR=/cfg BROWSER=true TERM=xterm-256color NO_COLOR=1" {
		t.Fatal(got)
	}
	if a, term := LoginArgs(LoginToken); a[0] != "setup-token" || !term {
		t.Fatal(a)
	}
}
