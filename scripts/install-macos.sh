#!/usr/bin/env bash
# Installs Fyr on a Mac as a launchd agent of the logged-in user (the Mac's
# stand-in for the NixOS module, decision 0007). It builds nothing: it
# downloads the CI release the tag `deployable` points at (decision 0027,
# #158) over HTTPS with no gh login (V0.release-without-gh), checks its
# SHA256SUMS, makes it the current build, keeps a copy as the seed and starts
# the launcher at login. From then on the
# running fyrd updates itself to each new release (signed, verified, smoke-
# tested, #153). Run it again to reinstall and restart; `--uninstall` stops the
# agent and removes its plist (the data folder stays).
#
#	scripts/install-macos.sh [--uninstall]
set -euo pipefail
cd "$(dirname "$0")/.."

# V0.release-without-gh: a label that names no GitHub account. The agents an
# older install loaded under com.github.meros.fyr (and its daily .update job
# before decision 0027) are stopped and their plists removed, so a Mac never
# runs two.
label=app.fyr.fyrd
old_labels=(com.github.meros.fyr com.github.meros.fyr.update)
data="$HOME/.local/share/fyr"
agents="$HOME/Library/LaunchAgents"
plist="$agents/$label.plist"
log="$HOME/Library/Logs/fyr.log"
domain="gui/$(id -u)"
repo="${FYR_REPO:-meros/fyr}"

# stop_agent stops the agent $1 and waits until it is gone: bootout returns
# before it is, and a bootstrap of the same program fails until it is.
stop_agent() {
  launchctl bootout "$domain/$1" 2>/dev/null || true
  for _ in $(seq 1 50); do launchctl print "$domain/$1" >/dev/null 2>&1 || break; sleep 0.2; done
}

if [ "${1:-}" = --uninstall ]; then
  for l in "${old_labels[@]}" "$label"; do
    stop_agent "$l"
    rm -f "$agents/$l.plist"
  done
  command -v tailscale >/dev/null && tailscale serve --https=7443 off >/dev/null 2>&1 || true
  rm -rf "$HOME/Applications/Fyr.app"
  echo "stopped and removed $label; data stays in $data"
  exit 0
fi

for t in git curl claude; do
  command -v "$t" >/dev/null || { echo "missing $t on PATH (brew install $t)" >&2; exit 1; }
done
# The install and the updates read the public releases without gh
# (V0.release-without-gh); the agents use gh for pull requests.
command -v gh >/dev/null || echo "note: no gh on PATH; the install needs none, but agents need it (logged in) for pull requests" >&2

case "$(uname -m)" in
  arm64) target=darwin-arm64 ;;
  x86_64) target=darwin-amd64 ;;
  *) echo "unsupported Mac: $(uname -m)" >&2; exit 1 ;;
esac
# The tag by git (no API call), an annotated tag's commit first; the assets
# from their public download URLs (V0.release-without-gh).
base="https://github.com/$repo"
out="$(git -c credential.helper= ls-remote --tags "$base.git" refs/tags/deployable)"
sha="$(awk '$2 == "refs/tags/deployable^{}" {print $1}' <<<"$out")"
[ -n "$sha" ] || sha="$(awk '$2 == "refs/tags/deployable" {print $1}' <<<"$out")"
[ "${#sha}" = 40 ] || { echo "no tag deployable in $base" >&2; exit 1; }
short="${sha:0:12}"
dl="$(mktemp -d)"
echo "downloading release build-$short for $target from $repo"
for f in "fyrd-$target" "fyr-$target" manifest.json SHA256SUMS SHA256SUMS.sig; do
  curl -fsSL --retry 3 -o "$dl/$f" "$base/releases/download/build-$short/$f"
done
# The running fyrd verifies the signature of every later update; this
# first install trusts HTTPS to GitHub and checks the sums.
(cd "$dl" && grep -E "  (fyrd-$target|fyr-$target|manifest.json)\$" SHA256SUMS | shasum -a 256 -c -)

build="$data/builds/$short"
mkdir -p "$build" "$data/seed" "$data/launcher" "$(dirname "$plist")" "$(dirname "$log")"
install -m 0755 "$dl/fyrd-$target" "$build/fyrd"
install -m 0755 "$dl/fyr-$target" "$build/fyr"
cp "$dl/manifest.json" "$dl/SHA256SUMS" "$dl/SHA256SUMS.sig" "$build/"
date -u +%FT%TZ >"$build/.verified"
install -m 0755 "$build/fyrd" "$data/seed/fyrd"
install -m 0755 "$build/fyr" "$data/launcher/fyr"
ln -sfn "builds/$short" "$data/current.tmp" && mv -fh "$data/current.tmp" "$data/current"
rm -rf "$dl" "$data/update.sh" "$data/src" "$data/src-built"

