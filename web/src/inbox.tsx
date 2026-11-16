// Rail and inbox list, as design/directions/04-rail.html draws them.

import type { ComponentChildren, Ref } from "preact";
import type { Profile, Project, Thread } from "./types.ts";
import { QuotaRail } from "./quota-ui.tsx";
import { LoadRail } from "./load-ui.tsx";
import { LINE_ICONS } from "./lucide.ts";
import { ProjectIcon, QuietGate, type QuietUnlock } from "./project-ui.tsx";
import { DESKTOP_LOOK, NeedsRowBody, OneRowBody } from "./row-ui.tsx";
import { colorVar } from "./projects.ts";
import { SECTIONS, unpinned, type Conn, type SectionKey, type Sections } from "./store.ts";
import { BrandMark, StateIcon } from "./icons.tsx";
import { count, untilLabel } from "./format.ts";
import { quietLocked, quietNote } from "./quiet.ts";

export type Theme = "system" | "light" | "dark";

interface RailProps {
  sec: Sections;
  active: SectionKey | null;
  conn: Conn;
  host: string;
  /** the bell and Install controls (desktop) */
  extra?: ComponentChildren;
  onJump(k: SectionKey): void;
  /** V0.40: show or hide the thread list */
  listOn?: boolean;
  onToggleList?(): void;
  /** V0.44: profiles, for the quota in the footer */
  profiles?: Profile[];
  /** V0.50: the gear opens Settings */
  onSettings?(): void;
  /** V0.64: open the Changes board; needs is how many changes need the owner */
  onChanges?(): void;
  changesNeeds?: number;
}

