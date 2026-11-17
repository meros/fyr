package heavy

import "testing"

// #174: a replay of real commands from Fyr's thread logs (16–17 Nov). A heavy command counts only where the shell
// runs it: not inside a heredoc, a grep pattern, a sed script or a commit message. `npm test` (the web unit tests, 3 s
// at the median) and `npm run build` (9 s) are not heavy by default; the host setting can add them.
func TestH174ReplayOfRealCommands(t *testing.T) {
	m := Default()
	for cmd, want := range map[string]string{
		"cd /home/meros/git/personal/fyr-quota && timeout 590 nix develop -c sh -c 'cd web && npm run test:layout 2>&1 | tail -4' 2>&1 | grep -v dirty; uptime":            "npm run test:layout",
		`nix develop -c sh -c 'go vet ./... && go test -count=1 ./... 2>&1 | grep -v "no test files" | grep -v "^ok" ; echo exit=$?' 2>&1 | grep -v "^warning" | tail -20`: "go test ./...",
		"cd /home/meros/src/fyr/web && export PATH=$PATH:/etc/profiles/per-user/meros/bin && npm run build 2>&1 | tail -2; ls dist":                                        "",
		"cd /home/meros/git/personal/fyr-projcut && nix develop -c go test ./... 2>&1 | grep -v 'warning: Git' | tail -40":                                                 "go test ./...",
		"cd /x/sc-33-thread && (cd web && npm ci --no-audit --no-fund --silent) && make test-web 2>&1 | tail -15":                                                          "make test-web",
		"cd /x/web && npm test 2>&1 | tail -12":                                                              "",
		`grep -n "test-go\\|^GO\\|go " Makefile | head; which nix`:                                           "",
		"sed -n 25,50p internal/cli/claudecli/claudecli.go; cat internal/agent/title_test.go | sed -n 1,60p": "",
		"python3 - <<'EOF'\nimport os\nos.system(\"make test\")\nEOF\necho done":                             "",
		`git commit -qm "V0.142: make test passes" && git push`:                                              "",
		"FOO=1 npx playwright test web/test":                                                                 "playwright test",
		"make -C web test 2>&1 | tail":                                                                       "make test",
		"timeout 900 nix develop -c make test-go":                                                            "make test-go",
		"cd ~/locked/norban/Norban && yarn screenshot:diff --project desktop":                                "yarn screenshot:diff",
	} {
		if got := m.Class(cmd); got != want {
			t.Errorf("%q: got %q, want %q", cmd, got, want)
		}
	}
}

func TestH174KindOfAClass(t *testing.T) {
	for class, want := range map[string]string{
		"make test": "test", "go test ./...": "test", "make check": "test", "npm run test:layout": "e2e",
		"yarn screenshot:diff": "e2e", "playwright test": "e2e", "make e2e": "e2e", "nix build": "build", "yarn build": "build",
	} {
		if got := KindOf(class); got != want {
			t.Errorf("%q: got %q, want %q", class, got, want)
		}
	}
}
