// The Hosts settings pages (split from settings-ui.tsx). V0.settings-ia: a host's page holds its own (the outline and
// the folding rule are stated above HostPage); the sections the host reports are in settings-host-groups.tsx.

import { HostDiagnostics, HostLoadSection, HostUpdates, diagnosticsLine, hasLoad, useHostLoad } from "./settings-host-groups.tsx";
import type { PageUi, SettingsProps } from "./settings-ui.tsx";
import { setupFoldLine, type SetupReport } from "./setup.ts";
import type { Data } from "./settings-load.ts";
import { readOnlyNote, type SettingsHost } from "./host-settings.ts";
import { HostMark, useHostMarks } from "./host-mark-ui.tsx";
import { HOST_ICON_NAMES, hostGlyph, ownerIcon } from "./host-mark.ts";
import { HOST_GLYPHS } from "./lucide.ts";
import { PALETTE_NAMES } from "./projects.ts";
import { useEffect, useRef, useState } from "preact/hooks";
import { type ComponentChildren } from "preact";
import { hostLine, linkLine, hostState, hostRoute, shortID, updateAction, updateTrouble, updateWords, type HostInfo } from "./hosts.ts";
import { setupLine } from "./projects.ts";
import { SetBy } from "./project-ui.tsx";
import { hostJumps, providersSummary, type HostSection, type PageRef } from "./settings-nav.ts";
import { ConfirmButton, Group, Row, SaveField, Section, SetContext, Switch, useSet } from "./settings-rows.tsx";
import { Grip, MoveControls, ReorderButton, useReorder } from "./reorder-ui.tsx";
import { useAsk } from "./confirm-ui.tsx";
import { removeHostAsk } from "./settings-asks.ts";
import { useLoadRead } from "./load-ui.tsx";
import { useUnd } from "./undeclared-ctx.tsx";

// ---- Hosts (V0.125, decision 0026 slice 1) ----

/** One host in Settings → Hosts, the same row on the desktop and the phone (the phone's 56 px and larger type come from
 *  the one `.set.phone` rule set, for touch): the state mark, the name, one line (state, route, short id); a changed id
 *  replaces the line and is the only red row. The row opens the host's page, where every action is. */
function HostRow({ h, now, go, still, view }: { h: HostInfo; now: number; go(r: PageRef): void; /** the phone's Reorder mode: the row does not open, and it shows the mark and the name only (with ↑, ↓ and the handle beside it, a line of state and a chip left the name a syllable a line) */ still?: boolean; /** the host's updates view, for a host whose row carries no update summary */ view?: { bad?: { commit: string; why: string } } | null }) {
  const env = useSet();
  const line = hostLine(h, now, env.readOnly); // no connection: the state is as last read, not "online"
  const link = linkLine(h.link, now); // decision 0030 Amendment 1: how the front reaches it, and why not
  // V0.settings-ia: the chip is the count (settings-nav.ts settingsCounts): amber, and the row red, on exactly the rows
  // the Hosts badge counts (a changed id; an update the owner can press on, while the host is online). What is not
  // counted is grey state and says why in its words: an offline host's held-back release ("· offline"), a host too
  // old to be read (its update is a command on that host), and every row while this device has no connection.
  const trouble = updateTrouble(h, view);
  const counted = !env.readOnly && (!!h.idWas || (h.online && !!h.id && !!trouble));
  const word = h.idWas ? "Needs you" : !h.id ? "Update needed" : trouble ? (h.online ? trouble.chip : `${trouble.chip} · offline`) : "";
  if (still) return <Row class="host" lead={<HostMark name={h.name} size={24} />} title={h.name} mono data={{ host: h.name, sub: h.name, ...(h.id ? { id: h.id } : {}) }} />;
  return (
    <Row
      class={counted ? "host bad" : "host"}
      onClick={() => go({ page: "hosts", sub: h.name })}
      lead={<HostMark name={h.name} size={24} />}
      title={h.name}
      mono
      desc={h.idWas ? (counted ? undefined : line) : link ? <>{line}<span class="hlink">{link}</span></> : line}
      error={h.idWas && counted ? line : null}
      data={{ host: h.name, sub: h.name, ...(h.id ? { id: h.id } : {}) }}
      end={
        <>
          {word && <span class={counted ? "pst needs-login" : "pst"}>{word}</span>}
          <span class="chev">›</span>
        </>
      }
    />
  );
}

