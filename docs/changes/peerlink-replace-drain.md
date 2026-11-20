# V0.peerlink-replace-drain: a link that loses a race closes only when both sides are done with it, so a request the peer still sends on it is answered and does not end in "unexpected EOF"

→ decision 0030 Amendment 1, S1 ("the loser ... is closed when its requests have drained or after 5 s"); V0.174 (2). Found by the flake hunt of V0.fix-flakes (9c).

(1) **The cause.** `TestSimultaneousDialsLeaveOneLink` failed about once in 1000 runs with "unexpected EOF". When both installs dial, a late link from the smaller id can replace one both sides already use. Each side moves to the new link when it registers it, and the two sides do that at different moments. The side that moved first closed the old link as soon as its own requests on it ended, without counting the requests it was answering for the peer, or those the peer starts before it moves too. `TestAReplacedLinkDrainsBothWays` sets that order by hand (the bigger id's link in use on both sides, a request from the bigger id held open in the handler, then the smaller id's link registered on one side only): before this change the old link closed at once and both the held request and a new one from the side that had not moved ended in `unexpected EOF`.

(2) **Both sides announce.** A side whose link lost sends nothing new on it and waits for its own requests on it to end; then it sends `POST /link/drained` on that link ("I am done with it") and waits for the peer's own announcement before it closes it. Since a side announces only after it has moved and its requests ended, a link closed this way has no request open from either side. A request that was handed the old link just before it was replaced is refused before anything is sent and goes on the new link; nothing that reached the peer is sent twice, so no request needs to be idempotent. Retrying a request that already failed on the old link was not chosen: link requests include streams and commands that are not safe to send twice.

(3) **An older peer.** The hello line gets an optional `drain: true`; a peer whose hello lacks it (an install from before this change) is not sent the announcement, and the old link waits for the requests this side answers to end as well as its own. ALPN stays `fyr-peer/2` and the hello `v: 2`: an older install ignores the field.

(4) **The limit.** The old link still closes after 5 s at most, as before, and at once on a removal (V0.174 (3)).

(5) **The tunnel was counted.** On the side that opened a link, the tunnel stream counted as an open request, so a link it opened never looked idle and its drain always ran to the 5 s limit. It is not counted now.

Tests: `internal/peerlink` `TestAReplacedLinkDrainsBothWays`, `TestAReplacedLinkToAnOlderPeer`; `TestSimultaneousDialsLeaveOneLink` passes 2000 runs in a row (`-count=2000`).
