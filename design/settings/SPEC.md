# Settings redesign: implementation spec

The mockups are `design/settings/i3.html` (final; sections A1–A9, B1–B3, C1–C13) and `i2.html` section 0 (the control set). The proposed CSS is part 3 of `design/settings/mock.css`. Where this spec and a mockup differ, this spec wins; where neither says, keep today's behaviour.

The work is two pull requests, each from its own thread:

- **Part 1, "Settings redesign: implement"**: the shell, the four pages, the controls, save at once, the shared reorder control. Web only, over today's API. No Go change.
- **Part 2, "Model discovery and project order: implement"**: model discovery and the on/off chooser, and the one project order. Go and web. Its web work starts after part 1 has merged, because both edit `settings-ui.tsx`.

Rules for both (CLAUDE.md): desktop and phone change in the same PR; a difference between them needs a reason written next to the code. Every difference this spec allows is listed with its reason.

---

## Part 1: the redesign

### 1.1 The shell

One nav, four pages, and three sub-pages.

| Page id | Title | Holds | Nav summary (second line) |
|---|---|---|---|
| `providers` | Providers | the provider list, Found on this host, Add provider, System agents | "3 providers", or "3 · 1 needs a login" / "3 · 2 need you" with the amber count badge when a provider is not ready or a system job cannot run |
| `automation` | Automation | Auto-deploy, Auto-merge | "Deploys on · merges in 2 repos" (or "merges off") |
| `projects` | Projects | the project list, New project, Hosts | "10 projects · 1 host" |
| `device` | This device | Notifications, Theme, Show tool details, Install | "Notifications off · theme auto" |

Sub-pages: `providers/<id>` (a provider), `providers/new` (Add provider), `projects/<id>` (a project).

- **Component.** `SettingsNav` (rows: title, summary, badge, chevron) is one component. Desktop: it is the left column (212 px) of the Settings dialog, and the pane on its right shows the page. Phone: it is the body of the Settings screen, and a page is the next screen. *Reason for the difference: a phone has no room for two columns.* The chevron shows on the phone only (there the row leaves the screen).
- **Desktop dialog.** `.dlg` at `min(820px, 100%)` wide and a fixed `min(76vh, 640px)` high, so it does not change size between pages. The nav does not scroll; the pane scrolls. It opens on Providers. A sub-page shows "‹ Providers / Claude" at the top of the pane: the crumb is a button back to the list.
- **Desktop project.** The separate Project dialog goes away. `dialog === "project"` (the picker's "Edit …", the project chip) opens the Settings dialog at `projects/<id>`. × closes Settings; the crumb goes to the Projects page.
- **Phone screens.** `Screen` in `nav.ts` becomes `{ name: "settings"; page?: "providers" | "automation" | "projects" | "device"; sub?: string }`. URLs: `?settings` (the nav), `?settings=providers`, `?settings=providers/<id>`, `?settings=providers/new`. The project screen keeps `{ name: "project", id }` and `?project=<id>`; its header title is the project's name (today "Project"). The header's Back goes one level up through the history, as every phone screen does.
- **Desktop URL for tests and screenshots.** `?mock=…&dialog=settings&page=automation` and `&page=providers/<id>`, `&page=projects/<id>`.
- **Keyboard (desktop).** ↑ and ↓ in the nav move between pages and the page follows the focus. Esc: in a changed text field it puts the text back; else on a sub-page it goes to the list and focuses the row it came from; else it closes Settings. Opening a page moves focus to its heading (`tabindex="-1"`).
- **The rail's bell** (desktop) opens Settings at This device. `PushDialog` and the phone's `push` sheet are removed; their texts move to the Notifications row (1.6).
- **The phone's More sheet** loses "Notifications" and the three "Theme: …" rows (owner's choice). "Settings" gets the sub line "Providers, automation, projects, this device" and, at the right, "1 needs a login" / "2 need you" when the Providers badge shows. "Show tool details" stays.

### 1.2 The controls (new file `web/src/settings-rows.tsx`)

