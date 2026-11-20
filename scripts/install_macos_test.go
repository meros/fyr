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
		`curl -fsSL --retry 3 -o "$dl/$f" "$base/releases/download/build-$short/$f"`,
		`ls-remote --tags "$base.git" refs/tags/deployable`,
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
