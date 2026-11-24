// Rail and inbox list, as the rail direction (decision 0006) draws them.

import type { HostProfiles } from "./hosts.ts";
import { Fragment, type ComponentChildren, type Ref } from "preact";
import type { Thread } from "./types.ts";
import type { FilterSel, HostChip, ProjectChip } from "./filters.ts";
import { LoadRail } from "./load-ui.tsx";
import { SlotQueueSection, useSlotQueue } from "./slot-queue-ui.tsx";
import { InvariantCards } from "./invariants-ui.tsx";
import { LINE_ICONS } from "./lucide.ts";
import { SettingsGear } from "./settings-entry.tsx";
import { ProjectIcon } from "./project-ui.tsx";
import { HostMark } from "./host-mark-ui.tsx";
import { DESKTOP_LOOK, HideClock, NeedsGroupHead, NeedsRowBody, OneRowBody, PIN_ICON, SectionTitle, hideUse, sectionHead } from "./row-ui.tsx";
import { needsHeads } from "./rows.ts";
import { AiSearchPanel, useAiSearch } from "./ai-search-ui.tsx";
import { drawnRows, type Guest, type GuestWhy } from "./selection.ts";
import { colorVar } from "./projects.ts";
import { SECTIONS, connLabel, connWords, isOpen, mergedToSettle, unpinned, type Conn, type SectionKey, type Sections } from "./store.ts";
import { BrandMark, StateIcon } from "./icons.tsx";
import { count } from "./format.ts";
import { snoozedHint } from "./snooze.ts";
import { quietLocked, quietNote } from "./quiet.ts";
import { type QuietUnlock, QuietGate } from "./quiet-ui.tsx";

export type Theme = "system" | "light" | "dark";

interface RailProps {
  sec: Sections;
  active: SectionKey | null;
  conn: Conn;
  /** the bell and Install controls (desktop) */
  extra?: ComponentChildren;
  onJump(k: SectionKey): void;
  /** V0.40: show or hide the thread list */
  listOn?: boolean;
  onToggleList?(): void;
  /** V0.44: each host's profiles (slice 2b); the rail's middle shows while they are given */
  profiles?: HostProfiles;
  /** V0.50: the gear opens Settings */
  onSettings?(): void;
  /** V0.64: open the Changes board; needs is how many changes need the owner */
  onChanges?(): void;
  changesNeeds?: number;
}

export function Rail(p: RailProps) {
  // Decision 0031 2.6: the connection is this browser's link to Fyr, the same words through every front; which front it
  // is goes in Settings → This device only.
  const title = connWords(p.conn);
  return (
    <nav class="rail" aria-label="Sections">
      {/* V0.93: the rendered isometric lighthouse (128 px, shown at 32) in place of the name */}
      <div class="brand" title="Fyr">
        <BrandMark size={32} />
      </div>
      {SECTIONS.map((s) => {
        const n = p.sec[s.key].length;
        if (s.transient && n === 0) return null; // V0.137: Waiting for a slot only while threads wait
        const g = s.g ? ` (g${s.g})` : "";
        return (
          <button class={`ri${s.key === "needs" && n > 0 ? " att" : ""}${p.active === s.key && n > 0 ? " on" : ""}`} data-sec={s.key} title={n ? `${s.label}: ${n}${g}` : `${s.label}: none`} disabled={n === 0} onClick={() => p.onJump(s.key)}>
            <StateIcon state={s.state} />
            <span class="n">{count(n)}</span>
            <span class="l">{s.short}</span>
            {s.g && <span class="k">g{s.g}</span>}
          </button>
        );
      })}
      {p.onToggleList && (
        <button class={`ri lt${p.listOn ? " on" : ""}`} aria-pressed={p.listOn} title={p.listOn ? "Hide the thread list (\\)" : "Show the thread list (\\)"} onClick={p.onToggleList}>
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <rect x="3" y="4" width="18" height="16" rx="2" />
            <path d="M9 4v16" />
          </svg>
          <span class="l">List</span>
          <span class="k">\</span>
        </button>
      )}
      {/* V0.141: the load is the rail's middle, its own scroll area (V0.quota-in-thread: the quota moved to the thread's header); the sections above and the bell,
          Changes and Settings below keep their size, so the rail never runs past the window. */}
      {p.profiles && (
        <div class="rmid">
          <LoadRail />
        </div>
      )}
      {p.extra && <div class="railx">{p.extra}</div>}
      {p.onChanges && (
        <button class="ri rchg" title="Changes across threads" aria-label={`Changes${p.changesNeeds ? `, ${p.changesNeeds} need you` : ""}`} onClick={p.onChanges}>
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" dangerouslySetInnerHTML={{ __html: LINE_ICONS["layout-dashboard"] }} />
          <span class="l">Changes</span>
          {!!p.changesNeeds && <span class="rb">{p.changesNeeds}</span>}
        </button>
      )}
      {p.onSettings && <SettingsGear onClick={p.onSettings} /> /* V0.settings-ia: with the one count, as Changes has */}
      <div class={`foot ${p.conn}`} title={title}>
        <i />
        <span class="hn">{connLabel(p.conn)}</span>
      </div>
    </nav>
  );
}

