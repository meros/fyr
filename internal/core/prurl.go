package core

import (
	"fmt"
	"regexp"
	"strconv"
)

// prPageRe is a whole value that is a PR URL. A value is never a URL found
// inside it, so "--subject=x <url>" is not a PR URL and never reaches gh
// (review 2026-11-15). A PR's own pages (/files, /checks) and a query or
// fragment are allowed after the number.
var prPageRe = regexp.MustCompile(`^https://github\.com/([\w.-]+/[\w.-]+)/pull/(\d+)(?:/[\w/-]*)?(?:[?#]\S*)?$`)

// ParsePR splits a GitHub PR URL into "owner/repo" and the number.
func ParsePR(url string) (repo string, number int, ok bool) {
	m := prPageRe.FindStringSubmatch(url)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], n, true
}

// CanonicalPR is the PR URL in the one form gh is given:
// https://github.com/owner/repo/pull/N.
func CanonicalPR(url string) (string, bool) {
	repo, n, ok := ParsePR(url)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("https://github.com/%s/pull/%d", repo, n), true
}
