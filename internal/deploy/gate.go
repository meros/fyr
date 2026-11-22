package deploy

import (
	"context"
	"fmt"
	"github.com/meros/fyr/internal/proc"
	"strings"
)

// Gate is the check before Fyr approves a deploy ask by itself (V0.111):
// the checkout has no uncommitted or untracked file (the build would
// include it), and its HEAD is the commit origin's default branch points
// at, or an ancestor of it. Such code is code a person already merged. It
// returns HEAD, or why the ask waits for the owner's press.
func Gate(ctx context.Context, run Runner, dir string) (commit, why string) {
	if run == nil {
		run = execRunner
	}
	git := func(args ...string) (string, error) {
		b, err := run(ctx, dir, proc.GitArgv(dir, args...)...)
		return strings.TrimSpace(string(b)), err
	}
	head, err := git("rev-parse", "HEAD")
	if err != nil || head == "" || strings.ContainsAny(head, " \n\t") {
		return "", fmt.Sprintf("Fyr could not read the HEAD of %s", dir)
	}
	st, err := git("status", "--porcelain")
	if err != nil {
		return "", fmt.Sprintf("Fyr could not read the status of %s", dir)
	}
	if st != "" {
		return "", "the checkout has changes that are not committed"
	}
	ls, err := git("ls-remote", "origin", "HEAD")
	main, _, _ := strings.Cut(ls, "\t")
	if err != nil || main == "" || strings.HasPrefix(main, "-") || strings.ContainsAny(main, " \n") {
		return "", "Fyr could not read origin's default branch"
	}
	if head == main {
		return head, ""
	}
	if _, err := git("merge-base", "--is-ancestor", head, main); err != nil {
		return "", fmt.Sprintf("commit %s is not on origin's default branch (pull it first)", short7(head))
	}
	return head, ""
}
