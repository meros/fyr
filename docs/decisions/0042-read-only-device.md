# 0042 A read-only device: it reads everything, changes only its own view, and writes for 5 minutes after a passkey unlock

Date: 2026-11-24 · Status: **proposed**: the owner answered three questions on 24 Nov (see "The owner's answers"); nothing is built · Builds on 0028 (sessions, devices, step-up), 0030 (one principal per hop, `Fyr-Actor`, the signed roster, the drive switch) · Amends 0028 (5) (the read-only device, dropped there, comes back as a switch per device; passkeys move from "later" to slice 2) · Spec: 5.4 (`verified` becomes built in part) and 11.2, with a change file per slice

## Context

A phone is the device most likely to be lost, lent, or picked up unlocked. Today every signed-in device is the whole owner: a session cookie is enough to send a message, answer a Decision, merge a PR or press a Run card. 0028 added Revoke, the node block and a step-up for trust changes, which all help after the loss is noticed. Nothing limits what the phone can do in the minutes or hours before.

What is built, from the code at `1725b4a2`:

| Piece | Where | What it gives this record |
|---|---|---|
| A session per browser, a device record with its hash | `internal/auth/auth.go` | the place for a per-device switch |
| A tailnet node record, approved or blocked | `internal/auth/state.go` (`NodeRec`) | the switch must also live here: an approved node gets a new session in silence |
| The principal on every hop | `internal/authn`, `internal/front/actor.go`, `withActor` in `internal/server/server.go` | the hop that must carry "locked" |
| Device statements in the signed roster | `internal/roster` (`DeviceAdd`, `DeviceRevoke`) | how a host learns that a device is read-only without trusting one request |
| Reads for every member, commands only for one that may drive | `LinkHandler` in `internal/server/server.go` | the precedent, but by HTTP method, which is too coarse here (below) |
| Step-up by session age (5 min) | `Store.StepUp` | stays, for trust changes on full devices |
| WebAuthn | nowhere | new: SPEC 5.4 and 11.2 describe it, no code exists |

0028 (5) weighed a read-only device and dropped it: no device needed it, and it would cost a second client mode. What changed: the phone is now where most presses happen, and a passkey gives the read-only device a way back to full use without another device at hand. That was the missing half.

## Decision

### 1. A switch per device, kept on the server

- `Device.ReadOnly`, set in Settings → Devices on both fronts and with `fyr device readonly <id> on|off` on the owner socket. For a tailnet device the node record carries it too, so a new session of the same node (cleared cookies, another browser) starts read-only.
- The switch is a roster statement about the device, as its sign-in and its revoke are, so every host knows it.
- A device added from a link that a read-only device made would be a way around the switch, so a read-only device adds no device (rule 5).
- Any device kind can be read-only: a phone, a tablet, a desktop browser. The client has one lock state and one unlock sheet, shared by both fronts.

### 2. Three classes, an allow-list, and a test that keeps it whole

Every route of the front, every route of the host and every Input kind an owner can send has one class:

| Class | Meaning | A locked device | Examples |
|---|---|---|---|
| `read` | changes nothing | allowed | every `GET` of state, streams, events, changes, previews, attachments; `POST …/push/status` |
| `view` | changes only this owner's view or this device's own record | allowed | `thread.markSeen`, `thread.markUnseen`, `thread.snooze`, `thread.snoozeWhenDone`, `thread.unsnooze`, `thread.changesSeen`, `thread.fileViewed`, `thread.pin`, `thread.unpin`; `POST /api/threads/{id}/focus`, `/api/board/visit`, `/api/front/presence`; this device's push subscription and prefs |
| `write` | anything an agent, a repo, a provider, a project or another device can notice | refused with `403 {locked: true}` | `thread.create`, `thread.send`, `thread.answer`, `thread.interrupt`, `thread.settle*`, `thread.archive`, `thread.handover`, `pr.merge`, `action.run`, every project, provider and host-setup kind; `POST /api/attachments`, `/api/fs/mkdir`, `/api/search` (it runs a model), the handover routes; all of `/api/front/auth/*` and `/api/front/peer/*` but their state reads |

- **A kind or route without a class is `write`.** The HTTP method decides nothing: a POST can be a read and a command can be view state.
- The class is a field on the Input kind (`core.Kinds`) and on the route's registration, so it sits next to the code it describes. A test walks `Front.RoutePatterns()`, the host's routes and `core.Kinds` and fails for one without a class; a second test sends each `write` as a locked device through a real front and host and expects 403 and no record changed.
- A snooze is the View's alone, `thread.snoozeWhenDone` included (its Rule is stored with the View, V0.185): `view`. `thread.settleWhenDone` and `thread.settleWhenMerged` arm a Rule that later settles the thread itself: `write`.
- A draft is kept in the browser and never sent, so typing stays possible. Send is what asks for the unlock.