export const SECTION_LIMIT: Record<SectionKey, number> = { needs: 500, working: 500, slots: 500, unread: 200, seen: 200, snoozed: 200, settled: 60 };
// Moved to store.ts (V0.134: the filters read them too); re-exported for the callers here.
export { COLLAPSED_BY_DEFAULT, isOpen } from "./store.ts";

interface RowProps {
  t: Thread;
  now: number;
  sel: boolean;
  picked: boolean;
  guest?: GuestWhy;
  onClick(): void;
}

function rowAttrs({ t, sel, onClick }: RowProps) {
  // V0.list-motion: the row's key and the section it is drawn in, for the list's motion (still-list-ui.ts)
  return { "data-id": t.id, "data-mk": `row:${t.id}`, "data-sec": t.attention.section, onClick, role: "option", "aria-selected": sel };
}

function NeedsRow(p: RowProps) {
  return <NeedsRowBody t={p.t} now={p.now} look={DESKTOP_LOOK} guest={p.guest} extra={`${p.sel ? " sel" : ""}${p.picked ? " picked" : ""}`} attrs={rowAttrs(p)} />;
}

function OneRow(p: RowProps) {
  return <OneRowBody t={p.t} now={p.now} look={DESKTOP_LOOK} guest={p.guest} extra={`${p.sel ? " sel" : ""}${p.picked ? " picked" : ""}`} attrs={rowAttrs(p)} />;
}

function sectionHint(k: SectionKey, list: Thread[], now: number): string {
  if (k === "working") {
    const waiting = list.filter((t) => t.attention.state === "waiting").length;
    return waiting ? `${waiting} waiting` : "";
  }
  if (k === "seen") return list.length ? "⇧E settle" : "";
  if (k === "snoozed") return snoozedHint(list, now);
  if (k === "settled") return "last 24 h";
  return "";
}

const PLUS = (
  <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true">
    <path d="M8 3v10M3 8h10" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
  </svg>
);

