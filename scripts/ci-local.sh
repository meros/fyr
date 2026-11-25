#!/usr/bin/env bash
# The PR gate run on this host, the fallback for when the self-hosted runner is down: a PR's gate is
# .github/workflows/pr.yml on that runner (V0.ci-self-hosted), and this script runs the same steps (`make test-ci`, the
# Nix packages, gitleaks over the PR's commits) in the flake's dev shell, on the pushed head of the current branch, and
# posts the result as the commit status `local/test` on that SHA. Fyr reads that status as the PR's CI: a merge ask
# waits for it and a failure is reported like a red check, until the runner's `test` passes on the same SHA: then local/test
# no longer counts, and a green local/test stands in for a `test` that is still queued (V0.ci-superseded-checks).
#
#   scripts/ci-local.sh          # push first; the head must be on GitHub
#
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

status pending "make test-ci on $(hostname)"

# One gate at a time on this host (V0.fix-flakes): two make tests at once load the machine until timing-sensitive
# tests fail that pass alone. The lock is held until this script exits; a second gate waits for it, with its status
# pending meanwhile.
lock="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/fyr-ci-local.lock" # one path for a shell and the service alike
exec 9>>"$lock"
if ! flock -n 9; then
	echo "ci-local: another gate runs; waiting" >&2
	status pending "waiting for another gate on $(hostname)"
	flock 9
	status pending "make test-ci on $(hostname)"
fi
log=$(mktemp -t fyr-ci-local.XXXXXX.log)
started=$(date +%s)
set +e
{
	git fetch -q origin main &&
		nix develop --command make test-ci &&
		nix build .#fyr .#fyr-desktop --no-link &&
		nix shell --inputs-from . nixpkgs#gitleaks -c gitleaks git --config .gitleaks.toml --redact \
			--log-opts="origin/main..$sha" .
} 9>&- 2>&1 | tee "$log" # 9>&-: nothing the gate starts (a detached Chrome) keeps the lock
rc=${PIPESTATUS[0]}
set -e
took=$(($(date +%s) - started))

if [ "$rc" -eq 0 ]; then
	status success "make test-ci, nix build and gitleaks passed on $(hostname) in ${took}s"
	echo "ci-local: passed; status $ctx=success on $sha"
else
	status failure "failed on $(hostname) (exit $rc); log: $log"
	echo "ci-local: FAILED (exit $rc); status $ctx=failure on $sha; log $log" >&2
fi
exit "$rc"