### 3. Enforced twice: the front that holds the session, and the host that acts

1. **The front** refuses a `write` from a read-only device that is not unlocked, before it forwards anything.
2. **The hop** says so: `Fyr-Actor` gains `access=read` (locked) or `access=unlocked` (a read-only device inside its 5 minutes). A full device sends neither. `authn.Parse` refuses a field it does not know, so a host older than this change answers 401 to a read-only device: it fails closed. Hosts are updated before a device is switched.
3. **The host** refuses a `write` when the principal says `access=read`, and also when the roster says the device is read-only and the principal does not say `access=unlocked`. A front that forgot the field is refused, not believed.

The host checks by Input kind in `Engine.CommandAs` and by route class for the other routes, with the same table the front uses (`internal/core`, imported by both).

**What the host does not verify itself: the passkey.** The front that holds the session checks the assertion. 0030 3.7 already says a member install that drives a host is the owner there and can name any live device, so a host that verified passkeys would not be safe from a compromised front anyway. The passkey's public key still goes into the device's roster statement, so a later record can move the check to the host without enrolling again.

### 4. The unlock: a passkey with user verification, 5 minutes, everything

- **Enrolment.** Switching a device to read-only shows it "Set up unlock" at its next visit: `navigator.credentials.create` with a platform authenticator, `userVerification: "required"`, the RP ID the host name of the front's origin. The front stores the credential id, the public key and the sign counter on the device record (the node record for a tailnet device). Enrolment needs a second confirmation that is not the read-only device itself: the press that switched it on, from a full device, opens a 10-minute window for that one device. Without a passkey the device is read-only with no unlock; a full device or the owner socket is the way back.
- **Challenge.** `POST /api/front/auth/unlock/challenge` returns 32 random bytes, single use, 60 s, kept in memory by session hash.
- **Assertion.** `POST /api/front/auth/unlock` with the assertion. The front checks: the type is `webauthn.get`; the challenge is the one it gave this session; the origin equals the origin it serves this request on; the RP ID hash; the user-present and user-verified flags; the signature with the stored key; a sign counter that went back is refused and written to the audit trail (a counter that stays 0 is a synced passkey, and is accepted).
- **The grant.** `unlockedUntil = now + 5 min` on that session, in memory only. It does not slide. It ends early at Lock, at Revoke, when the switch changes, and at a restart of the front. Every `write` in the window is audited with the device, as every unlock and every refused unlock is.
- **Everything, as the owner chose:** inside the window the device sends, answers, merges and presses Run cards as a full device does. The client's existing confirm steps (5.4 `confirm`, the digest of 5.13) stay.
- **Rate limit.** 5 failed assertions in 10 minutes lock the device's unlock for 10 minutes and push a notice to the other devices.

### 5. A read-only device never changes who is trusted

Locked or unlocked, a read-only device is refused: adding or approving a device, revoking another device, changing a way in's mode, pairing or removing an install, the drive switch, its own read-only switch, and enrolling a new passkey outside the window of rule 4. These go through a full device or the owner socket. It may still revoke itself.

Without this rule the 5 minutes would be enough to make the access permanent: unlock once, add a device, and the switch is gone.

### 6. What the owner sees

- **Devices:** each row has "Read-only", with "unlock set up" or "no unlock yet".
- **On the read-only device:** a lock mark in the header on both fronts. The composer, the Decision cards, Run cards and action buttons stay where they are; the first press that writes opens the system's biometric sheet and then does what was pressed. While unlocked the mark shows the minutes left, and a tap locks again.
- **A refusal is never silent:** a `403 {locked: true}` that the client did not expect (the window ended between the press and the request) opens the unlock sheet and keeps what was typed.
- **Notifications** on a read-only device open a thread, as today. No notification action writes.

## Threats

| Threat | What stops it |
|---|---|
| A phone picked up unlocked, with Fyr open | it reads; a write needs the passkey's user verification |
| A stolen session cookie used from another machine | the same: the cookie is a read-only session, and the passkey does not leave the phone's authenticator (a synced passkey is on the owner's other devices of that account, which have no session) |
| A page or a script calling the API with the session | 0030's rules, and now 403 on every `write` |
| The client hiding the lock, or a modified client | nothing depends on the client: front and host refuse |
| A front that forgets to say the device is locked | the host knows the device is read-only from the roster and wants `access=unlocked` |
| A new route or command that nobody classified | it is `write`; the test fails |
| Clearing cookies to come back as a new tailnet session | the node record carries the switch |
| Using the 5 minutes to make the access permanent | rule 5 |
| A replayed or relayed assertion | single-use challenge bound to the session, 60 s; the origin and the RP ID are checked |
| Guessing | no secret is typed in Fyr; failed assertions are rate limited and pushed |
| Lockout | a full device or `owner.sock` switches read-only off; `owner.sock` is never gated (0030) |

