package github

// V0.173: conditional REST reads. GitHub does not count an answer of 304
// Not Modified against the rate limit, so a poll that sends the ETag of its
// last answer costs nothing while nothing changed.

import (
	"bytes"
	"context"
	"strings"
	"sync"
)

// etagMax bounds the cache; past it, it starts again.
const etagMax = 1000

type etagEntry struct {
	tag  string
	body []byte
}

var (
	etagMu sync.Mutex
	etags  = map[string]etagEntry{}
)

// RestGet reads a REST path with gh api, sending the ETag of the last
// answer for that path; on 304 it returns that answer's body.
func RestGet(ctx context.Context, gh Runner, path string) ([]byte, error) {
	etagMu.Lock()
	e, ok := etags[path]
	etagMu.Unlock()
	args := []string{"api", "-i", path}
	if ok {
		args = []string{"api", "-i", "-H", "If-None-Match: " + e.tag, path}
	}
	out, err := gh(ctx, args...)
	if ok && notModified(out, err) {
		return e.body, nil
	}
	if err != nil {
		return out, err
	}
	status, head, body := splitHTTP(out)
	if status == 0 { // no head: a runner that does not answer -i (tests)
		return out, nil
	}
	if tag := header(head, "etag"); tag != "" && status == 200 {
		etagMu.Lock()
		if len(etags) >= etagMax {
			etags = map[string]etagEntry{}
		}
		etags[path] = etagEntry{tag: tag, body: body}
		etagMu.Unlock()
	}
	return body, nil
}

// notModified reports a gh api -i answer of 304, whether gh exits 0 or not.
func notModified(out []byte, err error) bool {
	s, _, _ := splitHTTP(out)
	if s == 304 {
		return true
	}
	return err != nil && strings.Contains(err.Error(), "HTTP 304")
}

// splitHTTP splits gh api -i output into the status, the head and the body;
// status 0 when the output has no HTTP head.
func splitHTTP(out []byte) (int, string, []byte) {
	if !bytes.HasPrefix(out, []byte("HTTP/")) {
		return 0, "", out
	}
	head, body := out, []byte(nil)
	if i := bytes.Index(out, []byte("\r\n\r\n")); i >= 0 {
		head, body = out[:i], out[i+4:]
	} else if i := bytes.Index(out, []byte("\n\n")); i >= 0 {
		head, body = out[:i], out[i+2:]
	}
	line, _, _ := strings.Cut(string(head), "\n")
	f := strings.Fields(line)
	status := 0
	if len(f) >= 2 {
		for _, c := range f[1] {
			if c < '0' || c > '9' {
				status = 0
				break
			}
			status = status*10 + int(c-'0')
		}
	}
	return status, string(head), body
}

func header(head, name string) string {
	for _, l := range strings.Split(head, "\n") {
		k, v, ok := strings.Cut(l, ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), name) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// resetETags empties the cache (tests).
func resetETags() {
	etagMu.Lock()
	etags = map[string]etagEntry{}
	etagMu.Unlock()
}