/** The Hosts page: the hosts in the owner's order (V0.132). A host gets on the list by pairing or by the config
 *  (V0.retire-discovery); V0.settings-ia: Add a host in the title line goes to the two groups that pair one, and the
 *  foot here says only what the order does. */
/** V0.132: the owner orders the hosts with the one reorder control (V0.119 (4)): a grip on the desktop, a Reorder mode
 *  with ↑ and ↓ on the phone. The first is the default host. Each move saves at once (front.hostOrder, kept on every
 *  host) and shows on this device first. */
export function HostsPage({ rows, now, go, ui, run, viewOf }: { rows: HostInfo[]; now: number; go(r: PageRef): void; ui: PageUi; run(names: string[]): Promise<string | null>; /** a host's updates view as Settings read it */ viewOf(h: HostInfo): { bad?: { commit: string; why: string } } | null }) {
  const env = useSet();
  const ro = useReorder(
    rows.map((h) => h.name),
    async (next) => {
      const e = await run(next);
      if (!e) env.saved();
      return e;
    },
  );
  const shown = ro.sort(rows, (h) => h.name); // a move shows here first, until the hosts event catches up
  const can = rows.length > 1 && !env.readOnly;
  const mode = env.phone && ui.reordering && can;
  return (
    <Group
      label="Hosts"
      foot={rows.length > 1 ? "The first host is the default for New project, New thread and the folder picker. The order is kept on your hosts, so every device shows it." : "This is your only host."}
    >
      {ro.err && (
        <div class="seterr" role="alert">
          {ro.err}
        </div>
      )}
      {shown.map((h, i) => {
        const r = ro.row(i);
        if (mode)
          return (
            <div key={h.name} class={`hreo mvrow ${r.class}`} ref={r.ref} data-sub={h.name}>
              <HostRow h={h} now={now} go={go} still />
              <MoveControls ro={ro} i={i} name={h.name} />
            </div>
          );
        if (!can || env.phone) return <HostRow key={h.name} h={h} now={now} go={go} view={viewOf(h)} />;
        return (
          <div key={h.name} class={`hreo g ${r.class}`} ref={r.ref}>
            <Grip {...ro.grip(i, h.name)} />
            <HostRow h={h} now={now} go={go} view={viewOf(h)} />
          </div>
        );
      })}
    </Group>
  );
}

/** The phone's Reorder and Done for the Hosts page (touch has no hover for a grip, and a drag fights the scroll). */
export function HostsReorderButton({ ui, n }: { ui: PageUi; n: number }) {
  const env = useSet();
  if (!env.phone || n < 2 || env.readOnly) return null;
  return <ReorderButton on={ui.reordering} set={ui.setReordering} />;
}

/** V0.settings-ia: the one visible way to add a host, in the Hosts title line on the desktop and first on the page on
 *  the phone (where the header holds only the title and Back). It goes to the Add a host group: make an invite here,
 *  or join with one from the other host. While that group is not there yet (the pairing state is still being read, or
 *  its read failed) it goes to the panel, which says so; the press is never silent. */
export function AddHostButton() {
  return (
    <button
      type="button"
      class="btn addh"
      onClick={() => {
        const el = document.querySelector<HTMLElement>(".peers .pinvite") ?? document.querySelector<HTMLElement>(".peers");
        el?.scrollIntoView({ block: "start" });
        el?.querySelector<HTMLElement>("button:not([disabled])")?.focus({ preventScroll: true });
      }}
    >
      Add a host
    </button>
  );
}

/** The status pill at the end of a host's title (desktop) or first on its page (phone: its header has only the title
 *  and Back). */
export function HostPill({ h }: { h: HostInfo }) {
  const env = useSet();
  if (h.idWas) return <span class="pst needs-login">Needs you</span>;
  // No connection: what shows is as last read, and "Online" would say more than this device knows.
  if (env.readOnly) return <span class="pst">As last read</span>;
  // a host too old to be read answers, but nothing of it shows: "Online" alone would say all is well
  return <span class="pst">{!h.online ? "Offline" : h.id ? "Online" : "Online · update needed"}</span>;
}