**What it does not do, stated plainly.**

- **User verification is not always a biometric.** The platform accepts the device's PIN or pattern where biometrics fail or are off, and WebAuthn cannot ask for a fingerprint or a face only. Someone who knows the phone's PIN passes.
- **Reading is not limited.** Conversations, diffs, attachments, previews and settings are all visible on a read-only device. This record limits what a device can do, not what it shows. Revoke is still the answer to a lost phone.
- **A compromised install is the owner** (0030 3.7), and so is a process with the owner's UID on a machine that runs Fyr (0028 (6)).
- **The 5 minutes are whole.** A phone taken while unlocked writes until the window ends. The owner chose one unlock for everything over a proof per effect; SPEC 11.2's proof bound to a payload stays the way to tighten this later.

## Options weighed

| Option | Verdict | Why |
|---|---|---|
| A lock in the client only (a PIN or biometric gate on the PWA) | rejected | the session still writes; a second browser tab or `curl` with the cookie is not asked |
| Read-only by HTTP method, as the drive switch does | rejected | read marks and presence are POSTs, a model search is a POST that costs, and one `POST /api/command` carries both `thread.markSeen` and `pr.merge` |
| The host verifies each assertion | not now | no gain while a front can name any device (0030 3.7); the key is in the roster for the day that changes |
| A proof per effect (merge, Run, deploy) on top of the window | not now (owner) | more prompts; kept as SPEC 11.2 |
| A sliding window | rejected | an unlocked phone in use by someone else would never lock |
| Strictly nothing while locked, read marks included | rejected (owner) | the phone would never clear unread |
| A separate short-lived write token in the browser | rejected | script on the page could read it; server-side state on the HttpOnly session cannot be taken from the page |

## Consequences

- **A passkey belongs to an address.** The RP ID is the front's host name. A node that is renamed, or a phone that reaches Fyr by another name (the tailnet name and a LAN name), enrols once per name. Until then that name is read-only without unlock.
- **A new dependency or a small verifier.** An assertion check is a few hashes and one signature. Enrolment parses CBOR and a COSE key. Slice 2 chooses between a WebAuthn library and a CBOR decoder with a hand-written check of ES256 and EdDSA; the test vectors come from the spec either way.
- **To verify on the real phone before slice 2 is called done:** that the installed PWA can create and use a platform passkey on the owner's phone and browser.
- **Update order.** Hosts first (they learn `access`), then fronts, then the switch appears in Devices. A read-only device against an old host gets 401 for everything, reads too, which is visible and safe.
- **Desktop and phone stay in sync.** The lock mark, the unlock sheet and the Devices row are shared components; the only difference allowed is the size of the touch target.

## Slices

| Slice | What | Shippable because |
|---|---|---|
| **1. Classes and the switch** | the class on every kind and route, the two tests of rule 2; `Device.ReadOnly`, the node record, the roster statement, the CLI; `access` on the hop; the front's and the host's refusal; rule 5; the lock mark and Devices row on both fronts | a read-only device is already useful: it reads, and a full device switches it back |
| **2. The passkey unlock** | enrolment, challenge, assertion, the 5-minute grant, Lock, the rate limit and its push, the audit lines, the unlock sheet on both fronts | adds the way back from the phone itself |
| 3. Later, not planned | a proof per effect (SPEC 11.2); the host as verifier | named only |

Each slice: red tests first, a change file in `docs/changes/`, and `internal/authtest` extended with a read-only device through a front and a second host over a link.

## The owner's answers

| Asked | Recommended | Owner |
|---|---|---|
| Which devices are read-only | a switch per device | **a switch per device** |
| How long one unlock allows writes | 5 minutes, with a proof per effect | **5 minutes for everything** |
| What a locked device may still change | only its own view | **only its own view** |

## Open, for the review of this record

1. Rule 5 is stricter than "5 minutes for everything": an unlocked read-only device still cannot add a device or change a way in. The record proposes it because the window would otherwise undo the switch.
2. A snooze removes a pin (V0.104), and pin and snooze are `view`. If a pin should count as something other devices notice, both pin kinds and the snoozes move to `write`.
