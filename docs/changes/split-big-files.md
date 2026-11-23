# V0.split-big-files: three large files split by area, no behaviour change

`core/feed.go` keeps the feed and its queue; the notice rules and the `feed.take` reduce move to `core/notices.go`. `cgroup/leftovers.go` keeps the tracker; the scan of processes goes to `leftovers_scan.go` and the end of a leftover to `leftovers_end.go`. `front/front.go` keeps the front, its links and its routes; following a host (follow, session, load, apply) goes to `front/stream.go`. Same packages, nothing newly exported, no line changed but the imports.
