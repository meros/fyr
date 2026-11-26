# V0.forward-cross-site-open: a forward link opened from another app or site loads instead of being refused

→ V0.forwarded-ports (4) (what a forward port checks), which it amends.

**Why.** On a phone, a press on a forward link in the Fyr app opens it in the browser's own tab. That navigation reaches the forward port as `Sec-Fetch-Site: cross-site`, and the port refused every such request with "this request comes from another site": the owner saw that text and never the server. A link pasted into another app, or opened from another site, met the same refusal. Letting the request through is not enough on its own: Fyr's session cookie is `SameSite=Strict`, so a cross-site navigation does not carry it, and in enforce mode the port would answer with its sign-in page instead.

(1) **The bounce.** A cross-site request that opens the top page (`GET` or `HEAD`, `Sec-Fetch-Mode: navigate`, `Sec-Fetch-Dest: document`) gets a small page from the forward port itself, not the server's. Its script opens the same address again with `location.replace`. That request starts from the forward's own origin, so it says `same-origin` and carries the cookie, and then passes the checks of V0.forwarded-ports (4) as before. Nothing of the cross-site request reaches the thread's server, and the page names nothing of it. Its policy allows only that script, by its hash.

(2) **No loop.** The script reopens the page at most once in ten seconds per tab (it keeps the time in `sessionStorage`). A browser that still says cross-site, or keeps no storage, sees the page's link "Open it" instead, and a press on it is same-origin.

(3) **Still refused.** Every other cross-site request: a frame, a script, an image or a fetch of another site's page, and a cross-site post (a form of another site that would act on the server as the owner).

(4) **Tests.** `internal/front` `TestForwardServesAThreadsServerThroughTheHostLink`: a cross-site page open gets the bounce page with its hash in the policy and does not reach the server; a cross-site frame is still refused. Not tested in a real browser: that Chrome's in-app tab on a phone then sends `same-origin` with the cookie.

(5) **Not in this change.** Fyr's own port answers a cross-site page open without a session (`/forward/open` among them) with its sign-in page in enforce mode, for the same `SameSite=Strict` reason.
