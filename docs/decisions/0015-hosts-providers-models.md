# 0015 Hosts, providers, models, projects and workspaces

Date: 2026-11-14 · Status: accepted (owner, three-round walk-through) · Builds on 0008, 0012, 0013

**Context.** Fyr grew "profiles" (claudeAlt, claudeAcme, claudeDeepseek) that mix an account, an endpoint and a CLI, defined in NixOS and T3 wrappers. Quota (V0.44) and handover (V0.45) need a clear unit, and a second host is coming. The owner asked to plan how hosts, providers, workspaces and projects relate, and to be grilled.

**The model.**

| Concept | Is | Has |
|---|---|---|
| Host | a machine running `fyrd` | folders, CLIs, provider logins, workspaces |
| Provider | an account behind an endpoint, on a host | a login or API key, curated models, quota |
| Model | what a provider runs (Opus, Sonnet, DeepSeek Flash) | an id, a display name |
| Project | a topic | a name, folders on hosts (one main), an automatic repo line, quiet hours, a default provider + model |
| Workspace | the folder or worktree a thread runs in, on one host | path, branch, host |
| Thread | one conversation | one host, one workspace, one provider + model (Hand over… changes the provider) |

**Decisions.**

| Question | Decision |
|---|---|
| Vocabulary | **Providers + models.** The UI talks about providers (accounts) and models, shown as "Claude (acme) · Opus". "Profile" disappears from the UI; the CLI is an internal detail. |
| Providers and hosts | **Per host.** Each host has its own providers (its logins and CLIs). The same account on two hosts shows as one provider "available on laptop, server" with one shared quota. |
| Where providers are configured | **In Fyr's UI**, in one **Settings** screen (gear in the rail on desktop, in the phone menu) with sections Providers, Hosts, Notifications, Appearance. |
| Claude subscription login | **Fyr runs the CLI's own login**: each account gets its own config folder (`~/.local/share/fyr/providers/<id>`), Fyr runs `claude /login` there and shows the link/code in the page; the CLI keeps and refreshes the token. |
| API keys | **A private file per key**: `~/.local/share/fyr/secrets/<provider>.key`, mode 0600, never in the database, never sent to clients; the UI shows only "set · ••••4f2a" and can replace it. |
| Migration | **Import once, then the UI.** Fyr creates today's three as its own providers; afterwards the UI is the place. |
| The wrappers | **Fyr does not rely on the `claude-*` aliases/wrappers** (owner: "we should not RELY on the claude aliases we have today"). Every provider runs the plain `claude` CLI, and Fyr sets what the wrapper used to set: `CLAUDE_CONFIG_DIR` (its own folder per Claude account), `ANTHROPIC_BASE_URL` and the model variables for an endpoint such as OpenRouter, and the key from Fyr's key file. The import copies only non-secret settings (name, endpoint URL, model ids); when detection finds the acme login or the OpenRouter key, Import reuses them; otherwise the account logs in once through Fyr or the key is entered once in Settings. The main account keeps using `~/.claude` until it is moved. The wrappers can then be removed from `~/nixos-config`. |
| Detection | **Auto-detect with an Import button** (owner: "auto-detecting is nice... Or an import button"). Settings → Providers lists "Found on this host": Claude logins (`~/.claude` and other `CLAUDE_CONFIG_DIR` folders), endpoint settings and keys found in the existing setup (T3 profiles, the wrappers' definitions, the daemon's environment). One Import per find creates a Fyr provider that points at the found login folder or copies the key into Fyr's key file (on the owner's click only). Detection reads; it never changes anything, and running a provider never goes through a wrapper. |
| Models | **Discovered, then curated**: Fyr asks the CLI (its startup handshake lists models) at login/test; the owner ticks which ones show in pickers and can add a custom id. |
| Projects and providers | **A default per project** (provider + model) for new threads; changeable per thread; nothing switches automatically. |
| Quota shows | On the **Providers page** (full detail), **where a provider is picked** (new thread, Hand over…, model change), and **on the thread** (its provider's quota when high, with a Hand over… shortcut). |
| Which host a thread runs on | **Where its folder is**: the host follows the picked folder (the project's main folder on a host); the new-thread screen shows "on host-a" and offers other hosts that have a folder for the project. |
| Workspace visibility | **A quiet line in the thread header**: "fyr-polish · branch ui-polish · host-a"; a tap shows the full path and actions. |

**Consequences.** `Profile` becomes `Provider` (+ curated models) in the store and API, with an import step at start; T3/NixOS profile seeding is replaced by the import. Quota (V0.44) attaches to providers. Handover (V0.45) picks provider + model. Secrets live in files under `~/.local/share/fyr/secrets/` (0600). The Settings screen is new. Hosts other than the local one come with the tailnet transport (M0); until then the Hosts section shows this host only.

## Addendum: the foundation, sharpened (owner approved, same day)

The owner refined: "A provider abstracts away auth mechanism and cli handling mechanism. Keys should be stored safely in a reused way. Models and effort is chosen separately." On challenge, the owner accepted five sharpenings:

1. **The CLI adapter is launch + protocol + sessions + capabilities.** What Fyr needs from an agent CLI (start, send, interrupt, answer callbacks, resume and where sessions live, the event stream, capabilities) is one interface; the claude stream-json code is its only implementation for now. A second CLI is a new adapter that translates that protocol, not a set of flags.
2. **Auth is data the adapter applies**: `{kind: cli-login | api-key | none, configDir?, secret?, endpoint?}`. The adapter declares which kinds it accepts and how it injects them.
3. **Quota is keyed by the auth identity** (login folder, or secret + endpoint), so providers on one account share one quota.
4. **Models carry their capabilities** (which effort levels, if any). A thread holds provider + model + effort. A project default is a preference ("Opus, high, preferably on Claude (acme)"), and Hand over… maps model and effort to the nearest match on the new provider and says so.
5. **One secret store behind an interface** (Put, Get, Delete, Has, Last4); files 0600 in a 0700 folder now, a different backend (age, sops, systemd-creds) later without provider changes. Secrets are never logged or sent to clients.
