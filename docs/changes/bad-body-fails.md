# V0.bad-body-fails: a malformed body is refused, never half acted on

→ V0.json-helpers. *Amends it: `httpguard.DecodeLoose` is gone.*

Why: `DecodeLoose` logged a decode error and kept what had decoded before it, so `{"id":"X","on":"true"}` to `POST /host/drive` called `Drive.Set(X, false)`, and a truncated body to any state-changing handler acted on a partial value. Other handlers called `httpguard.ReadJSON` directly and answered a hard-coded 400 in another shape.

(1) Every handler reads its body through `api.DecodeBody` (400 or 413, `{"ok":false,"error":...}`). A call whose fields are all optional (`/device/add`, `/api/front/auth/add`, push status and test, handover export) uses the new `api.DecodeOptional`: an empty body is fine, any other failure is answered like `DecodeBody`'s.

(2) The missing-field answers of `focus`, the presence route and the push routes use `api.Reply`. No web client depends on the old text; only the upload chip reads a 413.

(3) `deploy`'s default Runner gives a `git` command `proc.GitEnv` (no credential prompt, C locale), now exported; `proc.GitWith` builds on it.

(4) A release download replaces an unverified build directory by renaming the old one aside first (`replaceDir`), so the build directory is never missing, and puts it back if the swap fails. A failed `MkdirAll` of `builds` fails the install.
