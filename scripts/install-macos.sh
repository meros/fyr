#!/usr/bin/env bash
# Installs Fyr on a Mac as a launchd agent of the logged-in user (the Mac's
# stand-in for the NixOS module, decision 0007): builds this checkout, makes
# it the current build, keeps a copy as the seed, starts the launcher at
# login, and runs scripts/update.sh once a day at 04:00 (it builds origin's
# default branch and switches to it). Self-deploys and updates build with
# the toolchain on the agent's PATH (no Nix needed). Run it again to rebuild
# and restart; `--uninstall` stops both agents and removes their plists (the
# data folder stays).
#
#	scripts/install-macos.sh [--uninstall]
set -euo pipefail
cd "$(dirname "$0")/.."

label=com.github.meros.fyr
ulabel=$label.update
data="$HOME/.local/share/fyr"
plist="$HOME/Library/LaunchAgents/$label.plist"
uplist="$HOME/Library/LaunchAgents/$ulabel.plist"
log="$HOME/Library/Logs/fyr.log"
domain="gui/$(id -u)"

if [ "${1:-}" = --uninstall ]; then
  launchctl bootout "$domain/$ulabel" 2>/dev/null || true
  launchctl bootout "$domain/$label" 2>/dev/null || true
  rm -f "$plist" "$uplist"
  echo "stopped and removed $label and $ulabel; data stays in $data"
  exit 0
fi

# The web client needs a Node LTS (Makefile node-lts). fnm's lts-latest alias
# is a stable path that follows the newest LTS it installed.
if command -v fnm >/dev/null; then
  fnm install --lts >/dev/null 2>&1 || true # its corepack hook fails on Node 26 (no corepack); the install stands
  node_bin="$(fnm exec --using=lts-latest sh -c 'dirname "$(command -v node)"')"
  lts_alias="${FNM_DIR:-$HOME/.local/share/fnm}/aliases/lts-latest/bin"
  [ -d "$lts_alias" ] && node_bin="$lts_alias"
else
  node_bin="$(dirname "$(command -v node)")"
fi
export PATH="$node_bin:$PATH"

for t in go git gh claude; do
  command -v "$t" >/dev/null || { echo "missing $t on PATH (brew install $t)" >&2; exit 1; }
done

make build
mkdir -p "$data/seed" "$data/launcher" "$(dirname "$plist")" "$(dirname "$log")"
install -m 0755 bin/fyrd "$data/seed/fyrd"
install -m 0755 bin/fyr "$data/launcher/fyr"
install -m 0755 scripts/update.sh "$data/update.sh"
bin/fyr install -data "$data" bin

# What the agents and a self-deploy need: node (LTS), go, git, gh, claude, make.
path="$node_bin"
for t in go git gh claude; do
  d="$(dirname "$(command -v "$t")")"
  case ":$path:" in *":$d:"*) ;; *) path="$path:$d" ;; esac
done
path="$path:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

cat >"$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$label</string>
  <key>ProgramArguments</key>
  <array>
    <string>$data/launcher/fyr</string>
    <string>-data</string><string>$data</string>
    <string>-seed</string><string>$data/seed/fyrd</string>
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

cat >"$uplist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$ulabel</string>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/bash</string><string>$data/update.sh</string>
    <string>-data</string><string>$data</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key><string>$path</string>
    <key>HOME</key><string>$HOME</string>
  </dict>
  <key>StartCalendarInterval</key>
  <dict><key>Hour</key><integer>4</integer><key>Minute</key><integer>0</integer></dict>
  <key>ProcessType</key><string>Background</string>
  <key>StandardOutPath</key><string>$HOME/Library/Logs/fyr-update.log</string>
  <key>StandardErrorPath</key><string>$HOME/Library/Logs/fyr-update.log</string>
</dict>
</plist>
EOF

for l in "$ulabel" "$label"; do launchctl bootout "$domain/$l" 2>/dev/null || true; done
launchctl bootstrap "$domain" "$plist"
launchctl bootstrap "$domain" "$uplist"
echo "started $label; open http://127.0.0.1:7777 · log $log"
echo "daily update $ulabel at 04:00 · log ~/Library/Logs/fyr-update.log · run now: launchctl kickstart $domain/$ulabel"
