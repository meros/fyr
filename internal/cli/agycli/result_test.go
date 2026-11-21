package agycli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/core"
)

// V0.agy-result-envelope: a tool result is the tool's own output. The
// envelope's timestamps and exit line are cut, a preview is replaced by the
// file it names, and any result without an envelope is left exactly as it is.
func TestUnwrapToolResult(t *testing.T) {
	dir := t.TempDir()
	brain := filepath.Join(dir, "brain", "s1", ".system_generated", "steps", "7")
	if err := os.MkdirAll(brain, 0o700); err != nil {
		t.Fatal(err)
	}
	full := "Created At: 2026-01-01T00:00:00+01:00\nCompleted At: 2026-01-01T00:00:01+01:00\n" +
		"Command exited with code 0.\n\nCommand output:\nhttps://github.com/acme/app/pull/12\n"
	file := filepath.Join(brain, "output.txt")
	if err := os.WriteFile(file, []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, in, want string
	}{
		{
			"plain output",
			"Created At: 2026-01-01T00:00:00+01:00\nCompleted At: 2026-01-01T00:00:01+01:00\n" +
				"Command exited with code 0.\n\nCommand output:\nhttps://github.com/acme/app/pull/12\n",
			"https://github.com/acme/app/pull/12\n",
		},
		{
			"a failure keeps its output",
			"Created At: 2026-01-01T00:00:00+01:00\nCompleted At: 2026-01-01T00:00:01+01:00\n" +
				"Command exited with code 1.\n\nCommand output:\n--- FAIL: TestX\r\n",
			"--- FAIL: TestX\r\n",
		},
		{
			"an envelope with no output",
			"Created At: 2026-01-01T00:00:00+01:00\nCompleted At: 2026-01-01T00:00:01+01:00\n" +
				"Command exited with code 0.\n",
			"",
		},
		{
			"a truncated result is its file",
			truncatedIntro + file + ".\n\n" + truncatedScissors +
				"Created At: 2026-01-01T00:00:00+01:00\nCompleted At: 2026-01-01T00:00:01+01:00\n" +
				"Command exited with code 0.\n\nCommand output:\nhttps://github.com/acme/app/pull/12\n",
			"https://github.com/acme/app/pull/12\n",
		},
		{
			"a truncation whose file is gone keeps its preview",
			truncatedIntro + filepath.Join(dir, "gone.txt") + ".\n\n" + truncatedScissors +
				"Created At: 2026-01-01T00:00:00+01:00\nCompleted At: 2026-01-01T00:00:01+01:00\n" +
				"Command exited with code 0.\n\nCommand output:\nkept\n",
			"kept\n",
		},
		{
			"a tool's own text is never cut",
			"Created At: not our envelope\nsome text\n",
			"Created At: not our envelope\nsome text\n",
		},
		{
			"no envelope at all",
			"ok all tests",
			"ok all tests",
		},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := unwrapToolResult(tc.in); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// A truncated result pointing outside agy's brain folder is not read.
func TestUnwrapToolResultRefusesOtherFiles(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "etc", "passwd")
	if err := os.MkdirAll(filepath.Dir(secret), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte(fullEnvelope("do not read me")), 0o600); err != nil {
		t.Fatal(err)
	}
	in := truncatedIntro + secret + ".\n\n" + truncatedScissors + fullEnvelope("preview")
	got := unwrapToolResult(in)
	if strings.Contains(got, "do not read me") {
		t.Fatalf("read a file outside the brain folder: %q", got)
	}
	if got != "preview" {
		t.Fatalf("got %q", got)
	}
}

// V0.agy-result-envelope: the shape a live `gh pr create` result had. Its
// URL line is what Fyr's PR detection reads, and without the unwrap it was
// hidden behind the timestamps, the exit line and gh's own "Working…" lines.
func TestUnwrapToolResultKeepsGHPROutputUsable(t *testing.T) {
	const live = "Created At: 2026-11-21T04:52:01+01:00\nCompleted At: 2026-11-21T05:58:58+01:00\n" +
		"Command exited with code 0.\n\nCommand output:\nWorking...\r\n\r\n" +
		"Creating pull request for feature-one into main in acme/app\r\n\r\nWorking...\r\n" +
		"https://github.com/acme/app/pull/502\r\n"
	got := unwrapToolResult(live)
	if strings.Contains(got, "Created At") || strings.Contains(got, "Command exited") {
		t.Fatalf("envelope left in the result: %q", got)
	}
	var urls []string
	for _, line := range strings.Split(got, "\n") {
		if u, ok := core.CanonicalPR(strings.TrimSpace(line)); ok {
			urls = append(urls, u)
		}
	}
	if len(urls) != 1 || !strings.HasSuffix(urls[0], "/pull/502") {
		t.Fatalf("PR detection found %v in %q", urls, got)
	}
}

func fullEnvelope(out string) string {
	return "Created At: 2026-01-01T00:00:00+01:00\nCompleted At: 2026-01-01T00:00:01+01:00\n" +
		"Command exited with code 0.\n\nCommand output:\n" + out
}