| Component | What | Replaces |
|---|---|---|
| `Group({ title?, end?, foot?, danger? })` | a label, a card (`.sg-c`) of rows, a foot line | `section.sets` + `h3` + loose `.setd` |
| `Row({ title, desc?, error?, end?, stack?, onClick? })` | title, help text under it, the control at the end. `stack`: on the phone the control goes under the label (for two selects, a wide field) | `.setrow`, `.amrow`, `.sarow`, `.fld` rows |
| `Switch({ on, label, disabled?, onChange })` | `button role="switch" aria-checked`. Inside a `Row` the row is a `label`, so the whole row is the target | the On/Off buttons |
| `Seg({ value, options, label })` | the one segmented control, `role="radiogroup"` | the theme `.btn.p` radios, both `.seg` looks. Delete the first `.seg` rule set (`app.css` 714–716) |
| `SaveField({ value, onSave, label, mono?, placeholder? })` | a text field that saves on Enter; a Save button shows beside it only while the text differs; Esc puts the text back; an error from `onSave` shows on the row and the text stays | Name + the bottom Save, "Save key", the emoji field |
| `useSaved()` + `SavedMark` | "✓ Saved" for 2 s after a command answered ok, `aria-live="polite"`, once per page: beside the pane title on desktop, at the right of the header on the phone (*reason: the phone's header carries the title*) | "Saved" / "Image set" lines at the bottom |
| `useReorder(ids, onMove)` + `Grip` + `MoveButtons` | the one reorder control, 1.7 | new |
| `Skeleton({ rows })`, `LoadError({ text, onRetry })`, `Empty({ title?, text })` | 1.8 | "Loading…" text, the section's `seterr` |

Rules:

- Help text sits under the label of the row it explains. A group's foot says what the whole group does. Nothing sits above a group.
- A row's error shows on that row (`.sr.bad` and the description in `--signal-ink`), in the host's words. The control goes back to what the host has.
- One primary (`.btn.p`) button per page at most: "Open the sign-in page", "Save key" on a provider with no key, "Add provider" on the Add page and on the empty Providers page, "New project" on the empty Projects page, Done in reorder mode.
- A button that removes has red text and its label ends in "…" (it asks first, with the existing `useAsk`). It sits in the last group, `danger` (a red-tinted border).
- Phone sizes come from one rule set under `.set.phone` (rows 56 px, every control at least 44 px high, inputs 16 px text). Remove the per-control patches (`.set.phone .sbtn…`, `.m-nbody .pimg .btn`). *Reason: touch.*
- The switch is `--ink` when on, in both themes. Not `--harbour` (links and focus) and not `--calm` (passing).

### 1.3 Providers page (A1, A2, C1, C2, C7, C8)

- **List.** One card, one `Row` per provider, a button that opens `providers/<id>`: colour dot, name (one line, ellipsis), "Claude account · ~/.claude · 3 models" (kind, `whereText`, the model count; today's list of model names goes to the provider page), the quota line (`QuotaLine`, without repeating the name), the status badge (`.pst`, today's words), a chevron.
- **Found on this host.** A group: name, "Endpoint · key from OPENROUTER_API_KEY" (the key once), the note if any, Import. Foot: "Logins and keys Fyr found here. Import makes one a provider; nothing is copied off this host." More than three: the first three and "Show N more".
- **Add provider** opens `providers/new` (A5): Kind (`Seg`), Name, then Config folder with Choose… or Base URL and API key, Model ids, Cancel and Add provider. The fields and the validation are today's `AddProvider`.
- **System agents** (heading under the list; A2, C11): one group. A Host row with a select, only with more than one host. "Default" with the Provider and Model selects (`stack`). One row per job: its name, the line "Runs on Claude · claude-haiku-5-5" or the error in red, its select ("Same as default" or a provider), and a Model select once it has its own provider. Foot: "The model calls Fyr makes itself. A job that cannot run fails and says why; it never moves to a bigger model." Keep the `aria-label`s of the selects ("Provider", "Model", the job's name) and `section[aria-label="System agents"]`.
- With no provider: the empty card (C8), Found on this host, a primary Add provider, and System agents reads "Add a provider first. System agents run on one of them."

### 1.4 A provider page (A3, A4, C3, C4)

Title: the name; the status badge at the end of the title line (phone: first line of the page). Groups in this order:

1. **Login** (a Claude account) or **API key** (an endpoint). Whatever blocks the provider comes first and open:
   - needs a login: the `LoginPanel` (its states are today's: the link, the code field, Cancel, checking, failed, timed out, done; C4), then the "Login token" row;
   - needs a key: the key field with a primary "Save key", focused when the page opens (C3);
   - ready: a row "Logged in · ~/.claude" with "Log in again" (when `loginButton(p)`), and "Login token" / "API key": "Set" or "Not set" with "Replace…" / "Set…", which shows the field in place.
2. **Models** (part 1 form, over today's stored list): one row per model in the stored order: a grip, ★ (the default; pressing ☆ sends `provider.update {defaultModel}`), the name and the id in mono, × to remove (off on the default). Last rows: "Add a model id" (a `SaveField` that clears after adding) and "Default effort" (the select, when a model has efforts). Each change sends `provider.update {models: [...]}` with the whole list. The text area and "Save models" go away. Part 2 replaces × with the switch and adds the Off part (2.1).
3. **Usage**: the quota line large, and "Read from the Claude login · used by 9 threads" (map `p.quota`: `anthropic-oauth` → "Read from the Claude login"; other kinds get a plain sentence each; `none` → "This provider reports no usage."; then "imported from …" and the thread count as today).
4. **Remove provider** (`danger`): "9 threads use it. Hand them over first." with Remove… off, or "No thread uses it." with Remove… (today's `removeProviderAsk`).

### 1.5 Automation page (A6, C10, C11)

Lede: "What Fyr does without your press. Only you change these; an agent cannot."

- A group with one row: **Deploy Fyr without asking**, description "When an agent asks to deploy Fyr and the checkout is clean and on origin's default branch. The build and the tests still decide the switch.", a `Switch` (`aria-label="Auto-approve Fyr deploys"` stays).
- A group **Merge without asking**: one row per repo (`sortRepos`), the name in mono, a `Switch` (`aria-label="Auto-merge <repo>"` stays). Last row: the field `owner/repo` and Add (today's `repoKey`; "Write it as owner/repo." under it when it does not parse). Foot: "In a repo that is on, an agent's merge ask merges once the checks pass on the head it asked for. A repo shows here once a thread has a pull request in it." Empty: "No repos yet. A repo shows here once a thread has a pull request in it." More than eight repos: a search field above the rows.

Keep `section[aria-label="Auto-deploy"]` and `section[aria-label="Auto-merge"]` on the two groups.

### 1.6 Projects page, the project page, This device

**Projects** (A7 without the grips, C9, C10): "New project" at the end of the title line (phone: first on the page), the list (the `pli` rows of today, by name in part 1), a search field above it from nine projects ("Search 43 projects"; it filters by name and folder; "5 of 43 match."). A quiet project's second line starts "Quiet until 06:00 · ". New project opens today's `NewProjectStep` in place of the list. **Hosts** follow under their own heading: one card, a row per host, the name in mono, `hostStatus` as the description (an error in red).

**A project** (A8, C12), `ProjectEditor` rebuilt from the same parts; no Save button:

- Head: the icon at 44 px, the name, "~/git/fyr · 4 threads".
- **Identity**: Name (`SaveField`; an empty name is refused on the row: "A project needs a name."); Colour (the swatches, `role="radiogroup"`; a press saves); Icon (tabs Image, Letters, Emoji, Symbol — today's "Icon" kind is renamed Symbol; the Image tab has the thumbnail, Upload… or Replace…, Remove, and "PNG, JPEG or GIF, at most 2 MB."; with an image the other tabs say "Used when the image is removed or cannot load."; a pick saves); "Set up automatically" (a row: "An agent finds the logo and the real name and fills in what you have not set.", then `setupLine`'s text or its error; the button reads "Run setup" before the first run and "Run again" after; without a folder it is off and the row says "Needs a folder."). "Set by agent" marks stay on Name, Colour and Icon.
- **Threads**: Folder (the path in mono and `FOLDER_LINE`, or `NO_FOLDER_LINE`; Change… or Choose…); "Default provider and model" (two selects, `stack`; "What a new thread here starts with."; a provider change sends `{profile, model: ""}`).
- **Quiet hours**: the header's end says `quietNote` ("Quiet now, until 06:00"). One row per window: the two `TimePick`s, an "All day" `Switch`, ×, and the seven day chips under them (`.qd`). Last row: "Add quiet hours" and the presets that are not there yet. Foot: `quietLine`. A change saves 600 ms after the last one (so stepping through the hours is one command) and at once when the page closes.
- **Delete project** (`danger`): "Its 4 threads stay, without a project." and Delete… (`deleteProjectAsk`).

Each save sends `project.update` with only the changed field, so an untouched field does not become the owner's (V0.116 (2)).

**This device** (A9, C13). Lede: "These are kept in this browser. Each device has its own." One group:

- **Notifications**: a `Switch`. Description by state — off and on: "When a thread asks you something or fails."; on also shows "Send test" before the switch, and after a test the description reads "Sent. It should arrive in a few seconds." for 5 s; denied: "Blocked for this site. Allow them in the browser's site settings, then reload Fyr." in red, switch off and disabled; unsupported: `pushReason` (on an iPhone that has not installed Fyr: "On an iPhone, add Fyr to the Home Screen first: Share, then Add to Home Screen."), disabled.
  - *V0.notification-polish:* off and on now read "When a thread needs you: a question, an approval, a failure."; after a test, "Sent to this device. It should arrive in a few seconds." (the test goes to this device only). While they are on, a second row **Finished threads** (a `Switch`, on by default, kept in this browser as `fyr.push.prefs` and sent to the front): "Also when a thread finishes. Nothing arrives for the thread on screen or in a project's quiet hours."
- **Theme**: `Seg` Auto, Light, Dark.
- **Show tool details**: a `Switch`, "Every tool group open, every machine line in full." (new on the desktop Settings; the `?` bar keeps its own.)
- **Install Fyr** (only when `canInstall`): "As an app with its own window." and Install.

### 1.7 The reorder control (B1, B2, B3)

One hook, `useReorder(ids, onMove(from, to))`, and two faces:

- **Desktop:** a grip (`⠿`, 20 px) at the start of each row. Drag with the pointer: the row lifts (`.lift`), a 2 px `--harbour` line shows where it lands, the drop calls `onMove`. The grip is a focus stop: Alt+↑ and Alt+↓ move the row and keep the focus on it; its label is "Move Opus 5.5, 1 of 4".
- **Phone:** a "Reorder" button in the group's header. It turns the list into a mode: each row shows ↑ and ↓ (44 px each) in place of its other controls, rows that cannot move are left out, and "Done" leaves the mode. *Reason, to be written next to the code: touch has no hover to reveal a grip, and a drag on a list fights the page's scroll.*
- Every move saves at once. While a search field has text, reorder is off (no grips, no Reorder button).
- Part 1 uses it for a provider's models. Part 2 uses the same control for projects.

### 1.8 States

- **Loading** (C1): `Skeleton` rows, still (no shimmer: nothing moves on its own). Each part of a page loads by itself. The nav's summary line is empty until its data is there (the row keeps its height).
- **A failed read** (C2): `LoadError` in place of that part: "Could not read the providers: <reason>." and "Try again". The rest of the page is untouched.
- **A refused change** (C10): 1.2.
- **No connection** (C11): a line at the top of the pane (phone: under the header), "No connection to desk. Settings show what was last read and cannot be changed until it is back.", and every control disabled.
- **Long text**: a name in a list row is one line with an ellipsis; a title, a model id, a repo, a host name and an error wrap (`overflow-wrap: anywhere`); nothing runs past the right edge at 360 px.

### 1.9 Behaviour changes (everything else is layout and words)

1. Settings is four pages with a nav; a provider, Add provider and a project are sub-pages. The desktop Project dialog is gone.
2. The project page saves each change at once; its Save button is gone. Quiet hours save 600 ms after the last change.
3. A provider's models are edited as rows (reorder, ★, ×, add); "Save models" is gone. Their order is now the owner's to set.
4. A provider's login panel shows on the provider's page, not in the list. The list row shows the badge "Needs a login".
5. The key and token fields are behind "Replace…" / "Set…" when one is set.
6. Notifications are switched in Settings → This device. The desktop bell opens that page. `PushDialog` and the phone's push sheet are gone.
7. The phone's More sheet has no Notifications and no Theme rows.
8. Show tool details is also in Settings (desktop and phone).
9. Words: "Same as default" (was "Same as above"); "Symbol" (was the icon kind "Icon"); "Deploy Fyr without asking" (was "Approve Fyr deploys"); "Merge without asking" (the group; the page has no "Auto-merge" or "Auto-deploy" heading); "Add" for a repo (was "Turn on"); "Run setup" / "Run again" (was "Set up automatically" as a button; it is now the row's title).
10. Projects and Automation show a search field from nine rows.

### 1.10 Spec row (take the next free V number on origin/main when the PR opens; V0.118 on 16 Nov)

> | V0.NNN | → V0.50, V0.58, V0.102, V0.111, V0.115, V0.116, V0.15. **Settings is four pages behind one nav, every change saves at once, and one set of controls serves desktop and phone.** *Amends V0.50 (one page of sections), V0.55 and V0.58 (the project screen's Save button, the desktop Project dialog), V0.15 (the Notifications dialog and sheet).* (1) **Shell.** Settings has the pages Providers (with System agents), Automation (Auto-deploy and Auto-merge), Projects (with Hosts) and This device (Notifications, Theme, Show tool details, Install). `SettingsNav` lists them with one line of state each ("3 · 1 needs a login"). On the desktop it is the left column of the Settings dialog, which keeps one size; on the phone it is the Settings screen and a page is the next screen (the reason for the difference: width). A provider, Add provider and a project are sub-pages; the desktop's separate Project dialog is gone, and "Edit …" opens Settings at the project. (2) **Controls** (`web/src/settings-rows.tsx`): a switch for on or off, one segmented control, a text field that shows Save only while it differs, rows with the help text under the label, errors on the row that failed, "Saved" for 2 s beside the page title. On the phone a row is 56 px and every control at least 44 px, from one rule set. (3) **Save at once.** The project page has no Save button: each field sends its own `project.update`; quiet hours save 600 ms after the last change. A provider's models are rows (reorder, ★ for the default, ×, add an id), each change one `provider.update`. (4) **Reorder** is one control: a grip to drag on the desktop (Alt+↑/↓ from the keyboard), a Reorder mode with ↑ and ↓ on the phone (touch has no hover, and a drag fights the scroll). (5) **Notifications** are a switch on This device, with Send test; the desktop bell opens that page; the phone's More sheet has no Notifications and no Theme rows. (6) **States.** Loading is still rows; a failed read has Try again and leaves the rest of the page; with no connection the page is read-only and says so. Tests: `web/test/settings-nav.test.ts`, `web/test/reorder.test.ts`, `web/test/providers.test.ts`, `web/test/projects.test.ts`, `web/test/nav.test.ts`, and the layout test's V0.NNN checks (desktop and phone). |

### 1.11 Tests (red first)

Unit (`node --test`, pure modules; put the logic in `.ts` files so it is testable):

- `web/test/settings-nav.test.ts` (new, for `web/src/settings-nav.ts`): the four pages in order; each summary line from sample data (no providers, one that needs a login, two that need you, a job that cannot run; deploys on or off with 0, 1, 3 repos on; 1 and 3 hosts; push off, on, denied; each theme); the badge count; `parsePage("providers/abc")` and `pageUrl` round trip; an unknown page falls back to `providers`.
- `web/test/reorder.test.ts` (new, for `web/src/reorder.ts`): `move(ids, from, to)` for first, last, same place, out of range; `moveBy(ids, id, -1 | 1)` stops at the ends; the label "Move X, 2 of 4".
- `web/test/nav.test.ts`: the new `settings` screens to and from the URL; `?settings` alone is the nav; `?project=` is unchanged.
- `web/test/providers.test.ts`: the list line ("Claude account · ~/.claude · 3 models", "1 model"); the usage sentence per quota kind; `modelsAfter(models, {add | remove | move | star})` (remove of the default is refused; add trims and refuses a double).
- `web/test/projects.test.ts` / `quiet.test.ts`: the per-field patch (`patchFor(field, value)` sends only that field); the 600 ms quiet-hours save keeps only the last value (a fake timer); the search filter over name and folder.
- `web/test/push.test.ts`: the Notifications row's description and disabled state per `PushStatus` and reason.
- `web/test/bundle.test.ts` must still pass (no mock data in the start bundle).

Layout e2e (`web/test/layout.e2e.mjs`; take `mcp__fyr__slot_acquire {kind: "e2e"}` first):

- Replace the V0.50 desktop block's sections check with: the dialog has the nav with four rows "Providers|Automation|Projects|This device", opens on Providers with 3 provider rows, and keeps the same width and height on all four pages (click each, compare the rect).
- V0.115 desktop and phone: the selects' labels are unchanged ("Provider|Model|Thread titles|Project setup"); the job option reads "Same as default".
- Phone `?mock=rail&settings`: four nav rows, each at least 56 px, each with a summary. `?settings=providers`: today's "settings providers" checks, except the login panel, which moves to `?settings=providers/<id of the work account>` (link, code field, Cancel, all 44 px). `?settings=automation`: the Auto-deploy switch is `role="switch"`, `aria-checked="true"`, its row at least 44 px. `?settings=device`: the Notifications switch, the theme `radiogroup` with three radios.
- For every settings page and sub-page at 360, 390 and 430: no control under 44 px high, nothing past the right edge, the header under the top inset, the last control above the bottom inset; with the keyboard up, a focused `SaveField` and its Save are in the visible area.
- The project screen (`?project=p_fyr`, `p_home&quiet=p_home`, `p_web`): no `Save` button; the groups "Identity|Threads|Quiet hours" in this order; the V0.55 folder checks and the V0.116 image checks keep passing with the new markup; "Delete…" opens the confirm sheet.
- Save at once, desktop: on `page=projects/p_web` click a colour swatch; the mock records one `project.update` whose args have `color` and no `name`; "Saved" shows, then is gone after 2 s.
- Reorder: desktop, on a provider page: focus the second model's grip, Alt+↑, the mock records `provider.update` with the two ids swapped. Phone: Reorder shows ↑ and ↓ of 44 px and no switches or ×; ↓ on the first row swaps; Done restores the rows.
- The More sheet (phone): rows are "Select threads|Changes|Settings|Filter by project…|Adopt from T3|Show tool details".
- Desktop: the bell opens Settings on This device.
- Dark theme: one screenshot per page, no check beyond "renders".

### 1.12 Files (part 1)

| File | Change |
|---|---|
| `web/src/settings-rows.tsx` (new) | `Group`, `Row`, `Switch`, `Seg`, `SaveField`, `SavedMark`, `Grip`, `MoveButtons`, `Skeleton`, `LoadError`, `Empty` |
| `web/src/settings-nav.ts` (new) | the pages, the summaries, the badge count, `parsePage`, `pageUrl` |
| `web/src/reorder.ts` (new) | `move`, `moveBy`, the labels; `useReorder` lives in `settings-rows.tsx` |
| `web/src/settings-ui.tsx` | `Settings` becomes the shell (`SettingsNav` + page); the sections become the four pages and the provider and Add pages; reuse `LoginPanel`, `QuotaLine`, `useQuotas`, `useAsk`, `useFolderPick`, the helpers of `providers.ts`, `automerge.ts`, `system-agents.ts`, `hosts.ts` |
| `web/src/project-ui.tsx` | `ProjectEditor` in groups with per-field saves; `QuietRow` and `FolderField` as rows; `ProjectIcon`, `ProjectList`, `NewProjectStep`, `QuietHold`, `QuietGate` unchanged |
| `web/src/providers.ts`, `projects.ts`, `quiet.ts`, `push.ts` | the pure helpers the tests name |
| `web/src/app.tsx` | the Settings dialog's size and `page` state, `?page=`; `dialog === "project"` routes into it; the bell; remove `themeControl`, `PushDialog` use |
| `web/src/phone.tsx`, `nav.ts` | the settings screens; the More sheet; remove the `push` sheet; `MProject`'s title |
| `web/src/pwa-ui.tsx` | remove `PushDialog`; `PwaControls` stays (the bell, Install) |
| `web/src/app.css`, `phone.css` | part 3 of `design/settings/mock.css` (drop the `.phx`/`.dlgx` prefixes: `.set.phone` and `.dlg .set`); remove `.sets`, `.setrow`, `.amrow`, `.sarow`, `.sajob`, `.provb`, the first `.seg`, the per-control phone patches |
| `web/src/mock.ts`, `mock-providers.ts`, `mock-api.ts` | record commands for the e2e checks; a provider with no key, a failed read and an offline state behind mock parameters for screenshots |
| `web/test/…` | 1.11 |
| `docs/SPEC.md` | the row of 1.10; "amended by V0.NNN" notes on V0.50, V0.55, V0.58, V0.15 |

---

## Part 2: model discovery and project order

### 2.1 The model chooser (provider settings only; A3, A4, B1, C4, C5, C6)

**What the owner sees.** The Models group of a provider page lists what the provider reports.

- Header: "Models", then at the end "Checked 12 min ago" (or "Checking…", "Last checked yesterday 14:02", "Not checked: needs a login" / "needs a key"), "Refresh", and on the phone "Reorder".
- A search field shows from nine models; it filters both parts by name and id; the header then says "13 of 342 match".
- **On** ("On · offered in every model picker, in this order"): grip, ★, name, id in mono, a switch. ★ marks the default (one, always on). The default's switch is on and locked; pressing it says "Star another model first." on the row.
- **Off** ("Off · 3"): name, id, a switch; by name. With more than eight (or while searching, more than three), "Show N more". Turning one on puts it at the end of On.
- Marks: **New** (the provider reported it for the first time after Fyr's first successful check of this provider; it stays until the owner switches it or 14 days pass); **Gone** (it is on, and the last successful check did not list it: "The provider no longer lists it. Threads on it keep running."; a model that is off and gone leaves the list); **Added by hand** (the owner typed its id; only these have ×, when off).
- Last rows: "Add a model id by hand" and "Default effort". Foot: "★ is the default for a new thread. Fyr asks the provider for its models when you open this page and once a day." A failed check puts its reason there in red: "Could not list the models: api.deepseek.com/v1/models answered 401. The list below is the one from yesterday; it still works."
- The Providers list row adds " · 4 new" to its line while a provider has New models.
- Add provider: the Model ids field is optional for an endpoint; after adding, Fyr checks at once.

**Every other picker** (New thread, Hand over, System agents, the project's default, a provider's Default effort) keeps its control. It lists the models that are on, in the owner's order. A thread, a job or a project whose model is now off still shows it, as "Opus 5.4 (off)", until another is picked; it is offered nowhere else. A thread on a model that is off keeps running. A project whose default model is off says so on its row: "Opus 5.4 is turned off in Claude. New threads here start on Claude's default until you pick another." (C12).

**Backend.**

- `provider.Provider` gains `Catalog []CatalogModel` (`ID`, `Name`, `Efforts`, `FirstSeen`, `Gone bool`, `Manual bool`), `CheckedAt`, `CheckError`. **`Models` stays what it is: the models that are on, in the owner's order.** So `Profile()`, `ModelCaps`, `EffortFor`, `HaikuModel` and every picker need no change, and the migration is: nothing moves; the stored `Models` are on; `Catalog` starts empty and the first check fills it; no model is New after that first check.
- Discovery, on the host (the key never leaves it):
  - an endpoint: `GET <base URL>/v1/models` with the key, as the adapter sends it to that endpoint (Anthropic style `x-api-key` and `anthropic-version`; if that answers 401 or 404, `Authorization: Bearer`); read `data[].id` and `display_name` or `name`; follow `has_more` / `after_id` up to 20 pages; 15 s; at most 2 MB a page; only the provider's own base URL, no redirect to another host.
  - a Claude login: `claudecli.Models`, plus the models the CLI itself reports for that login. **Find out first what the installed `claude` reports** (for example in its stream's init message or a models command) and write what you found in the PR; if it reports nothing usable, the catalog is `claudecli.Models` and the spec row says so. Do not invent a CLI flag.
- When: `provider.refreshModels {id}` (owner; the page sends it on open when `CheckedAt` is older than 10 minutes, and on Refresh), and the host itself once a day per provider that can run (ready, with its key). A check never runs for a provider that needs a login or a key. One check per provider at a time.
- A failed check stores `CheckError` and changes nothing else.
- `provider.update {models}` keeps its meaning (the on-list, in order). An id that is not in the catalog is added to it as `Manual`. Removing a `Manual` model that is off drops it from the catalog (`provider.update {forget: [ids]}`).
- `ProviderDTO` gains `catalog`, `checkedAt`, `checkError`; the system-agents view is unchanged.
- **V0.117 stays:** a Claude login's default system agent is its Haiku even when the on-list lacks it. Add a test that turns Haiku off and still gets it for the default system agent.
- Only the owner changes any of this (owner commands with receipts); no agent tool.

### 2.2 Project order (A7, B2)

- **One order, the owner's.** A list of project ids, one list across all hosts, kept by the front (beside the device data it already holds, `internal/front`), because a front merges the projects of its hosts and no single host knows them all. It is in the state the front serves (`projectOrder`), so every device of the owner sees the same order. `front.projectOrder {ids}` sets it: owner only (the browser's own command path, with the X-Fyr header), never an agent tool, never an MCP tool.
- **Sorting.** `orderProjects(projects, order)` in `web/src/projects.ts`: the projects named in `order` first, in that order; then the rest by creation time (a new project lands at the end); an id that no longer exists is ignored. Before the owner has set an order, the order is by name (today's Settings order), so nothing jumps on the day this ships.
- **Where it is used:** New thread's project list, the project picker ("Project…"), the filter sheet, the filter chips on the desktop and the phone (`pfrow`), Settings → Projects, and any other list of projects (grep for `matchProjects`, `ProjectList` and sorts of `projects`; check `board-ui.tsx`). Today's order by last use (`projects.ts`, the sort at line 128) goes away. *This amends V0.30 ("the projects by last use").* Search in a picker still filters; it does not reorder.
- **Quiet projects** stay hidden wherever V0.74 and V0.114 hide them; their place in the order is kept. Settings → Projects lists them (it is where their hours are changed), marked "Quiet until 06:00".
- **The page.** Settings → Projects shows the grip on each row on the desktop and "Reorder" beside "New project" on the phone (the mode of 1.7; in it the rows show ↑ and ↓ in place of the thread count). Foot: "This order is used in New thread, the project chips and every list of projects, on all your devices." While the search has text, reorder is off.
- A front that cannot store the order (read-only disk) refuses the command with the reason; the row goes back.

### 2.3 Spec rows (two rows, the next free V numbers when the PR opens)

> | V0.NNN | → V0.44, V0.50, V0.115, V0.117, decision 0015. **A provider's models are discovered; the owner turns each on or off, orders them and stars the default.** (1) **Catalog.** Each provider keeps what its endpoint or CLI reports (`Catalog`, with when each model was first seen). An API endpoint is asked at `GET /v1/models` with its key, on the host; a Claude login uses `claudecli.Models` plus what the CLI reports. Fyr checks when the owner opens the provider's page (at most every 10 minutes), on Refresh, and once a day; a provider that needs a login or a key is not checked; a failed check keeps the last list and shows its reason. (2) **On, order, default.** `Models` is the list that is on, in the owner's order; ★ is `DefaultModel` and is always on. The stored models of V0.44 and V0.50 start as on; nothing else moves. (3) **Marks.** New: first reported after the provider's first check, until the owner switches it or 14 days. Gone: on, and no longer reported; it stays and its threads keep running; off and gone leaves the list. Added by hand: an id the owner typed. (4) **Settings → Providers → a provider → Models** (desktop and phone): search from nine models, the On part with reorder (V0.NNN-1 (4)), ★ and a switch, the Off part with a switch. (5) **Other pickers** keep their control and list only what is on, in order; a model in use that is off shows as "(off)" there and nowhere else. (6) V0.117 holds: the default system agent of a Claude login is its Haiku even when Haiku is off. Tests: … |
>
> | V0.NNN | → V0.23, V0.30, V0.58, V0.74, V0.114, decision 0016. **One project order, set by the owner, used in every list of projects.** *Amends V0.30 (the picker's order by last use).* The front keeps `projectOrder`, one list of project ids across all its hosts, and serves it in the state; `front.projectOrder {ids}` sets it, owner only, no agent tool. Listed projects come first in that order, then the rest by creation time, so a new project is last; with no order set, by name. New thread, the project picker, the filter sheet, the filter chips (desktop and phone), Settings → Projects and every other list of projects use it. Quiet projects stay hidden where V0.74 and V0.114 hide them and keep their place. Settings → Projects reorders with the one reorder control: a grip on the desktop, a Reorder mode with ↑ and ↓ on the phone (touch). Tests: … |

### 2.4 Tests (part 2, red first)

Go: `internal/provider` (catalog merge: new, gone, manual, first check marks nothing New, 14 days, a failed check changes nothing; `Models` untouched by a check); the models fetch against `httptest` (both auth styles, paging, the size and time limits, a redirect to another host refused, the key never in an error text); `internal/engine` (`provider.refreshModels` is the owner's; the daily check skips a provider that needs a login or a key; `provider.update` adds a hand id as Manual and `forget` drops it; the default cannot be turned off); `internal/agent` (`HaikuModel` with Haiku off, V0.117); `internal/front` (`front.projectOrder` is the owner's; the order is in the state and survives a restart; an agent path cannot send it).

Web unit: `providers.test.ts` (the two parts from a DTO; the search; the header line per state; "(off)" options via `withCurrent(options, value)`; " · 4 new"); `projects.test.ts` (`orderProjects`: listed first, new last by creation time, unknown ids ignored, no order → by name; quiet filtering keeps the order); `new-thread.test.ts` and `rows.test.ts` where they assumed last-use order.

Layout e2e: a provider page on the desktop and the phone with the two parts, a locked default switch, New and Gone marks; search on a mock provider with 40 models; Refresh records `provider.refreshModels`; the phone's Reorder mode for projects (44 px, ↓ swaps, the mock records `front.projectOrder`); after a reorder the desktop's project chips and New thread's list follow the new order; a thread on a model that is off shows "(off)" in Hand over.

### 2.5 Files (part 2)

`internal/provider/provider.go` (+ a new `catalog.go`, `discover.go`), `internal/cli/claudecli`, `internal/engine` (the commands, the daily check), `internal/api` (the DTO), `internal/front` (the order, the state, the command), `internal/server` (routes if needed), `docs/API-v0.md`, `docs/SPEC.md`; `web/src/settings-ui.tsx` (the Models group, Projects reorder), `providers.ts`, `projects.ts`, `types.ts`, `api.ts`, `inbox.tsx` and `phone.tsx` (the chips), `project-ui.tsx` (`ProjectList`), `new-thread*.ts`, `handover*.ts*`, `system-agents.ts`, `mock*.ts`, the tests above.