interface InboxProps {
  /** V0.184: the handle on the list's edge (the desktop's; see app.tsx) */
  resizer?: ComponentChildren;
  /** V0.48, V0.196: sections that close by themselves, with their deadline (ms) */
  autoHideAt?: Partial<Record<SectionKey, number>>;
  onSectionActivity?(k: SectionKey): void;
  /** what the filters let through: the counts */
  sec: Sections;
  /** V0.192: the rows: `sec`, with the current thread in its place when the filters hide it */
  rows: Sections;
  /** V0.192: the row that is drawn only because it is the current thread */
  guest: Guest | null;
  now: number;
  selected: string | null;
  picked: Set<string>;
  open: Partial<Record<SectionKey, boolean>>;
  expanded: Partial<Record<SectionKey, boolean>>;
  query: string;
  searchRef: Ref<HTMLInputElement>;
  onQuery(q: string): void;
  onClick(id: string): void;
  onToggle(k: SectionKey): void;
  /** The Seen header's "Settle merged" (the phone has the same button): these ids, merged and settleable. ⇧E settles the
   *  whole section instead, which is the keyboard's sweep; the button is the narrower, pointer-first choice. */
  onSettleMerged?(ids: string[]): void;
  onExpand(k: SectionKey): void;
  onNew(): void;
  onFocusZone(): void;
  /** V0.status-home: a click on the list's empty space (no row, no header) closes the open thread */
  onBackground?(): void;
  /** the bell and Install controls (phone layout, where the rail is hidden) */
  extra?: ComponentChildren;
  /** the host and project filters (V0.23, V0.134) */
  filter: FilterView;
  /** V0.114: the one unlock of quiet projects, and how many search hits it hides */
  unlock: QuietUnlock;
  quietHits: number;
  /** V0.ai-search: what AI search may read, in list order (the same reach as the typed search) */
  searchable: Thread[];
  /** Decision 0037: optional "Set up · X of 6" card atop the inbox */
  setup?: ComponentChildren;
}

