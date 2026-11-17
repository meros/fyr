// Decision 0029 S1 (#185), #174: heavy commands are recognised by pattern and named by a class.
package heavy

import "testing"

func TestS1ClassOfACommand(t *testing.T) {
	m := Default()
	for cmd, want := range map[string]string{
		"make test":                             "make test",
		"cd /x && make test-go 2>&1 | tail -20": "make test-go",
		"make -j4 check":                        "make check",
		"go test ./...":                         "go test ./...",
		"go test -count=1 ./internal/... 2>&1 | tail": "go test ./...",
		"yarn screenshot:diff --all":                  "yarn screenshot:diff",
		"yarn run test:layout":                        "yarn test:layout",
		"npx playwright test web/test":                "playwright test",
		"nix build .#fyrd":                            "nix build",
		"go test ./internal/slots -run TestX":         "",
		"go build ./cmd/fyrd":                         "",
		"git commit -m 'make test pass'":              "",
		"nix develop -c sh -c 'go test ./...'":        "go test ./...",
		`gh pr create --title "make check green"`:     "",
		`git commit --message="run make test"`:        "",
		"echo hi":                                     "",
	} {
		if got := m.Class(cmd); got != want {
			t.Errorf("%q: got %q, want %q", cmd, got, want)
		}
	}
}

func TestS1PatternsFromTheHostSetting(t *testing.T) {
	m, err := New([]Pattern{{Name: "bazel $1", Re: `\bbazel\s+(test|build)\b`}})
	if err != nil {
		t.Fatal(err)
	}
	if m.Class("bazel test //...") != "bazel test" || m.Class("make test") != "" {
		t.Fatal("the setting replaces the defaults")
	}
	if _, err := New([]Pattern{{Name: "x", Re: "("}}); err == nil {
		t.Fatal("a bad pattern is an error")
	}
}
