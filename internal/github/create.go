package github

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/meros/fyr/internal/core"
)

// CreatePRReq is what gh pr create takes (V0.pr-create-tool).
type CreatePRReq struct {
	Dir       string
	Title     string
	Body      string
	Head      string
	Base      string
	Draft     bool
	Labels    []string
	Reviewers []string
}

// CreatePR opens a GitHub pull request with gh pr create and returns its
// canonical PR URL (V0.pr-create-tool).
func CreatePR(ctx context.Context, gh Runner, req CreatePRReq) (string, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return "", errors.New("title is required")
	}
	if strings.TrimSpace(req.Dir) == "" {
		return "", errors.New("dir is required")
	}
	for _, flag := range []struct{ name, val string }{
		{"head", req.Head},
		{"base", req.Base},
	} {
		if strings.HasPrefix(strings.TrimSpace(flag.val), "-") {
			return "", fmt.Errorf("%s cannot start with '-'", flag.name)
		}
	}
	args := []string{"pr", "create", "--title", title, "--body", req.Body}
	if h := strings.TrimSpace(req.Head); h != "" {
		args = append(args, "--head", h)
	}
	if b := strings.TrimSpace(req.Base); b != "" {
		args = append(args, "--base", b)
	}
	if req.Draft {
		args = append(args, "--draft")
	}
	for _, l := range req.Labels {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "-") {
			return "", fmt.Errorf("label %q cannot start with '-'", l)
		}
		args = append(args, "--label", l)
	}
	for _, r := range req.Reviewers {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if strings.HasPrefix(r, "-") {
			return "", fmt.Errorf("reviewer %q cannot start with '-'", r)
		}
		args = append(args, "--reviewer", r)
	}
	out, err := gh(ctx, args...)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if u, ok := core.CanonicalPR(strings.TrimSpace(line)); ok {
			return u, nil
		}
	}
	return "", fmt.Errorf("gh pr create returned no pull request URL: %s", strings.TrimSpace(string(out)))
}
