package scripts

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// V0.release-without-gh: install-macos.sh reads the release without gh,
// loads its agent under a label that names no GitHub account, and stops and
// removes the old label's agents (install and uninstall), so a Mac that had
// com.github.meros.fyr does not run two. It runs on a Mac only; this reads it.
func TestInstallMacosLabelAndNoGh(t *testing.T) {
	if out, err := exec.Command("bash", "-n", "install-macos.sh").CombinedOutput(); err != nil {
		t.Fatalf("bash -n: %v %s", err, out)
	}
	b, err := os.ReadFile("install-macos.sh")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		"label=app.fyr.fyrd\n",
		"old_labels=(com.github.meros.fyr com.github.meros.fyr.update)",
		`for l in "${old_labels[@]}" "$label"; do`, // --uninstall
		`for l in "${old_labels[@]}"; do`,          // install, before the bootstrap
		`rm -f "$agents/$l.plist"`,
		`curl -fsSL --retry 3 -o "$dl/$f" "$base/releases/download/$tag/$f"`,
		`ls-remote --tags "$base.git" "refs/tags/$1"`,
		`sha="$(tag_sha deployable)"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("install-macos.sh lacks %q", want)
		}
	}
	for _, not := range []string{"gh api", "gh release", "gh auth", "label=com.github"} {
		if strings.Contains(src, not) {
			t.Errorf("install-macos.sh still has %q", not)
		}
	}
	if i, j := strings.LastIndex(src, `for l in "${old_labels[@]}"; do`), strings.LastIndex(src, `launchctl bootstrap "$domain" "$plist"`); i < 0 || j < i {
		t.Error("the old labels are not stopped before the new agent is bootstrapped")
	}
}

// V0.versioned-releases: install-macos.sh takes the same --channel and
// --version as install-linux.sh, and --print says which release and channel
// it would install without touching anything.
func TestInstallMacosChannelPrint(t *testing.T) {
	run := func(home string, args ...string) string {
		t.Helper()
		cmd := exec.Command("bash", append([]string{"install-macos.sh", "--print"}, args...)...)
		cmd.Env = append(os.Environ(), "HOME="+home)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	home := t.TempDir()
	if out := run(home); !strings.Contains(out, "channel: stable (updateChannel stable") || !strings.Contains(out, "the newest stable version") {
		t.Errorf("new install:\n%s", out)
	}
	if out := run(home, "--version", "v1.2.3"); !strings.Contains(out, "download version v1.2.3") || !strings.Contains(out, `"updateChannel" in `+home+`/.config/fyr/config.json to "v1.2.3"`) {
		t.Errorf("--version:\n%s", out)
	}
	if es, _ := os.ReadDir(home); len(es) != 0 {
		t.Fatalf("--print wrote into HOME: %v", es)
	}
	os.MkdirAll(home+"/.config/fyr", 0o755)
	os.WriteFile(home+"/.config/fyr/config.json", []byte(`{"wellKnownListen": "127.0.0.1:7779"}`), 0o644)
	if out := run(home); !strings.Contains(out, "channel: main (kept from") || strings.Contains(out, `set "updateChannel"`) {
		t.Errorf("existing config:\n%s", out)
	}
	if out := run(home, "--channel", "stable"); !strings.Contains(out, "channel: stable (updateChannel stable") {
		t.Errorf("--channel stable:\n%s", out)
	}
}
