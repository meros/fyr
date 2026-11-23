# V0.peerlink-drain-flush: a link that lost a race closes only after the responses it served reached the peer

→ V0.peerlink-replace-drain.

(1) **The cause.** A replaced link to a peer that does not announce its end waits until it serves no request of the peer's, then closes. A request stops counting when its handler returns, but the HTTP/2 server ends the response stream only after that. A drain that looked in that gap closed the connection under a response it had not finished sending, and the peer read "client connection force closed". `TestAReplacedLinkToAnOlderPeer` failed this way in CI under load; it did not fail in 100 local runs under load, because the gap is short.

(2) **The fix.** After the served count reaches zero, the drain shuts the link's servers down gracefully (`http.Server.Shutdown`, bounded by the drain's 5 s limit): it sends GOAWAY and returns once every stream has ended, and only then is the link closed. On the side that accepted the link, the tunnel is a stream that lasts as long as the link, so its handler ends first.

(3) **The test.** `TestAReplacedLinkClosesAfterTheResponseIsSent` holds the gap open with a test hook (`afterServed`, which runs after a request stops counting and before its handler returns). It failed every time before the fix, with the error seen in CI. The peerlink tests pass 20 times under four busy loops.
