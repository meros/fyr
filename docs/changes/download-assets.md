# V0.download-assets: an image or a file in a thread can be downloaded

→ V0.18, V0.19, V0.165. 

(1) **Download.** An image preview's card has Download beside Full screen, the image viewer has it beside Close, and a file the owner sent has it beside its chip. It is a link: the browser saves the file the way it saves any other (Downloads on a desktop, the system's own sheet on a phone). Both fronts show the same control; on the phone it is 44 px, like the button beside it.

(2) **The host.** `GET /api/attachments/{id}?download=1` answers with `Content-Disposition: attachment` and the file's name; without the parameter the file is shown inline, as before. The link also carries the `download` attribute, for a source with no host behind it (the mock's data URLs).

(3) **Markdown images.** An image an agent wrote into a message (`.md-img`, V0.19) opens in the viewer on a press, so it zooms and has Download like the others.

(4) **Not in this change.** An HTML preview is a page, often a folder of files, and has no Download.