export function Inbox(p: InboxProps) {
  const queue = useSlotQueue();
  const ai = useAiSearch(p.query, p.searchable);
  const why = (t: Thread) => (p.guest?.id === t.id ? p.guest.why : undefined);
  return (
    <aside class="ib" onMouseDown={p.onFocusZone}>
      {p.resizer}
      <div class="top">
        <label class="search">
          <input
            ref={p.searchRef}
            type="text"
            value={p.query}
            placeholder="Search threads"
            aria-label="Search threads"
            spellcheck={false}
            onInput={(e) => p.onQuery((e.target as HTMLInputElement).value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") ai.run(); // V0.ai-search
            }}
          />
          <kbd>⌘K</kbd>
        </label>
        {p.extra && <span class="phone-only">{p.extra}</span>}
        <button class="ibtn" onClick={p.onNew} title="New thread (c)" aria-label="New thread">
          {PLUS}
        </button>
      </div>
      <FilterRow f={p.filter} now={p.now} unlock={p.unlock} />
      {p.quietHits > 0 && <QuietGate now={p.now} unlock={p.unlock} what="search" />}
      <div class="list" role="listbox" aria-label="Threads" onClick={(e) => e.target === e.currentTarget && p.onBackground?.()}>
        {p.setup}
        <AiSearchPanel ai={ai} onOpen={p.onClick} />
        {!p.query.trim() && <FilterEmpty f={p.filter} cls="fempty" />}
        {p.rows.pinned.length > 0 && (
          // V0.104: the pinned threads, above every section and always open; each keeps its section's row shape.
          <div id="sec-pinned">
            <div class="sec" data-mk="sec:pinned">
              <SectionTitle label="Pinned" icon={PIN_ICON} n={p.sec.pinned.length} shown={count(p.sec.pinned.length)} mk="pinned" />
            </div>
            {p.rows.pinned.map((t) =>
              t.attention.section === "needs" ? (
                <NeedsRow t={t} now={p.now} sel={p.selected === t.id} picked={p.picked.has(t.id)} guest={why(t)} onClick={() => p.onClick(t.id)} />
              ) : (
                <OneRow t={t} now={p.now} sel={p.selected === t.id} picked={p.picked.has(t.id)} guest={why(t)} onClick={() => p.onClick(t.id)} />
              ),
            )}
          </div>
        )}
        {SECTIONS.map((s) => {
          if (s.key === "slots")
            return (
              <SlotQueueSection
                open={isOpen("slots", p.open)}
                onToggle={() => p.onToggle("slots")}
                guest={p.guest?.id ?? null}
                phone={false}
                queue={queue}
                threads={p.rows.slots}
                n={p.sec.slots.length}
                row={(t, a) => (
                  <OneRowBody key={t.id} t={t} now={p.now} look={DESKTOP_LOOK} lead={a.lead} guest={why(t)} extra={`${p.selected === t.id ? " sel" : ""}${p.picked.has(t.id) ? " picked" : ""}${a.attrs.class ? " " + a.attrs.class : ""}`} attrs={{ ...rowAttrs({ t, now: p.now, sel: p.selected === t.id, picked: p.picked.has(t.id), onClick: () => p.onClick(t.id) }), ref: a.attrs.ref }} />
                )}
              />
            );
          const list = unpinned(p.sec[s.key]);
          const rows = unpinned(p.rows[s.key]); // V0.192: `list` counts, `rows` draws
          const open = isOpen(s.key, p.open) && rows.length > 0;
          const limit = p.expanded[s.key] ? rows.length : SECTION_LIMIT[s.key];
          const shown = drawnRows(rows, open, limit, p.guest);
          const hint = sectionHint(s.key, list, p.now);
          const settleMerged = s.key === "seen" ? mergedToSettle(p.sec.seen) : { ids: [], any: false };
          const heads = s.key === "needs" ? needsHeads(shown) : {};
          const hideAt = open ? p.autoHideAt?.[s.key] : undefined;
          return (
            <div id={`sec-${s.key}`} {...hideUse(hideAt !== undefined, () => p.onSectionActivity?.(s.key))}>
              <div class={`sec${list.length ? " tog" : ""}`} data-mk={`sec:${s.key}`} {...sectionHead({ n: list.length, open, toggle: () => p.onToggle(s.key) })} title={list.length ? (open ? "Collapse" : "Expand") : undefined}>
                <SectionTitle label={s.label} icon={<StateIcon state={s.state} size={14} />} n={list.length} shown={count(list.length)} open={open} attention={s.key === "needs"} mk={s.key} />
                {hideAt !== undefined && <HideClock at={hideAt} />}
                {hint && <span class="h ell">{hint}</span>}
                {s.key === "seen" && settleMerged.any && p.onSettleMerged && (
                  <button
                    class="sbtn"
                    title="Settle the threads whose PR has merged"
                    onClick={(e) => {
                      e.stopPropagation(); // the header toggles its section
                      p.onSettleMerged?.(settleMerged.ids);
                    }}
                  >
                    Settle merged
                  </button>
                )}
              </div>
              {s.key === "needs" && <InvariantCards phone={false} now={p.now} onOpen={p.onClick} />}
              {shown.map((t) =>
                s.key === "needs" ? (
                  <Fragment key={t.id}>
                    {heads[t.id] && <NeedsGroupHead label={heads[t.id]} />}
                    <NeedsRow t={t} now={p.now} sel={p.selected === t.id} picked={p.picked.has(t.id)} guest={why(t)} onClick={() => p.onClick(t.id)} />
                  </Fragment>
                ) : (
                  <OneRow t={t} now={p.now} sel={p.selected === t.id} picked={p.picked.has(t.id)} guest={why(t)} onClick={() => p.onClick(t.id)} />
                ),
              )}
              {open && rows.length > shown.length && (
                <button class="more" data-mk={`more:${s.key}`} onClick={() => p.onExpand(s.key)}>
                  Show all {rows.length.toLocaleString()}
                </button>
              )}
            </div>
          );
        })}
      </div>
    </aside>
  );
}

/** The filters as the app computed them (V0.134, V0.169), shared by the desktop and the phone. */
export interface FilterView {
  hosts: HostChip[];
  projects: ProjectChip[];
  sel: FilterSel;
  /** V0.169: turn a host off, or back on; V0.204: on the only host on, turn every host back on */
  /** V0.client-refs: by the chip's key (the host's install id) */
  toggleHost(key: string): void;
  setProject(id: string | null): void;
  /** every host on and no project picked */
  clear(): void;
  /** filterEmpty: null, or the second line of the empty state */
  empty: string | null;
}

