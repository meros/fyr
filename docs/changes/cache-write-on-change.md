# V0.cache-write-on-change: the front writes a host's cache file only when its state changed, at most every 30 s

→ V0.54, decision 0016. Fixes #22 point 6 ("The front's disk cache does not rewrite a busy host's whole file every 2 s").

1. **Only on a change.** Every 2 s the front looks at the hosts whose state was marked changed. A host whose file would hold the same content as its last write (all but `savedAt`) is not written.
2. **A bound per host.** A host's file is written at most every 30 s. A busy host's changes in between wait for the next write; none is lost while the front runs.
3. **At stop.** When the front stops it writes every host that changed since its last write, whatever the bound.
4. **"Offline since".** An online host that is marked changed but whose content is the same is still written after 10 min, so `savedAt`, which a restart shows as "offline since", stays near the truth.
