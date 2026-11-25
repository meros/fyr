# V0.attachment-doc-preview: a sent PDF or HTML file previews in the full-screen viewer

→ amends V0.165 (the viewer), V0.download-assets.

A sent file that was not an image opened as a link in a new tab, which is a bare full screen of the browser with no way back inside Fyr. Now:

1. A press on a sent PDF or HTML attachment opens the viewer, on both fronts, with Download and Close as for an image. The viewer swipes between the images, PDFs and HTML pages of one message. A modified click or a middle click keeps the link's own tab.
2. A PDF shows in the browser's own reader in a frame; an HTML page shows in a frame with `sandbox="allow-scripts"` and no `allow-same-origin`, so it never has Fyr's origin.
3. `GET /api/attachments/{id}` sends a PDF without the `sandbox` policy (the browser's PDF reader does not run under it), an HTML file with `sandbox allow-scripts allow-popups allow-forms` (it runs, with no origin), and every other type with `sandbox` as before.
4. A document frame scrolls and zooms by itself: the viewer's pan, pinch and double-tap do not apply to it.
