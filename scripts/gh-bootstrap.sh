#!/usr/bin/env bash
# Creates or updates the labels and milestones of docs/PROCESS.md.
# Idempotent. Sends one mutation per second (GitHub content limits).
set -euo pipefail
repo="${1:-meros/fyr}"

label() { # name colour description
  gh label create "$1" --repo "$repo" --color "$2" --description "$3" --force >/dev/null
  echo "label $1"; sleep 1
}
milestone() { # title description
  if gh api "repos/$repo/milestones?state=all&per_page=100" -q '.[].title' | grep -qxF "$1"; then
    echo "milestone $1 (exists)"; return
  fi
  gh api "repos/$repo/milestones" -f title="$1" -f description="$2" >/dev/null
  echo "milestone $1"; sleep 1
}

label "type:work"     "1f6feb" "A slice of the plan with acceptance criteria"
label "type:bug"      "d73a4a" "The code breaks the spec"
label "type:spec"     "8250df" "The spec must change"
label "type:decision" "e8960f" "A choice with options and a recorded reason"
label "type:research" "6e7781" "A question to answer with sources"
for a in core agent store server web t3 github nix design android; do
  label "area:$a" "c5def5" "Area: $a"
done

milestone "v0 first viable" "SPEC 0.5. One host, one desktop client. Exit: a working day in Fyr with T3 closed."
milestone "M0 Skeleton"     "FYR-V1 7. tsnet, pairing, ConnectRPC, PWA shell, flake."
milestone "M1 Conversation" "FYR-V1 7. Exit: A7; a 3-hour session survives two host restarts."
milestone "M2 Inbox"        "FYR-V1 7. Exit: A1, A3, A4, A6."
milestone "M3 GitHub"       "FYR-V1 7. Exit: A5, A9."
milestone "M4 Calm machine" "FYR-V1 7. Exit: A2, A8, A10."
milestone "M5 Cut-over"     "FYR-V1 7. Exit: A1-A10 for one week."
