# 0005 Phone client: PWA first, no APK

Date: 2026-11-14 · Status: accepted · Research: `../../research/android-distribution.md`

**Decision.** The phone client is the PWA served by `fyrd` (spec 13, M0). No APK in v1. If a real-phone check shows the PWA cannot install as a WebAPK from a tailnet-only origin (share target, notification actions and the capture loop A3 need it), build a small native shell: one long-lived release key outside the repo, built with `androidenv` in a flake, installed with `adb install -r` over wireless debugging during development and updated by Obtainium from a page `fyrd` serves on the tailnet.

**Consequences.** Before Google's global developer-verification enforcement in 2027, register the package name and key in a free limited-distribution account, or accept the per-phone "advanced flow". First check, on a real phone: WebAPK install from the tailnet origin, share target, Web Push, notification buttons, passkey.
