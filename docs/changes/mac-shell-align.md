# V0.mac-shell-align: Fyr.app keeps only Fyr's own origin in its window, as the Linux app does, and guesses no address, log path or delay

→ decision 0037, decision 0038, V0.mac-selfupdate. *Amends decision 0037's rule for which links stay in the window.*

Why: the two native shells disagreed. On Linux (`internal/desktop` `External`) a link stays in the window only when it is on the origin Fyr signed in on; on macOS (`Window.swift` `isFyr`) any link to `127.0.0.1`, `localhost` or `*.localhost` stayed, so a link to a dev server opened inside Fyr.app on a Mac and in the browser on Linux. The Mac shell also opened a hard-coded `http://127.0.0.1:7777` when it knew no address, named the log as `~/Library/Logs/fyr.log` although the agent's log can be set elsewhere (`Agent.log`, from the plist), and restarted after an update with `sleep 1; open -n`, a guess at how long the old copy takes to quit.

(1) **Origin only.** A link stays in Fyr.app's window when its scheme, host and port are the signed-in origin's (case-insensitive, as on Linux); about:, data: and blob: pages and subframes stay as before. Every other http(s) link opens in the default browser.

(2) **No guessed address.** Open in Browser opens a one-time link, else the origin signed in on before; with neither it shows an alert that says why and where the log is, instead of opening a port Fyr may not listen on.

(3) **The log's real path.** The "Fyr does not answer" page and that alert name `Agent.log` (with `~` for the home folder), the file the agent writes.

(4) **Relaunch after exit.** After an update the new build starts once the old process has exited (the relaunch script waits on its pid); if the old one still runs after 60 s the quit failed and no second copy starts.

Not built here: the change is checked with `swiftc -parse` and the origin rule with a Foundation-only copy run on Linux; the Mac runner compiles it (mac.yml on main, mac-pr.yml once V0.ci-gates lands).