/** An install id in full: four groups, the first (the short form) in ink. */
function IdFull({ id, big }: { id: string; big?: boolean }) {
  return (
    <span class={`idf${big ? " big" : ""}`} aria-label={`id ${id}`}>
      {id.split("-").map((g, i) => (
        <span key={i} class={i ? undefined : "s"}>
          {g}
        </span>
      ))}
    </span>
  );
}

/** Copy writes the id with its dashes; "Copied" for 2 s. Not rendered where the browser has no clipboard API. */
function CopyId({ id }: { id: string }) {
  const [done, setDone] = useState(false);
  useEffect(() => {
    if (!done) return;
    const t = setTimeout(() => setDone(false), 2000);
    return () => clearTimeout(t);
  }, [done]);
  if (typeof navigator === "undefined" || !navigator.clipboard?.writeText) return null;
  return (
    <button type="button" class="btn" aria-label={`Copy the id ${id}`} onClick={() => void navigator.clipboard.writeText(id).then(() => setDone(true), () => {})}>
      {done ? "Copied" : "Copy"}
    </button>
  );
}

/** A row whose text is a label, an id in full and a line under it (the id rows of a host's page). */
function IdRow({ title, id, desc, end, bad }: { title: string; id: string; desc: ComponentChildren; end?: ComponentChildren; bad?: boolean }) {
  return (
    <div class={`sr idr${bad ? " bad" : ""}`}>
      <span class="sr-b">
        <span class="sr-t">{title}</span>
        <IdFull id={id} big />
        <span class="sr-d">{desc}</span>
      </span>
      {end && <span class="sr-e">{end}</span>}
    </div>
  );
}

const sentence = (s: string) => {
  const t = s.trim();
  if (!t) return "";
  return `${t[0].toUpperCase()}${t.slice(1)}${/[.!?]$/.test(t) ? "" : "."}`;
};

/** A host's page: Identity (the full id, Copy, how Fyr trusts it today) and Connection (state, address and route, how
 *  it got on the list). A changed id: the note, both ids in full and Trust <new> (front.hostId); the foot says what
 *  Fyr does meanwhile. The same page on the desktop and the phone. */
const GLYPH_WORDS: Record<string, string> = { laptop: "a laptop (it has a battery)", monitor: "a desktop", server: "a server" };

/** V0.171: the host's mark, set by the owner: an icon (Automatic, one of the glyphs, or an emoji) and a colour
 *  (Automatic or one of the ten hues). Kept by its install id on every host (front.hostMark); the same group on the
 *  desktop and the phone. V0.208: as a project's fields, an agent fills what the owner has not set ("Set by agent"
 *  until the owner changes it), and Fill in automatically runs it on that host. */
