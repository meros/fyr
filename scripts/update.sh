#!/usr/bin/env bash
# Daily self-update: builds and tests origin's default branch in a clean
# checkout of its own, links it as next and sends fyrd SIGHUP, so the
# launcher switches to it with its probation and rollback (as a deploy does,
# V0.13). Agents keep running: the launcher's runner holds them (V0.90).
# Does nothing when that commit is already built. scripts/install-macos.sh
# runs it from launchd once a day.
#
#	scripts/update.sh [-data DIR] [REPO_URL]
set -euo pipefail

data="$HOME/.local/share/fyr"
if [ "${1:-}" = -data ]; then data="$2"; shift 2; fi
repo="${1:-https://github.com/meros/fyr.git}"
src="$data/src"
say() { echo "$(date '+%F %T') fyr update: $*"; }

if [ ! -d "$src/.git" ]; then
  git clone --quiet "$repo" "$src"
fi
git -C "$src" fetch --quiet origin
git -C "$src" remote set-head origin --auto >/dev/null
head="$(git -C "$src" rev-parse origin/HEAD)"
if [ "$head" = "$(cat "$data/src-built" 2>/dev/null)" ]; then
  say "${head:0:12} is built already"
  exit 0
fi
git -C "$src" checkout --quiet --force --detach "$head"
git -C "$src" clean -qfdx -e web/node_modules

id="$(date +%Y%m%d-%H%M%S)-update"
out="$data/builds/$id"
make=(make -C "$src")
# Like a deploy: the checkout's own toolchain when Nix is there (decision 0007).
if [ -f "$src/flake.nix" ] && command -v nix >/dev/null; then
  make=(nix develop "$src" --command "${make[@]}")
fi
say "building ${head:0:12} into $out"
if ! "${make[@]}" build OUT="$out" || ! "${make[@]}" test; then
  rm -rf "$out"
  say "build or tests failed for ${head:0:12}; nothing was switched"
  exit 1
fi
ln -sfn "builds/$id" "$data/next"
echo "$head" >"$data/src-built"
install -m 0755 "$src/scripts/update.sh" "$data/update.sh" # the next run is the new script
if pkill -HUP -f "fyrd serve -data $data( |\$)"; then
  say "switching to ${head:0:12}"
else
  say "fyrd is not running; next is linked for the next switch"
fi