# The first process of the launchd job answers to macOS for everything under
# it, and a permission (App Management, Full Disk Access) is granted to that
# binary's exact hash. The launcher changes with each install, so the job
# starts this small host instead: it runs the launcher as its child, passes
# signals on and exits as the launcher did. It holds no paths and is rebuilt
# only when this source changes, so a permission once given stays given.
host_dir="$(mktemp -d)" # the file name is the binary's name in the macOS prompt
cat >"$host_dir/fyr.c" <<'EOF'
#include <errno.h>
#include <signal.h>
#include <spawn.h>
#include <stdio.h>
#include <sys/wait.h>
#include <unistd.h>
extern char **environ;
static volatile pid_t child;
static void forward(int sig) {
	if (child > 0)
		kill(child, sig);
}
int main(int argc, char **argv) {
	int sigs[] = {SIGTERM, SIGINT, SIGHUP};
	sigset_t block, none;
	struct sigaction sa = {0};
	posix_spawnattr_t attr;
	pid_t pid;
	int err, st;
	if (argc < 2)
		return 64;
	sigemptyset(&block);
	sigemptyset(&none);
	sa.sa_handler = forward;
	for (int i = 0; i < 3; i++)
		sigaddset(&block, sigs[i]);
	sigprocmask(SIG_BLOCK, &block, 0); /* a signal waits until child is set */
	for (int i = 0; i < 3; i++)
		sigaction(sigs[i], &sa, 0);
	posix_spawnattr_init(&attr);
	posix_spawnattr_setsigmask(&attr, &none);
	posix_spawnattr_setflags(&attr, POSIX_SPAWN_SETSIGMASK);
	err = posix_spawn(&pid, argv[1], 0, &attr, argv + 1, environ);
	if (err) {
		fprintf(stderr, "fyr: cannot start %s: error %d\n", argv[1], err);
		return 127;
	}
	child = pid;
	sigprocmask(SIG_UNBLOCK, &block, 0);
	while (waitpid(pid, &st, 0) < 0)
		if (errno != EINTR)
			return 1;
	return WIFEXITED(st) ? WEXITSTATUS(st) : 128 + WTERMSIG(st);
}
EOF
if [ ! -x "$data/host/fyr" ] || ! cmp -s "$host_dir/fyr.c" "$data/host/fyr.c"; then
  mkdir -p "$data/host"
  cc -O2 -arch arm64 -arch x86_64 -o "$host_dir/fyr" "$host_dir/fyr.c"
  mv -f "$host_dir/fyr" "$data/host/fyr"
  cp "$host_dir/fyr.c" "$data/host/fyr.c"
fi
rm -rf "$host_dir"

# What the agents need on their PATH: git, gh, claude, and node/go when you
# have them (the projects you work on may).
path=""
for t in git gh claude node go; do
  command -v "$t" >/dev/null || continue
  d="$(dirname "$(command -v "$t")")"
  case ":$path:" in *":$d:"*) ;; *) path="${path:+$path:}$d" ;; esac
done
path="$path:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

# ~/.config/fyr/github.env (GH_TOKEN=..., mode 0600) is loaded first, as
# the NixOS hosts' EnvironmentFile: a fine-grained token that may only read
# meros/fyr's contents (README, Updates). A public release repo needs none.
cat >"$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$label</string>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/sh</string><string>-c</string>
    <string>set -a; [ -r "\$HOME/.config/fyr/github.env" ] &amp;&amp; . "\$HOME/.config/fyr/github.env"; set +a; exec "$data/host/fyr" "$data/launcher/fyr" -data "$data" -seed "$data/seed/fyrd"</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key><string>$path</string>
    <key>HOME</key><string>$HOME</string>
  </dict>
  <key>WorkingDirectory</key><string>$HOME</string>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>ProcessType</key><string>Interactive</string>
  <key>StandardOutPath</key><string>$log</string>
  <key>StandardErrorPath</key><string>$log</string>
</dict>
</plist>
EOF

