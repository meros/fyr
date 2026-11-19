package heavy

import (
	"strings"
	"testing"
)

// V0.job-limits: a runner that sizes its workers from the host's CPUs gets the
// flag to add in the gate hook's notice.
func TestWorkerHint(t *testing.T) {
	cases := []struct {
		cmd  string
		want []string
	}{
		{"npx playwright test e2e/", []string{"`--workers=3` to `playwright test`"}},
		{"cd web && yarn playwright test --project=chromium", []string{"--workers=3"}},
		{"npx playwright test --workers=2", nil},
		{"npx playwright test -j 2", nil},
		{"npx playwright install chromium", nil},
		{"yarn turbo run build lint", []string{"`--concurrency=3` to `turbo`"}},
		{"turbo build --concurrency=4", nil},
		{"npx playwright test && turbo run test", []string{"--workers=3", "--concurrency=3"}},
		{"go test ./...", nil},
		{"git commit -m 'turbo test'", nil},
	}
	for _, c := range cases {
		got := WorkerHint(c.cmd, 3)
		if (got == "") != (c.want == nil) {
			t.Errorf("%q: %q", c.cmd, got)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%q: %q lacks %q", c.cmd, got, w)
			}
		}
	}
}