function HostMarkGroup({ h, api, part }: { h: HostInfo; api: SettingsProps["api"]; /** the key of the host's folds */ part: string }) {
  const env = useSet();
  const marks = useHostMarks();
  const [err, setErr] = useState<string | null>(null);
  const [setupErr, setSetupErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // The group folds once a mark is set, but not under the hand that is setting it: after a change on this visit it stays open.
  const edited = useRef(false);
  if (!h.id)
    return (
      <Group title="Mark">
        <Row title={<HostMark name={h.name} size={32} />} desc="Automatic. This host runs a Fyr older than V0.124 and has no id, so its mark cannot be set until its next update." />
      </Group>
    );
  const own = marks[h.id] ?? {};
  const icon = ownerIcon(own.icon);
  const auto = hostGlyph(h.machine);
  const set = async (patch: { icon?: string; color?: string }) => {
    edited.current = true;
    const next = { icon: own.icon ?? "", color: own.color ?? "", ...patch };
    setBusy(true);
    const r = await api.frontCommand("front.hostMark", { id: h.id, ...next });
    setBusy(false);
    if (r.ok) {
      setErr(null);
      env.saved();
    } else setErr(r.error || "the mark could not be saved");
  };
  const off = env.readOnly || busy;
  const iconBy = !!own.icon && own.by?.icon === "agent";
  const colorBy = !!own.color && own.by?.color === "agent";
  const setter = (own.icon && !iconBy) || (own.color && !colorBy) ? "you" : "an agent";
  const setup0 = setupLine({ setup: own.setup });
  const setup = async () => {
    setBusy(true);
    const r = await api.command(h.id!, "host.setupRun");
    setBusy(false);
    setSetupErr(r.ok ? null : (r.error ?? "That did not work"));
  };
  const isSet = !!own.icon || !!own.color;
  return (
    // Folded once a mark is set (the header shows it and who set it): its pickers are half a screen that is used once.
    <Group
      title="Mark"
      label="Mark"
      class="hmarkg"
      fold={{
        id: `${part}:mark`,
        open: !isSet || edited.current,
        summary: (
          <span class="hmsum">
            <HostMark name={h.name} size={16} /> {isSet ? `Set by ${setter}` : "Automatic"}
          </span>
        ),
      }}
      foot="Kept on every host by its id, so every device shows it."
    >
      <Row
        class="hmprev"
        title={<HostMark name={h.name} size={32} />}
        desc={!own.icon && !own.color ? `Automatic: ${GLYPH_WORDS[auto]}, a colour from its id.` : `Set by ${setter}.${!own.icon ? ` The icon is automatic: ${GLYPH_WORDS[auto]}.` : ""}${!own.color ? " The colour is automatic." : ""}`}
        error={err}
      />
      <div class="sr col hmicon">
        <span class="sr-t">Icon{iconBy && <SetBy of="host" />}</span>
        <div class="igrid" role="radiogroup" aria-label="Icon">
          <button type="button" class={`ig auto${!icon ? " on" : ""}`} role="radio" aria-checked={!icon} title="Automatic" aria-label="Automatic" disabled={off} onClick={() => icon && void set({ icon: "" })}>
            Auto
          </button>
          {HOST_ICON_NAMES.map((nm) => {
            const on = !!icon && "glyph" in icon && icon.glyph === nm;
            return (
              <button type="button" class={`ig${on ? " on" : ""}`} role="radio" aria-checked={on} title={nm} aria-label={nm} disabled={off} onClick={() => !on && void set({ icon: nm })}>
                <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" dangerouslySetInnerHTML={{ __html: HOST_GLYPHS[nm] }} />
              </button>
            );
          })}
        </div>
        <span class="pemoji">
          <SaveField value={icon && "emoji" in icon ? icon.emoji : ""} label="Emoji" placeholder="Or an emoji" onError={setErr} check={(v) => (v.trim() && !ownerIcon(v.trim()) ? "One emoji" : null)} onSave={async (v) => {
              // Empty gives the automatic icon back; otherwise the first emoji typed.
              const i = ownerIcon(v.trim());
              await set({ icon: i && "emoji" in i ? i.emoji : "" });
              return null;
            }} />
        </span>
      </div>
      <Row
        title={<>Colour{colorBy && <SetBy of="host" />}</>}
        stack
        end={
          <div class="swatches" role="radiogroup" aria-label="Colour">
            <button type="button" class={`sw auto${!own.color ? " on" : ""}`} role="radio" aria-checked={!own.color} aria-label="Automatic" title="Automatic" disabled={off} onClick={() => own.color && void set({ color: "" })}>
              A
            </button>
            {PALETTE_NAMES.map((c) => (
              <button type="button" class={`sw${c === own.color ? " on" : ""}`} role="radio" aria-checked={c === own.color} aria-label={c} title={c} style={{ "--pc": `var(--p-${c})` }} disabled={off} onClick={() => c !== own.color && void set({ color: c })} />
            ))}
          </div>
        }
      />
      <Row
        title="Fill in automatically"
        class="psetup"
        desc={!setupErr && !setup0?.error ? <>An agent on this host looks at it and fills in what you have not set.{!h.online ? " Needs the host online." : setup0 ? ` ${setup0.text}` : ""}</> : undefined}
        error={setupErr ?? (setup0?.error ? setup0.text : null)}
        end={
          <button type="button" class="btn" disabled={off || !h.online} onClick={() => void setup()}>
            {own.setup ? "Run again" : "Run"}
          </button>
        }
      />
    </Group>
  );
}

/** What the shell hands a host's page besides the host: its part (name, online, too old), its settings as read (none
 *  for a host that cannot be read), its setup report and count, where a link landed, how to jump to a section, the
 *  per-device switch, and its pairing (for Remove). */
export interface HostPageProps {
  h: HostInfo;
  api: SettingsProps["api"];
  now: number;
  part: SettingsHost;
  d?: Data;
  hasDeploy: boolean;
  focus?: string;
  go(r: PageRef): void;
  /** land on a section of this page without a new history entry */
  jump(section: HostSection): void;
  /** the front has one host (its load reading may carry another name) */
  single: boolean;
  setup: SetupReport | null;
  /** required setup steps that are not ready (settingsCounts) */
  setupNeeds: number;
  /** V0.207: whether this device shows the host; absent with one host (nothing to hide it from) */
  shown?: { on: boolean; /** the last host shown cannot be hidden */ locked: boolean; set(on: boolean): void };
  /** the host is paired with the one this device is connected to: Remove ends that; the error, or null */
  unpair?(): Promise<string | null>;
}

// V0.settings-ia: A HOST'S PAGE, top to bottom, and THE FOLDING RULE of every Settings page that folds.
//
// The page: jump links; what needs the owner (a changed id with Trust, a note when the host is offline or too old to
// be read, a required setup step that is missing); then six sections under ONE heading level (settings-rows.tsx
// Section; no part of the page is a bare group that looks weaker than a section it equals):
//   Updates              the row that asks for a press, what runs, the Auto-update line, the history
//   Load                 memory now; Running now; Rules
//   Providers and setup  two rows that lead to this host's providers and its setup checks
//   Diagnostics          what Fyr measured and checked
//   Look                 the mark, and whether this device shows the host
//   About                the id, how Fyr trusts and reaches it
// and last, Remove (a paired host only).
// What exists once: the Auto-update switch is on Automation and the setup checks are on the Setup page (they run live
// on the host each time they are read, and Setup is where the first run lands); this page shows each as one line that
// leads there.
//
// The folding rule:
//  1. A section or group the owner acts in is open: Updates, Leftover processes, Look's switch.
//  2. One that only reports, or that is set once and left, is folded, and its header says the state in one line:
//     Update history, Heavy work Fyr did not expect, Unused worktrees, every Rules group, Diagnostics, About, Mark once
//     it is set.
//  3. A folded one that reports something wrong now shows its line in the error colour and is open: Diagnostics while
//     memory is short, an invariant is broken or the GitHub limit is low; Update history while a release is held back.
//     So is the section a link landed on.
//  4. What the owner opened or closed is remembered on this device (settings-fold.ts) and wins over 1 and 2; 3 opens
//     the fold all the same, and one press closes it.
export function HostPage({ h, api, now, part: sh, d, hasDeploy, focus, go, jump, single, setup, setupNeeds, shown, unpair }: HostPageProps) {
  const env = useSet();
  const ask = useAsk();
  const und = useUnd();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [rmErr, setRmErr] = useState<string | null>(null);
  // Decision 0031 2.6: the host the front runs in is the one its link is the pipe to, a fact about the link.
  const isSelf = h.link?.via === "pipe";
  const route = hostRoute(h);
  const trust = async () => {
    setBusy(true);
    const r = await api.frontCommand("front.hostId", { host: h.id });
    setBusy(false);
    if (r.ok) {
      setErr(null);
      env.saved(); // the hosts stream event makes this page the ordinary one
    } else setErr(r.error || "the id could not be saved");
  };
  const state = env.readOnly ? "as last read" : hostState({ ...h, deploy: undefined }, now);
  const part = sh.name; // the key of this host's folds: "" for the one part of a front with one host
  const load = useHostLoad(h.name, single);
  const reading = !useLoadRead() && h.online && !!h.id;
  const scrollTo = (id: string) => document.querySelector(`.stp [data-section="${CSS.escape(id)}"]`)?.scrollIntoView({ block: "start" });
  // A link that lands on a section scrolls it into view (leftovers scroll themselves once the load is read).
  useEffect(() => {
    if (focus && focus !== "leftovers") scrollTo(focus);
  }, [focus, h.name]);
  const jumpTo = (id: HostSection) => {
    jump(id); // opens the section (the fold's `force`)
    scrollTo(id); // and when the link already points there, scrolls all the same
  };
  const act = updateAction(h, d?.updates ?? null, now);
  const diag = diagnosticsLine(load, d, und.now());
  const blocked = d?.sa?.jobs.filter((j) => j.error).length ?? 0;
  const provNeed = h.online && d ? (d.providers?.filter((x) => x.status !== "ready").length ?? 0) + blocked : 0;
  // A jump link's word is in the count's colour only for what this host's tab counts (its update, while a press
  // reaches it). The rest is grey state: memory and diagnostics are not counted, and what Providers and Setup count
  // for this host says where it is counted.
  const jumps = hostJumps(
    { updates: !!(h.update || d?.updates || (hasDeploy && d)), load: hasLoad(load) || reading, more: true, diagnostics: diag.any, look: true, about: true },
    {
      updates: act?.chip ? (act.button === "Try again" ? "held back" : "failed") : "",
      load: load?.slots?.guard?.short ? "memory short" : "",
      more: [provNeed ? `${provNeed} on Providers` : "", setupNeeds ? `${setupNeeds} on Setup` : ""].filter(Boolean).join(", "),
      diagnostics: diag.bad ? "something is wrong" : "",
    },
    { updates: !env.readOnly && h.online && !!h.id },
  );
  const note = readOnlyNote({ ...sh, label: h.name }, d?.readAt ?? null, now);
  const toHost = sh.name ? { host: sh.name } : {};
  const under = !h.online ? (
    <>
      {h.error && <span class="err">{sentence(h.error)} </span>}
      Its threads show as last read and cannot be changed.
    </>
  ) : h.deploy && updateWords(h) ? (
    `Updating Fyr: ${updateWords(h)}`
  ) : undefined;
  return (
    <>
      {/* The page is several screens long on both fronts, so its sections are one press away (the same line on the
          desktop and the phone; on the phone it scrolls sideways, as the host tabs do). */}
      <nav class="hjump" aria-label={`Sections of ${h.name}`}>
        {jumps.map((j) => (
          <button type="button" key={j.id} class={`hj${j.counted ? " w" : ""}`} onClick={() => jumpTo(j.id)}>
            {j.title}
            {j.word && <span class="hj-w"> · {j.word}</span>}
          </button>
        ))}
      </nav>
      {h.idWas && h.id && (
        <>
          <div class="note bad" role="alert">
            <span>
              <b>Another host answers as {h.name}.</b> That is normal after you reinstalled or replaced {h.name}. It is wrong if you did not.
            </span>
          </div>
          <Group title="Identity" foot={`Fyr still follows ${h.name} meanwhile: today it trusts a host through your tailnet login, not its key. If you do not know this host, take it off your tailnet.`}>
            <IdRow title="It was" id={h.idWas} desc={`The id Fyr first saw for ${h.name}.`} />
            <IdRow title="It is now" id={h.id} bad desc={`To check, open Fyr on ${h.name}: Settings → Hosts → ${h.name} shows its own id under About.`} end={<CopyId id={h.id} />} />
            <Row
              class="trust"
              acts
              title={`Trust ${shortID(h.id)} as ${h.name}`}
              desc={err ? undefined : `Fyr then remembers this id as ${h.name}'s and warns again only if it changes once more. It cannot be taken back here.`}
              error={err ? `Not trusted: Fyr answered “${err}”. Nothing changed.` : null}
              // a security decision with no way back on this page: it asks in place (settings-rows.tsx ConfirmButton)
              end={
                <>
                  <span class="pst needs-login">Needs you</span>
                  <ConfirmButton label={`Trust ${shortID(h.id)}`} disabled={env.readOnly || busy} onConfirm={() => void trust()} />
                </>
              }
            />
          </Group>
        </>
      )}
      {note && <div class="note hnote">{note}</div>}
      {setupNeeds > 0 && (
        <Group label="Setup">
          <Row class="hneed" onClick={() => go({ page: "setup", ...toHost })} title={setupNeeds === 1 ? "1 required setup check is not ready" : `${setupNeeds} required setup checks are not ready`} desc={`${h.name} may not run threads until it is. The checks and their fixes are on the Setup page.`} end={<span class="chev">›</span>} />
        </Group>
      )}
      {/* What the host itself holds is read-only while it is offline, as on Providers and Automation. */}
      <HostOwn online={h.online}>
        <HostUpdates h={h} d={d} hasDeploy={hasDeploy} api={api} now={now} focus={focus} go={go} />
        <HostLoadSection h={load} reading={reading} part={part} id={h.id} api={api} focus={focus} jump={jumpTo} />
      </HostOwn>
      <Section title="Providers and setup" id="more">
        <Group label="Providers and setup">
          <Row class="hprov" onClick={() => go({ page: "providers", ...toHost })} title="Providers" desc={sh.noId ? "Cannot be read until it updates" : d ? providersSummary(d.providers, blocked, !!d.provErr) || "Reading…" : "Not read yet"} end={<span class="chev">›</span>} />
          {api.setup && <Row class="hsetup" onClick={() => go({ page: "setup", ...toHost })} title="Setup checks" desc={sh.noId ? "Cannot be read until it updates" : !h.online ? "Not checked while it is offline" : setupFoldLine(setup) || "Not checked yet"} end={<span class="chev">›</span>} />}
        </Group>
      </Section>
      <HostOwn online={h.online}>
        <HostDiagnostics h={load} part={part} d={d} focus={focus} />
      </HostOwn>
      <Section title="Look" id="look">
        <HostMarkGroup h={h} api={api} part={part} />
        {shown && (
          // kept in this browser, so it works with no connection, as on This device and on a project's page
          <SetContext.Provider value={{ ...env, readOnly: false }}>
            <Group title="Show on this device" foot="Only on this device. A host you turn off leaves this device's inbox, filters, Changes, pickers and notifications; it keeps running, and your other devices still show it.">
              <Row label title={`Show ${h.name}`} desc={shown.locked ? "The last host this device shows cannot be turned off." : undefined} end={<Switch on={shown.on} label={`Show host ${h.name}`} disabled={shown.locked} onChange={shown.set} />} />
            </Group>
          </SetContext.Provider>
        )}
      </Section>
      <Section title="About" id="about" fold={{ id: `${part}:about`, open: false, force: focus === "about", summary: [h.id ? `id ${shortID(h.id)}` : "no id", route, state].filter(Boolean).join(" · ") }}>
        {!(h.idWas && h.id) && (
          <Group title="Identity">
            {h.id ? (
              <IdRow title="Id" id={h.id} desc={isSelf ? `The id of the host this device is connected to. Your other hosts show this id for ${h.name}.` : "Fyr remembers this id and tells you when another host answers under this name."} end={<CopyId id={h.id} />} />
            ) : (
              <Row title="Id" desc="None. This host runs a Fyr older than V0.124, which has no key of its own. It shows an id after its next update." />
            )}
            {!isSelf && <Row title="Trust" desc="Through your tailnet: Fyr follows it because it is a node of your Tailscale login, not because of its key." />}
          </Group>
        )}
        <Group title="Connection">
          <Row class="hstate" title={state[0].toUpperCase() + state.slice(1)} desc={under} />
          {h.url && (
            <Row
              title="Address"
              desc={
                <>
                  <span class="mono">{h.url}</span>
                  {route && ` · ${route}`}
                </>
              }
            />
          )}
          {!isSelf &&
            (h.found ? (
              <Row title="Paired" desc="Fyr follows it over its peer link, as one of your hosts. It leaves this list when it is removed." />
            ) : (
              <Row
                title="Set in the config"
                desc={
                  <>
                    It is listed under <span class="mono">hosts</span> in the config file of the host this device is connected to, and stays on this list until it is taken out there.
                  </>
                }
              />
            ))}
        </Group>
      </Section>
      {unpair && (
        <Group danger title="Remove" label="Remove">
          <Row
            class="hremove"
            title="Remove this host"
            desc="Ends its pairing for ever: its link closes, and what it signed counts for nothing."
            error={rmErr}
            end={
              // for ever, and it reaches another machine: the confirmation dialog, which names the host (settings-asks.ts)
              <button
                type="button"
                class="btn danger"
                disabled={env.readOnly}
                onClick={async () => {
                  if (!(await ask(removeHostAsk(h.name)))) return;
                  const e = await unpair();
                  setRmErr(e);
                  if (!e) go({ page: "hosts" });
                }}
              >
                Remove…
              </button>
            }
          />
        </Group>
      )}
    </>
  );
}

function HostOwn({ online, children }: { online: boolean; children: ComponentChildren }) {
  const env = useSet();
  return <SetContext.Provider value={{ ...env, readOnly: env.readOnly || !online }}>{children}</SetContext.Provider>;
}

/** V0.58: every project with its icon, folder and thread count; a row opens the project's page. Search from nine
 *  projects. V0.121: in the owner's one order, which this page sets with the one reorder control (a grip on the
 *  desktop, a Reorder mode with ↑ and ↓ on the phone); quiet projects are listed and keep their place. V0.125: the hosts have their own page. */
