# V0.mac-shell-finish: Fyr.app Uninstall, Dock badge, notification clearing, device name, Trash cleanup and version-safe self-update

→ decision 0037 (#434); completes the remaining shell items from V0.mac-shell, V0.mac-notify and V0.mac-selfupdate.

1. **Uninstall…** in the menu bar unregisters both the `SMAppService` agent and Login Items, turns off Fyr's Tailscale Serve (`--https=7443 off`) if installed, removes any leftover script-install plists, optionally deletes Fyr's data and log (unchecked by default), moves `Fyr.app` to the Trash and quits. `Fyr --agent uninstall` runs the non-interactive uninstall (keeping data and the bundle, for scripts).
2. **Dragged to the Trash.** When `fyr-host` starts from a bundle path containing `/.Trash/`, it runs `Fyr --agent unregister` and exits 0 instead of starting `fyrd` from the Trash under `KeepAlive`.
3. **Dock badge.** `Window` observes `WKWebView.title` (`"(N) Fyr"`, set by `web/src/app.tsx` for *Your move*) and mirrors `N` onto `NSApp.dockTile.badgeLabel` (clearing it at 0).
4. **Notification clearing.** `Notifier` records each notification's `threadId` and `state` and handles `op: "clear"` on the `fyrPush` bridge (`web/src/pwa.ts` `clearThreadNotification`), removing delivered notifications whose thread left the state they were shown for (matching `notificationIsStale`).
5. **Device name.** `Window` sets `applicationNameForUserAgent = "FyrDesktop/mac"`, so Settings → Devices lists the window as "The Fyr app on macOS" (`BrowserName`).
6. **Version-safe self-update.** `Updater` scans `/releases?per_page=30` for the newest non-draft release carrying `Fyr.app.zip` (`app-<sha12>` or `vX.Y.Z`) instead of reading only `/releases/latest`, so publishing a versioned `vX.Y.Z` release never stalls shell updates.
7. **`FyrLog`** is recorded in `Info.plist` by `macos/build.sh` so `Show Log` and `Uninstall…` honor custom `FYR_LOG` paths too.