/** Desktop and phone (V0.100, V0.134, V0.169, V0.204): two rows under the search field. Row 1 is a small pill per host
 *  (its mark, as on every thread row, and its threads in view; the name is in the tooltip and the label); a click turns
 *  the host off (mark grey, count struck through) or back on, and a click on the only host on shows every host again. Row 2 is the project chips, counted
 *  over the hosts that are on: a click picks a project, a click on the picked one or the × clears it. Each row shows
 *  only when it has a choice. V0.114: picking a quiet project shows the gate under the rows, not its threads, until the
 *  30 s hold. */
export function FilterRow({ f, now, unlock }: { f: FilterView; now: number; unlock: QuietUnlock }) {
  if (!f.hosts.length && !f.projects.length) return null;
  const picked = f.projects.find((c) => c.selected)?.project;
  const n = (x: number) => `${x.toLocaleString()} thread${x === 1 ? "" : "s"} in view`;
  return (
    <>
    <div class="frows">
    {f.hosts.length > 0 && (
      <div class="hfrow" role="group" aria-label="Hosts">
        {f.hosts.map((h) => {
          // V0.170: the host's mark, as everywhere (an offline host's has a ring at its corner).
          // V0.hover-cards: one sentence per fact, not a chain of "·"
          const state = !h.info ? "is not in the host list" : h.info.idWas ? "has a new id" : h.info.online ? "is online" : "is offline";
          const tip = `${h.name} ${state}, ${n(h.n)}. ${h.on ? (h.last ? "It is the only host shown; click to show every host." : "Click to hide its threads.") : "Hidden; click to show its threads."}`;
          return (
            <button class={`hft${h.on ? "" : " off"}`} aria-pressed={h.on} title={tip} aria-label={`${h.name}, ${h.on ? "shown" : "hidden"}, ${n(h.n)}`} onClick={() => f.toggleHost(h.key)}>
              <HostMark name={h.name} size={16} />
              <span class="hn">{h.n.toLocaleString()}</span>
            </button>
          );
        })}
      </div>
    )}
    {f.projects.length > 0 && (
      <div class="pfrow" role="toolbar" aria-label="Filter by project">
        {f.sel.project && (
          <button class="pfx" onClick={() => f.setProject(null)} title="Show every project" aria-label="Clear the project filter">
            ×
          </button>
        )}
        {f.projects.map(({ project: p, n: k, selected }) => {
          // V0.46: a project in its quiet hours says so. V0.114: picking it here opens it only after the hold.
          const note = quietNote(p, now);
          return (
            <button class={`pfc${selected ? " on" : ""}${note ? " quiet" : ""}`} aria-pressed={selected} style={{ "--pc": colorVar(p.color, p.name) }} title={`${p.name}: ${n(k)}${note ? ` · ${note}, hidden from the inbox` : ""}`} onClick={() => f.setProject(selected ? null : p.id ?? null)}>
              <ProjectIcon project={p} name={p.name} size={16} />
              <span class="ell">{p.name}</span>
              {note && selected && <span class="pfq">{note}</span>}
            </button>
          );
        })}
      </div>
    )}
    </div>
    {picked && quietLocked(picked, now, unlock.showUntil) && <QuietGate project={picked} now={now} unlock={unlock} what="threads" />}
    </>
  );
}

/** V0.134, V0.169: the list when a filter is on and none of its threads is in view (its last one left, or is in a
 *  folded section): what is picked or hidden, how many wait in folded sections, and Clear filter (every host on, no
 *  project). */
export function FilterEmpty({ f, cls }: { f: FilterView; cls: string }) {
  if (f.empty === null) return null;
  // V0.169: "for fyr", "with server hidden", "for fyr with server hidden".
  const project = f.projects.find((c) => c.selected)?.project.name;
  const off = f.hosts.filter((h) => !h.on).map((h) => h.short);
  const what = [project ? `for ${project}` : "", off.length ? `with ${off.join(", ")} hidden` : ""].filter(Boolean).join(" ");
  return (
    <div class={cls} role="status" data-mk="empty">
      <span class="fe1">No threads in view {what || "for this filter"}.</span>
      {f.empty && <span class="fe2">{f.empty}</span>}
      <button class="btn fe3" onClick={f.clear}>
        Clear filter
      </button>
    </div>
  );
}
