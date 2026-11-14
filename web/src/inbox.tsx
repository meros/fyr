// Rail and inbox list, as design/directions/04-rail.html draws them.

import type { ComponentChildren, Ref } from "preact";
import type { ThreadDTO } from "./types.ts";
import { SECTIONS, type SectionKey, type Sections } from "./store.ts";
import { StateIcon, stateClass, stateLabel } from "./icons.tsx";
import { count, relTime, untilLabel, whenLabel } from "./format.ts";
import { prLabel } from "./thread.tsx";
import type { ConnState } from "./api.ts";

export type Theme = "system" | "light" | "dark";

interface RailProps {
  sec: Sections;
  active: SectionKey | null;
  conn: ConnState;
  host: string;
  /** the bell and Install controls (desktop) */
  extra?: ComponentChildren;
  onJump(k: SectionKey): void;
}

export function Rail(p: RailProps) {
  const title = p.conn === "live" ? `Connected to ${p.host}` : p.conn === "offline" ? `${p.host} is not reachable, retrying` : "Connecting…";
  return (
    <nav class="rail" aria-label="Sections">
      <div class="brand">Fyr</div>
      {SECTIONS.map((s, i) => {
        const n = p.sec[s.key].length;
        return (
          <button class={`ri${s.key === "needs" && n > 0 ? " att" : ""}${p.active === s.key ? " on" : ""}`} title={`${s.label}: ${n} (g${i + 1})`} onClick={() => p.onJump(s.key)}>
            <StateIcon state={s.state} />
            <span class="n">{count(n)}</span>
            <span class="l">{s.short}</span>
            <span class="k">g{i + 1}</span>
          </button>
        );
      })}
      {p.extra && <div class="railx">{p.extra}</div>}
      <div class={`foot ${p.conn}`} title={title}>
        <i />
        {p.host}
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

function needsMeta(t: ThreadDTO): string {
  const st = t.attention.state;
  const d = t.decisions?.[0];
  if (st === "asking" && d) {
    // The asker is named on the card (U25); the row keeps the question, as the design draws it.
    const who = "";
    if (d.kind === "ask") return d.questions?.[0]?.question ?? "";
    if (d.kind === "approval") {
      const cmd = typeof d.input?.command === "string" ? ` ${d.input.command}` : "";
      return `${who}Allow ${d.tool ?? "tool"}${cmd}`;
    }
    if (d.kind === "effect") return `${who}${d.effect?.action ?? "action"} · ${d.effect?.reason ?? ""}`;
  }
  if (st === "failed") return `${t.failure?.message ?? ""} · r to retry`;
  if (st === "working") return t.activity ?? "";
  return t.summary ?? "";
}

function stateWord(t: ThreadDTO): string {
  const st = t.attention.state;
  if (st === "asking") {
    const d = t.decisions?.[0];
    if (d?.kind === "ask") {
      const n = d.questions?.length ?? 1;
      return n === 1 ? "1 question" : `${n} questions`;
    }
    if (d?.kind === "approval") return "Approval";
    if (d?.kind === "effect") return "Action";
  }
  return t.attention.reason || stateLabel(st);
}

function PrBadge({ t, short }: { t: ThreadDTO; short?: boolean }) {
  if (!t.pr) return <span class="pr none" aria-label="no pull request" />;
  const l = prLabel(t.pr, short);
  return (
    <span class={`pr ${l.cls}`} title={t.pr.title || t.pr.url}>
      {l.text}
    </span>
  );
}

interface RowProps {
  t: ThreadDTO;
  now: number;
  sel: boolean;
  picked: boolean;
  onClick(): void;
}

function NeedsRow({ t, now, sel, picked, onClick }: RowProps) {
  const st = t.attention.state;
  return (
    <div class={`rw${sel ? " sel" : ""}${picked ? " picked" : ""}${st === "done" ? " dn" : ""}`} data-id={t.id} onClick={onClick} role="option" aria-selected={sel}>
      <span class="tile">
        <StateIcon state={st} />
      </span>
      <span class="t ell" title={t.title}>
        {t.title || "Untitled"}
      </span>
      <span class="age">{relTime(t.activityAt ?? t.updatedAt, now)}</span>
      <span class="m ell">
        <b class={stateClass(st)}>{stateWord(t)}</b> · {t.project} · {needsMeta(t)}
      </span>
      <PrBadge t={t} />
    </div>
  );
}

function OneRow({ t, now, sel, picked, onClick }: RowProps) {
  const st = t.attention.state;
  const working = t.attention.section === "working";
  const sub = working ? t.activity || t.project : st === "snoozed" ? `until ${untilLabel(t.view?.snoozeUntil, now)}` : t.project;
  const when = working ? relTime(t.runStartedAt, now) : whenLabel(t.activityAt ?? t.updatedAt, now);
  return (
    <div class={`one${sel ? " sel" : ""}${picked ? " picked" : ""}`} data-id={t.id} onClick={onClick} role="option" aria-selected={sel}>
      <StateIcon state={st} />
      <span class="t ell" title={t.title}>
        {t.title || "Untitled"}
        <i>{sub}</i>
      </span>
      {working ? <span class="sb">{t.subagents ?? 0} sub</span> : t.pr ? <PrBadge t={t} short /> : <span />}
      <span class="age">{when}</span>
    </div>
  );
}

function sectionHint(k: SectionKey, list: ThreadDTO[], now: number): string {
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
      <div class="list" role="listbox" aria-label="Threads">
        {SECTIONS.map((s) => {
          const list = p.sec[s.key];
          const open = isOpen(s.key, p.open) && list.length > 0;
          const limit = p.expanded[s.key] ? list.length : SECTION_LIMIT[s.key];
          const shown = open ? list.slice(0, limit) : [];
          const hint = sectionHint(s.key, list, p.now);
          return (
            <div id={`sec-${s.key}`}>
              <div class={`sec${list.length ? " tog" : ""}`} onClick={() => list.length && p.onToggle(s.key)} title={list.length ? (open ? "Collapse" : "Expand") : undefined}>
                {s.label}
                <span class="n">{count(list.length)}</span>
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