export function Rail(p: RailProps) {
  const title = p.conn === "live" ? `Connected to ${p.host}` : p.conn === "offline" ? `${p.host} is not reachable, retrying` : "Connecting…";
  return (
    <nav class="rail" aria-label="Sections">
      {/* V0.93: the rendered isometric lighthouse (128 px, shown at 32) in place of the name */}
      <div class="brand" title="Fyr">
        <BrandMark size={32} />
      </div>
      {SECTIONS.map((s, i) => {
        const n = p.sec[s.key].length;
        return (
          <button class={`ri${s.key === "needs" && n > 0 ? " att" : ""}${p.active === s.key && n > 0 ? " on" : ""}`} title={n ? `${s.label}: ${n} (g${i + 1})` : `${s.label}: none`} disabled={n === 0} onClick={() => p.onJump(s.key)}>
            <StateIcon state={s.state} />
            <span class="n">{count(n)}</span>
            <span class="l">{s.short}</span>
            <span class="k">g{i + 1}</span>
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
      {p.profiles && <LoadRail />}
      {p.profiles && <QuotaRail profiles={p.profiles} />}
      {p.extra && <div class="railx">{p.extra}</div>}
      {p.onChanges && (
        <button class="ri rchg" title="Changes across threads" aria-label={`Changes${p.changesNeeds ? `, ${p.changesNeeds} need you` : ""}`} onClick={p.onChanges}>
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" dangerouslySetInnerHTML={{ __html: LINE_ICONS["layout-dashboard"] }} />
          <span class="l">Changes</span>
          {!!p.changesNeeds && <span class="rb">{p.changesNeeds}</span>}
        </button>
      )}
      {p.onSettings && (
        <button class="ri rgear" title="Settings (providers, notifications)" aria-label="Settings" onClick={p.onSettings}>
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" dangerouslySetInnerHTML={{ __html: LINE_ICONS.settings }} />
          <span class="l">Settings</span>
        </button>
      )}
      <div class={`foot ${p.conn}`} title={title}>
        <i />
        <span class="hn">{p.host}</span>
      </div>
    </nav>
  );
}

export const SECTION_LIMIT: Record<SectionKey, number> = { needs: 500, working: 500, seen: 200, snoozed: 200, settled: 60 };
/** Sections that show only their header line until opened (the design draws Snoozed and Settled this way). */
export const COLLAPSED_BY_DEFAULT: SectionKey[] = ["snoozed", "settled"];

export function isOpen(k: SectionKey, open: Partial<Record<SectionKey, boolean>>): boolean {
  return open[k] ?? !COLLAPSED_BY_DEFAULT.includes(k);
}

interface RowProps {
  t: Thread;
  now: number;
  sel: boolean;
  picked: boolean;
  onClick(): void;
}

function rowAttrs({ t, sel, onClick }: RowProps) {
  return { "data-id": t.id, onClick, role: "option", "aria-selected": sel };
}

function NeedsRow(p: RowProps) {
  return <NeedsRowBody t={p.t} now={p.now} look={DESKTOP_LOOK} extra={`${p.sel ? " sel" : ""}${p.picked ? " picked" : ""}`} attrs={rowAttrs(p)} />;
}

function OneRow(p: RowProps) {
  return <OneRowBody t={p.t} now={p.now} look={DESKTOP_LOOK} extra={`${p.sel ? " sel" : ""}${p.picked ? " picked" : ""}`} attrs={rowAttrs(p)} />;
}

function sectionHint(k: SectionKey, list: Thread[], now: number): string {
  if (k === "working") {
    const waiting = list.filter((t) => t.attention.state === "waiting").length;
    return waiting ? `${waiting} waiting` : "";
  }
  if (k === "seen") return list.length ? "⇧E settle" : "";
  if (k === "snoozed") {
    const next = list
      .map((t) => t.view?.snoozeUntil)
      .filter((x): x is string => !!x)
      .sort()[0];
    return next ? `next: ${untilLabel(next, now)}` : "none";
  }
  if (k === "settled") return "last 24 h";
  return "";
}

const PLUS = (
  <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true">
    <path d="M8 3v10M3 8h10" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
  </svg>
);
const ADOPT = (
  <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true">
    <path d="M8 2.5v7.5M4.8 6.8 8 10l3.2-3.2M3 12.8h10" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
  </svg>
);

interface InboxProps {
  /** V0.48: sections that close by themselves, with their deadline (ms) */
  autoHideAt?: Partial<Record<SectionKey, number>>;
  onSectionActivity?(k: SectionKey): void;
  sec: Sections;
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
  onExpand(k: SectionKey): void;
  onNew(): void;
  onAdopt(): void;
  onFocusZone(): void;
  /** the bell and Install controls (phone layout, where the rail is hidden) */
  extra?: ComponentChildren;
  /** the project filter (V0.23) */
  projects: Project[];
  projectFilter: string | null;
  onProjectFilter(id: string | null): void;
  /** V0.114: the one unlock of quiet projects, and how many search hits it hides */
  unlock: QuietUnlock;
  quietHits: number;
}

export function Inbox(p: InboxProps) {
  return (
    <aside class="ib" onMouseDown={p.onFocusZone}>
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
          />
          <kbd>⌘K</kbd>
        </label>
        {p.extra && <span class="phone-only">{p.extra}</span>}
        <button class="ibtn" onClick={p.onAdopt} title="Adopt from T3" aria-label="Adopt from T3">
          {ADOPT}
        </button>
        <button class="ibtn" onClick={p.onNew} title="New thread (c)" aria-label="New thread">
          {PLUS}
        </button>
      </div>
      <ProjectFilterRow projects={p.projects} selected={p.projectFilter} onPick={p.onProjectFilter} now={p.now} unlock={p.unlock} />
      {p.quietHits > 0 && <QuietGate now={p.now} unlock={p.unlock} what="search" />}
      <div class="list" role="listbox" aria-label="Threads">
        {p.sec.pinned.length > 0 && (
          // V0.104: the pinned threads, above every section and always open; each keeps its section's row shape.
          <div id="sec-pinned">
            <div class="sec">
              Pinned
              <span class="n">{count(p.sec.pinned.length)}</span>
            </div>
            {p.sec.pinned.map((t) =>
              t.attention.section === "needs" ? (
                <NeedsRow t={t} now={p.now} sel={p.selected === t.id} picked={p.picked.has(t.id)} onClick={() => p.onClick(t.id)} />
              ) : (
                <OneRow t={t} now={p.now} sel={p.selected === t.id} picked={p.picked.has(t.id)} onClick={() => p.onClick(t.id)} />
              ),
            )}
          </div>
        )}
        {SECTIONS.map((s) => {
          const list = unpinned(p.sec[s.key]);
          const open = isOpen(s.key, p.open) && list.length > 0;
          const limit = p.expanded[s.key] ? list.length : SECTION_LIMIT[s.key];
          const shown = open ? list.slice(0, limit) : [];
          const hint = sectionHint(s.key, list, p.now);
          return (
            <div id={`sec-${s.key}`} onPointerMove={open && p.autoHideAt?.[s.key] ? () => p.onSectionActivity?.(s.key) : undefined} onKeyDown={open && p.autoHideAt?.[s.key] ? () => p.onSectionActivity?.(s.key) : undefined}>
              <div class={`sec${list.length ? " tog" : ""}`} onClick={() => list.length && p.onToggle(s.key)} title={list.length ? (open ? "Collapse" : "Expand") : undefined}>
                {s.label}
                <span class="n">{count(list.length)}</span>
                {open && p.autoHideAt?.[s.key] ? (
                  // V0.48: a small clock runs down to the auto-close; it restarts on use.
                  <span key={p.autoHideAt[s.key]} class="ahc" style={{ "--ah": `${Math.max(0, (p.autoHideAt[s.key]! - Date.now()) / 1000)}s` }} title="Closes by itself; select it in the rail to keep it open" aria-hidden="true" />
                ) : null}
                {hint && <span class="h ell">{hint}</span>}
              </div>
              {shown.map((t) =>
                s.key === "needs" ? (
                  <NeedsRow t={t} now={p.now} sel={p.selected === t.id} picked={p.picked.has(t.id)} onClick={() => p.onClick(t.id)} />
                ) : (
                  <OneRow t={t} now={p.now} sel={p.selected === t.id} picked={p.picked.has(t.id)} onClick={() => p.onClick(t.id)} />
                ),
              )}
              {open && list.length > shown.length && (
                <button class="more" onClick={() => p.onExpand(s.key)}>
                  Show all {list.length.toLocaleString()}
                </button>
              )}
            </div>
          );
        })}
      </div>
    </aside>
  );
}

/** Desktop and phone (V0.100): a compact row of projects under the search field; one click filters, the × clears.
 *  V0.114: picking a quiet project shows the gate under the row, not its threads, until the 30 s hold. */
export function ProjectFilterRow({ projects, selected, onPick, now, unlock }: { projects: Project[]; selected: string | null; onPick(id: string | null): void; now: number; unlock: QuietUnlock }) {
  // V0.121: in the owner's order (the store keeps it), on the desktop and the phone alike.
  const list = projects.filter((p) => (p.threads ?? 0) > 0 || p.id === selected);
  if (!list.length) return null;
  const picked = projects.find((p) => p.id === selected);
  return (
    <>
    <div class="pfrow" role="toolbar" aria-label="Filter by project">
      {selected && (
        <button class="pfx" onClick={() => onPick(null)} title="Show every project" aria-label="Clear the project filter">
          ×
        </button>
      )}
      {list.map((p) => {
        // V0.46: a project in its quiet hours says so. V0.114: picking it here opens it only after the hold.
        const note = quietNote(p, now);
        return (
          <button class={`pfc${p.id === selected ? " on" : ""}${note ? " quiet" : ""}`} aria-pressed={p.id === selected} style={{ "--pc": colorVar(p.color, p.name) }} title={`${p.name}: ${p.threads ?? 0} threads${note ? ` · ${note}, hidden from the inbox` : ""}`} onClick={() => onPick(p.id === selected ? null : p.id ?? null)}>
            <ProjectIcon project={p} name={p.name} size={16} />
            <span class="ell">{p.name}</span>
            {note && p.id === selected && <span class="pfq">{note}</span>}
          </button>
        );
      })}
    </div>
    {picked && quietLocked(picked, now, unlock.showUntil) && <QuietGate project={picked} now={now} unlock={unlock} what="threads" />}
    </>
  );
}
