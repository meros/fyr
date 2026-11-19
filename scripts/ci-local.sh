#!/usr/bin/env bash
# The PR gate, run on this host instead of GitHub Actions (Actions minutes on a private repo cost money; PRs no
# longer start a workflow). It runs what the workflow's go, web and secrets jobs ran, in the flake's dev shell, on
# the pushed head of the current branch, and posts the result as the commit status `local/test` on that SHA. Fyr
# reads that status as the PR's CI: a merge ask waits for it and a failure is reported like a red check.
#
#   scripts/ci-local.sh          # push first; the head must be on GitHub
#
# The nix package build (`nix build .#fyr`) runs on main only, in the workflow.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

sha=$(git rev-parse HEAD)
branch=$(git rev-parse --abbrev-ref HEAD)
repo=$(gh repo view --json nameWithOwner -q .nameWithOwner)
ctx=local/test

if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
	echo "ci-local: uncommitted changes; commit and push first, the status names a commit" >&2
	exit 2
fi
if [ "$(git rev-parse "origin/$branch" 2>/dev/null || true)" != "$sha" ]; then
	git fetch -q origin "$branch" 2>/dev/null || true
	if [ "$(git rev-parse "origin/$branch" 2>/dev/null || true)" != "$sha" ]; then
		echo "ci-local: HEAD $sha is not pushed to origin/$branch; push first" >&2
		exit 2
	fi
fi

status() { # state description
	gh api -X POST "repos/$repo/statuses/$sha" -f state="$1" -f context="$ctx" -f description="$2" >/dev/null
}

status pending "make test on $(hostname)"
log=$(mktemp -t fyr-ci-local.XXXXXX.log)
started=$(date +%s)
set +e
{
	git fetch -q origin main &&
		nix develop --command bash -c 'make test FYR_LAYOUT_TIMING=0' &&
		nix shell --inputs-from . nixpkgs#gitleaks -c gitleaks git --config .gitleaks.toml --redact \
			--log-opts="origin/main..$sha" .
} 2>&1 | tee "$log"
rc=${PIPESTATUS[0]}
set -e
took=$(($(date +%s) - started))

if [ "$rc" -eq 0 ]; then
	status success "make test and gitleaks passed on $(hostname) in ${took}s"
	echo "ci-local: passed; status $ctx=success on $sha"
else
	status failure "failed on $(hostname) (exit $rc); log: $log"
	echo "ci-local: FAILED (exit $rc); status $ctx=failure on $sha; log $log" >&2
fi
exit "$rc"
