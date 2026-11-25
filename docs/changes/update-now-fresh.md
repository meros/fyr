# V0.update-now-fresh: Update now installs the newest green main without waiting

→ V0.126, V0.host-updates (#153, #154). *Amends V0.host-updates: the Updates row of a host's page also shows while the host is up to date by its last read.*

(1) **The press does not wait.** Update now reads the tag `deployable` at once (git ls-remote, no API call) and installs that release, without the quiet period a release seen by the watcher waits out. It was so before; what changes is where it is offered. The press also wakes the watcher, so the host's update state reads the tag again too.

(2) **When a release waits.** The watcher keeps when the waiting release's quiet period ends, and the host's update state sends it as `due`. The row reads "With Auto-update on, Fyr installs it by itself at 14:32 … Update now does not wait." A due time that has passed (Auto-update is off, or an update runs) is not said.

(3) **When the host thinks it is up to date.** A host reads `deployable` once an hour, so a release CI published since may not be known there. The row "Up to date as of the last read" offers Update now, which reads the tag again and installs only a newer release ("Fyr already runs …" otherwise).

(4) **Both fronts.** The row is one component (`settings-host-groups.tsx` `HostUpdates`) on desktop and phone.
