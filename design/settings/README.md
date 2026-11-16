# Settings redesign

The design work for the Settings screens and the project screen (owner, 16 Nov: "redesigning/polishing all the settings screens. They are becoming a bit messy").

| Path | What |
|---|---|
| `i1.html` | Iteration 1: structure (navigation, grouping, order). Superseded; kept for the history. |
| `i2.html` | Iteration 2: controls and copy, on the structure the owner chose. |
| `i3.html` | **Iteration 3: the final set.** Part A every screen, part B the reorder control, part C the states. Build from this one. |
| `SPEC.md` | The implementation spec for the two implementation threads. |
| `mock.css` | Part 1: tokens and components copied from `web/src/app.css`. Part 2: the board. Part 3: the proposed new CSS (`.stx`, `.stn`, `.sg`, `.sr`, `.swt`, `.grip` and the rest); copy from here. |
| `mock.js`, `i1.js`, `i2.js`, `i3.js` | The pages. A page is written once and drawn in the desktop dialog and the phone screen. |
| `before/` | Screenshots of today's screens (`?mock=rail`, V0.116) at desktop and phone width. |
| `impl/` | V0.119 (part 1): the screens before and after the implementation in mock mode, at desktop and phone width; `after/` has every page and sub-page light, each desktop page dark, and the states (no key, a failed read, no connection, no providers, the phone's Reorder mode and More sheet). |

Open an `iN.html` file in a browser. The Light and Dark buttons are at the top right; `?theme=dark` starts dark.

## Inventory: every settings surface today

| Surface | Desktop | Phone | Code |
|---|---|---|---|
| Settings | A dialog (620 px, at most 76 vh) from the rail's gear | A screen from More → Settings | `Settings` in `settings-ui.tsx`, mounted in `app.tsx` and `phone.tsx` |
| Providers (V0.44, V0.50, V0.53) | A list; a row unfolds its editor in place; the login panel; Found on this host; Add provider unfolds a form | the same body | `ProvidersSection`, `ProviderRow`, `LoginPanel`, `AddProvider` |
| Projects (V0.58) | A list by name and New project | the same | `ProjectsSection` |
| Auto-merge (V0.102) | A row per repo with an On/Off button, a field to add a repo | the same | `AutoMergeSection` |
| Auto-deploy (V0.111) | One On/Off button | the same | `AutoDeploySection` |
| System agents (V0.115) | Host picker (several hosts), Provider and Model, one picker per job | the same | `SystemAgentsSection` |
| Hosts (V0.51) | Two lines of text per host | the same | inline in `Settings` |
| Notifications (V0.15) | The rail's bell icon, repeated in Settings; opens a dialog | A button that reads "Notifications are off"; opens a sheet. Also a row in the More sheet | `PwaControls`, `PushDialog`, the `push` sheet |
| Appearance | Three buttons. Also in the `?` key bar | Three buttons. Also three rows in the More sheet | `themeControl` in `app.tsx`, `MSheets` |
| Show tool details | Only in the `?` key bar | Only in the More sheet | `prefs.ts` |
| Project screen (V0.23, V0.46, V0.55, V0.58, V0.74, V0.116) | A second dialog, opened from Settings → Projects or the picker's "Edit …" | A screen | `ProjectEditor`, `QuietRow`, `FolderField` in `project-ui.tsx` |

## What is messy

1. **One scroll, no way around it.** Eight sections of very different weight in one column. The desktop dialog shows a section and a half; the phone page is about 3 900 px. Auto-merge, Auto-deploy and System agents sit under a project list that grows without limit.
2. **On/Off is a button.** "Off" reads as an action. A pressed button, a dark `.btn.p` radio (theme), two different `.seg` looks (`app.css` defines `.seg` twice, lines 714 and 1037) and a `role=tablist` all mean "pick one".
3. **Saving is mixed.** Settings pickers save at once. A provider's models and key each have a Save button. The project screen has one Save at the bottom, but its folder, its image and the setup run act at once. The result line of a project shows at the bottom, far from the control.
4. **Help text has no place.** Above the section (Auto-merge, Auto-deploy, System agents), under the field (Image), in a lede (Quiet hours), missing (Hosts).
5. **A provider row holds too much.** Name, kind, where, models, quota, a need line, the login panel and the whole editor unfold inside a list item. On the phone the summary wraps to three lines. "Found on this host" says the key twice. The editor ends with internal words ("Quota: anthropic-oauth · imported from profile claudeAgent").
6. **Models are a text box.** "Models (one id per line)" with its own Save, beside a Default model select that saves at once.
7. **The project screen has nine fields and no groups.** Quiet hours (behaviour) sits between Name and Colour (identity). Set up automatically is inside Image, but it also sets the name and the colour. On the phone a quiet-hours row breaks into three lines with × far from its row.
8. **Hosts, Notifications and Appearance are leftovers.** Bare text, a status sentence used as a button label, and a third kind of radio.
9. **Doubles on the phone.** Notifications and Theme are both in the More sheet and in Settings.
10. **Errors and loading.** One red line at the top of a section, wherever the failing control is. "Loading…" as text, per section.
11. **Phone sizes are patched in per control** (`.set.phone .sbtn…`, `.m-nbody .pimg .btn`), so each new control needs a new patch.

## What the owner decided

- After iteration 1 (16 Nov): **four pages** (Providers with System agents, Automation, Projects with Hosts, This device); **every change saves at once**; the More sheet **loses Notifications and Theme**.
- During iteration 3, through the coordinator: a **model chooser** in provider settings (discovered models, on/off, search, reorder, a star for the default) and **one project order** used everywhere, with **one reorder control** (drag on desktop, an up/down mode on the phone).
