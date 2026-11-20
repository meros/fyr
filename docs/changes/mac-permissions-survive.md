# V0.mac-permissions-survive: a permission given to Fyr on a Mac survives a reinstall

→ V0.126, V0.168, decision 0007. 

(1) **Why.** macOS holds the first process of the launchd job responsible for everything under it, the agents and the browsers they start included, and grants App Management and Full Disk Access to that binary's exact hash. That process was the launcher, which `scripts/install-macos.sh` replaces on every run, so each reinstall lost the grants. An auto-update does not touch the launcher.

(2) **The host.** The job now starts `host/fyr` in the data folder, a small C program that runs the launcher as its child, forwards SIGTERM, SIGINT and SIGHUP and exits with the launcher's status. It holds no paths, is built on the Mac and is rebuilt only when its source changes, so its hash stays.

(3) **The move.** The first install with the host asks for each permission once more, for the new `fyr` entry.
