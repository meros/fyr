package github

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// V0.review-hangs (5): gh's stderr (an update notice) never reaches the
// output a caller parses as JSON or as an HTTP head; it is in the error of
// a failed run.
func TestGhKeepsStderrOutOfOutput(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gh")
	script := "#!/bin/sh\necho 'A new release of gh is available' >&2\n" +
		"if [ \"$1\" = fail ]; then echo 'HTTP 304' >&2; exit 1; fi\n" +
		"pwd\nprintf 'HTTP/2.0 200 OK\\r\\nEtag: \"x\"\\r\\n\\r\\n{\"a\":1}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	old := ghBin
	ghBin = bin
	t.Cleanup(func() { ghBin = old })

	out, err := GhIn(dir)(context.Background(), "api", "-i")
	if err != nil {
		t.Fatal(err)
	}
	pwd, rest, _ := strings.Cut(string(out), "\n")
	if strings.Contains(string(out), "new release") {
		t.Fatalf("stderr in the output: %q", out)
	}
	if real, _ := filepath.EvalSymlinks(dir); pwd != dir && pwd != real {
		t.Fatalf("ran in %q, want %q", pwd, dir)
	}
	if status, _, body := splitHTTP([]byte(rest)); status != 200 || string(body) != `{"a":1}` {
		t.Fatalf("status %d body %q", status, body)
	}
	_, err = Gh(context.Background(), "fail")
	if err == nil || !strings.Contains(err.Error(), "HTTP 304") || !notModified(nil, err) {
		t.Fatalf("a failed run's stderr is not in its error: %v", err)
	}
}
