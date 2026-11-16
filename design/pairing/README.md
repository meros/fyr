# Pairing and discovery in Settings

The design of every Settings surface that decision 0026 (keys, not networks) needs: the hosts, their ids and trust, the found installs, pairing by code, the tailnet shortcut, adding by address, a changed id, devices, removal and key rotation, and the empty and error states. Desktop and phone, drawn from one page each. (Owner, 16 Nov: "Make sure the settings pane is fully built and thought through for the pairing and discovery workflows".)

| Path | What |
|---|---|
| `i1.html` | Iteration 1: structure. Where it lives, what a host row and a host's page hold, today's frame beside the all-slices frame. Superseded; kept for the history. |
| `i2.html` | Iteration 2: the flows, on the structure the owner chose. |
| `i3.html` | **Iteration 3: the final set.** Part A what to build now, part B every later screen by slice, part C the states, D what is not drawn today, E which slice fills what. Build from this one. |
| `SPEC.md` | The implementation spec: part 1 to build now, part 2 what each later slice adds, part 3 the placeholders. |
| `pairing.css` | Part 1: the proposed new CSS; copy from here. Part 2: board only. |
| `kit.js`, `i1.js`, `i2.js`, `i3.js` | The pages. `kit.js` has the nav, the rows and the frames. |
| `impl/` | Screenshots of the implementation, when it exists. |

The pages load `../settings/mock.css`, `../settings/mock.js` and the fonts there (the tokens and the V0.119 settings rows), so open them from the repository. Light and Dark are at the top right; `?theme=dark` starts dark.

## What the owner decided

After iteration 1 (16 Nov):

- **Hosts is its own Settings page**, the fifth, between Projects and This device. Projects loses its Hosts group.
- **A host row opens the host's page.** The row is one line (state, route, short id). The full id, a changed id and every action are on the page. V0.124's Trust button moves there.
- **Devices joins This device**, which is renamed Devices when device pairing arrives (slice 5).
- **The front sends each host's address now** (`hosts[].url`), so today's page can show the address and the route.

After iteration 2 (16 Nov):

- The side that types a pairing code **gives only the code**. Fyr asks the installs it has found for the one that shows it; an address field appears only when none does.
- The tailnet shortcut has **a switch, on by default**. Remove on a host's page is the undo for one install.
- The pairing code is **six digits and a QR** (the recommendation was digits only). The typing side gets Scan… where the browser has a camera.
- A new id under a known name that is the owner's own untagged tailnet node is trusted with **one press after a confirm**; never by itself. Elsewhere it pairs again by code.

## What the design holds to

- An install is its id. A name, an address and a network are labels; the page never lets one stand in for the id where trust is decided: both ids are shown in full before Trust, Block, Remove or a pairing is done.
- Being found gives nothing. A found install has no name of its own, cannot be followed, and one that sits at a known host's address or name is not offered Pair.
- Nothing is drawn that does not work. In the first build a missing action is absent, and a sentence says how the thing is done today.
- Calm for what is vouched for (a key the old key signed, a pairing the tailnet made), a warning only for what nothing vouches for (a new id under a known name).
- One mark and words for a state, never colour alone. Red is for a row that needs the owner, not for an offline host.
