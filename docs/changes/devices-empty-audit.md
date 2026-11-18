# V0.devices-empty-audit: Settings → Devices opens on an install whose audit trail is empty

→ V0.157. (owner, 18 Nov: the page showed "This page could not be shown. e is not iterable".)

(1) **The host.** `/auth/state` sends `audit: []` when nothing has been recorded yet. It sent `null`.

(2) **The page.** It reads `audit: null` as empty, so it also opens against a host that has not been updated yet. The page is the same on the desktop and the phone.