# ~/Applications/Fyr.app, for Spotlight and the Dock: the page in a Chrome
# app window (or the default browser), with the lighthouse icon.
app="$HOME/Applications/Fyr.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cat >"$app/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>Fyr</string>
  <key>CFBundleIdentifier</key><string>$label.app</string>
  <key>CFBundleExecutable</key><string>Fyr</string>
  <key>CFBundleIconFile</key><string>fyr</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>LSUIElement</key><true/>
</dict>
</plist>
EOF
# A native (arm64 and x86_64) launcher: macOS takes a script for an Intel
# app and asks for Rosetta. It runs `fyr open -app` (V0.130), so the window
# opens signed in on http://fyr.localhost:PORT, the origin fyr open uses; a
# bare http://127.0.0.1 has no session and keeps its own saved filters.
# Only when fyr open fails (fyrd is down) does it open the plain address.
launcher_c="$(mktemp -d)/fyr.c" # a file: with two -arch flags cc reads its input twice
cat >"$launcher_c" <<EOF
#include <spawn.h>
#include <sys/wait.h>
#include <unistd.h>
extern char **environ;
int main(void) {
	char *argv[] = {"fyr", "open", "-app", "-data", "$data", 0};
	pid_t pid;
	int st;
	if (posix_spawn(&pid, "$data/launcher/fyr", 0, 0, argv, environ) == 0 && waitpid(pid, &st, 0) == pid && WIFEXITED(st) && WEXITSTATUS(st) == 0)
		return 0;
	execl("/usr/bin/open", "open", "http://127.0.0.1:7777", (char *)0);
	return 1;
}
EOF
cc -O2 -arch arm64 -arch x86_64 -o "$app/Contents/MacOS/Fyr" "$launcher_c"
icons="$(mktemp -d)/fyr.iconset"
mkdir -p "$icons"
for s in 16 32 128 256 512; do
  sips -z $s $s design/logo/lighthouse-512.png --out "$icons/icon_${s}x${s}.png" >/dev/null
  sips -z $((s * 2)) $((s * 2)) design/logo/lighthouse-512.png --out "$icons/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$icons" -o "$app/Contents/Resources/fyr.icns"
codesign -s - -f "$app" >/dev/null 2>&1 || true
touch "$app"
# Spotlight lists it only once LaunchServices knows it.
/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$app"
mdimport -i "$app" >/dev/null 2>&1 || true

# The tailnet (decision 0030): installs of the same tailnet user pair by
# reading https://<this Mac>.ts.net:7443/.well-known/fyr, then link on the
# peer port. The macOS app's Serve proxies to TCP only, never to Fyr's Unix
# socket, so it goes to a port that answers the well-known and nothing else.
# No hostListen (the old whois listener) is set any more: a paired install
# follows this one over the peer link (V0.retire-discovery). Keys already in
# the config are kept, a hostListen an older run set among them.
cfg="$HOME/.config/fyr/config.json"
if command -v tailscale >/dev/null && tailscale status >/dev/null 2>&1; then
  mkdir -p "$(dirname "$cfg")"
  python3 - "$cfg" <<'PY'
import json, os, sys
p = sys.argv[1]
c = json.load(open(p)) if os.path.exists(p) else {}
c.setdefault("wellKnownListen", "127.0.0.1:7779")
c.setdefault("peerListen", "0.0.0.0:7780")  # the peer port (V0.158); 7779 is taken by wellKnownListen
json.dump(c, open(p, "w"), indent=2)
PY
  wk="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("wellKnownListen", ""))' "$cfg")"
  if [ -n "$wk" ]; then
    tailscale serve --bg --https=7443 "http://$wk" >/dev/null
    echo "tailnet: https://$(tailscale status --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["Self"]["DNSName"].rstrip("."))'):7443 answers /.well-known/fyr"
  fi
else
  echo "tailnet: Tailscale is not running here; other installs will not find this one (run this again once it is)"
fi

# The old labels go first (V0.release-without-gh): both would start the
# same launcher on the same data.
for l in "${old_labels[@]}"; do
  if [ -e "$agents/$l.plist" ] || launchctl print "$domain/$l" >/dev/null 2>&1; then
    echo "moving $l to $label"
  fi
  stop_agent "$l"
  rm -f "$agents/$l.plist"
done
stop_agent "$label"
launchctl bootstrap "$domain" "$plist"
echo "started $label; open http://127.0.0.1:7777 · log $log"
echo "release $short is current; fyrd installs each new release by itself (Settings → Auto-update)"
