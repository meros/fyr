package agycli

import (
	"os"
	"regexp"
	"strings"
)

// The tool result of an `agy` session is the tool's own output wrapped in
// agy's envelope (V0.agy-result-envelope): two timestamps, the exit code,
// and a header before the output. Fyr keeps the output, because its readers
// were written against the CLI's own result text: PR detection matches the
// lines `gh pr create` printed, the activity summary reads the first line,
// and the owner reads the same text on the thread. A long output arrives as
// a preview and a file:// path to the full text; that file is on this host,
// so the full output replaces the preview.

var (
	// envelopePrefix is the two timestamp lines.
	envelopePrefix = regexp.MustCompile(`^Created At: [^\n]*\nCompleted At: [^\n]*\n`)
	// envelopeExit is the exit-code line; the output follows it.
	envelopeExit = regexp.MustCompile(`(?m)^Command exited with code -?\d+\.\n`)

	truncatedIntro    = "The tool's output was truncated due to being too long. You can find the full output in a file at file://"
	truncatedScissors = "Here is a preview with the middle of the output truncated:\n"
)

// unwrapToolResult returns a tool result's own output, or s as it is when it
// carries no envelope of agy's.
func unwrapToolResult(s string) string {
	if strings.HasPrefix(s, truncatedIntro) {
		if full, ok := readTruncated(s); ok {
			return unwrapToolResult(full)
		}
		// The file is gone (the session's brain folder was cleaned up): cut
		// its sentence before the preview, which carries an envelope too.
		cut := strings.Index(s, truncatedScissors)
		if cut < 0 {
			return s
		}
		return unwrapToolResult(s[cut+len(truncatedScissors):])
	}
	if s == "" {
		return s
	}
	body := s
	if loc := envelopePrefix.FindStringIndex(body); loc != nil {
		body = body[loc[1]:]
	}
	loc := envelopeExit.FindStringIndex(body)
	if loc == nil {
		return s // no envelope, or not one Fyr knows: never cut a tool's own text
	}
	body = body[loc[1]:]
	body = strings.TrimPrefix(body, "\n")
	body = strings.TrimPrefix(body, "Command output:\n")
	return body
}

// readTruncated reads the full output a truncated result points at: the
// regular file under agy's own brain folder that the sentence names.
func readTruncated(s string) (string, bool) {
	rest := s[len(truncatedIntro):]
	end := strings.IndexAny(rest, "\n .")
	if end <= 0 {
		return "", false
	}
	path := rest[:end]
	if !strings.HasPrefix(path, "/") || !strings.Contains(path, "/.system_generated/") {
		return "", false
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 16<<20 {
		return "", false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}
