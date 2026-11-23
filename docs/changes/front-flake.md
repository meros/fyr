# V0.front-flake: two races in the front found under load

Found by running `go test -race -count=30` beside six busy loops. Two tests failed, each from a race in the code, not in the test.

1. **A stall right after a recovery was dropped** (`TestV181StalledLocalHostIsOfflineAndTheFrontStaysUp`). The probe of a stalled host marks it online again, and only afterwards did its goroutine clear the "one probe at a time" flag. A stall frame that came in that gap found the flag set and was ignored, so the host was never marked offline. The flag is now released before the answer brings the host online.
2. **An edit that follows a shared edit at once could write the old values back** (`TestProjectSharedPriority`). `front.projectShared` starts from the front's copy of the project, which is only updated by the host's event. A rename sent before the event for the priority arrived wrote the old priority with a newer stamp. The front now puts what a host stored into its copy at once (`noteShared`); the event later carries the same record.

No timeout was raised. The failure seen on the dedup PR was not caused by it: the same tests fail on main.
