// Rail and inbox list.

import type { ThreadDTO } from "./types.ts";
import { SECTIONS, type SectionKey, type Sections } from "./store.ts";
import { StateIcon, stateClass, stateLabel } from "./icons.tsx";
import { count, relTime, untilLabel } from "./format.ts";
import { prLabel } from "./thread.tsx";
import type { ConnState } from "./api.ts";

export type Theme = "system" | "light" | "dark";

interface RailProps {
  sec: Sections;
  active: SectionKey | null;
  conn: ConnState;
  mock: boolean;
  theme: Theme;
  onJump(k: SectionKey): void;
  onTheme(): void;
}

export function Rail(p: RailProps) {
  const connLabel = p.mock ? "mock" : p.conn === "live" ? "live" : p.conn === "offline" ? "offline" : "…";
  return (
    <nav class="rail" aria-label="Sections">
      <div class="brand">Fyr</div>
      {SECTIONS.map((s, i) => {
        const n = p.sec[s.key].length;
        return (
          <button
            class={`ri${p.active === s.key ? " on" : ""}${s.key === "needs" && n > 0 ? " att" : ""}`}
            title={`${s.label}: ${n} (g${i + 1})`}
            onClick={() => p.onJump(s.key)}
          >
            <StateIcon state={s.state} />
            <span class="n tnum">{count(n)}</span>
            <span class="l">{s.short}</span>
            <span class="k">g{i + 1}</span>
          </button>
        );
      })}
      <div class="foot">
        <button class="theme" onClick={p.onTheme} title={`Theme: ${p.theme} (click to change)`}>
          {p.theme === "system" ? "auto" : p.theme}
        </button>
        <span class={`conn ${p.conn}`} title={p.mock ? "Mock data" : `Connection: ${p.conn}`}>
          <i />
          {connLabel}
        </span>
      </div>
    </nav>
  );
}

export const SECTION_LIMIT: Record<SectionKey, number> = { needs: 500, working: 500, seen: 200, snoozed: 200, settled: 60 };

interface InboxProps {
  sec: Sections;
  now: number;
  selected: string | null;
  open: string | null;
  picked: Set<string>;
  expanded: Partial<Record<SectionKey, boolean>>;
  onClick(id: string): void;
  onExpand(k: SectionKey): void;
  onNew(): void;
  onAdopt(): void;
  onFocusZone(): void;
}

function needsMeta(t: ThreadDTO): string {
  const st = t.attention.state;
  const d = t.decisions?.[0];
  if (st === "asking" && d) {
    const who = d.agent ? `${d.agent}: ` : "";
    if (d.kind === "ask") return who + (d.questions?.[0]?.question ?? "");
    if (d.kind === "approval") {
      const cmd = typeof d.input?.command === "string" ? ` ${d.input.command}` : "";
      return `${who}Allow ${d.tool ?? "tool"}${cmd}`;
    }
    if (d.kind === "effect") return `${who}${d.effect?.action ?? "action"} · ${d.effect?.reason ?? ""}`;
  }
  if (st === "failed") return t.failure?.message ?? "";
  if (st === "working") return t.activity ?? "";
  return t.summary ?? "";
}

function stateWord(t: ThreadDTO): string {
  const st = t.attention.state;
  if (st === "asking") {
    const d = t.decisions?.[0];
    if (d?.kind === "ask") {
      const n = d.questions?.length ?? 1;
      return n === 1 ? "Question" : `${n} questions`;
    }
    if (d?.kind === "approval") return "Approval";
    if (d?.kind === "effect") return "Action";
  }
  return t.attention.reason || stateLabel(st);
}

function PrBadge({ t }: { t: ThreadDTO }) {
  if (!t.pr) return <span class="pr none" aria-label="no pull request" />;
  const l = prLabel(t.pr);
  return (
    <span class={`pr ${l.cls}`} title={t.pr.title || t.pr.url}>
      {l.text}
    </span>
  );
}

function NeedsRow({ t, now, sel, open, picked, onClick }: { t: ThreadDTO; now: number; sel: boolean; open: boolean; picked: boolean; onClick(): void }) {
  const st = t.attention.state;
  return (
    <div
      class={`rw${sel ? " sel" : ""}${open ? " open" : ""}${picked ? " picked" : ""}${st === "done" ? " dn" : ""}`}
      data-id={t.id}
      onClick={onClick}
      role="option"
      aria-selected={sel}
    >
      <span class="tile">
        <StateIcon state={st} />
      </span>
      <span class="t ell" title={t.title}>
        {t.title || "Untitled"}
      </span>
      <span class="age tnum">{relTime(t.activityAt ?? t.updatedAt, now)}</span>
      <span class="m ell">
        <b class={stateClass(st)}>{stateWord(t)}</b> · {t.project} · {needsMeta(t)}
      </span>
      <PrBadge t={t} />
    </div>
  );
}

function OneRow({ t, now, sel, open, picked, onClick }: { t: ThreadDTO; now: number; sel: boolean; open: boolean; picked: boolean; onClick(): void }) {
  const st = t.attention.state;
  const sub = st === "working" ? t.activity || t.project : st === "snoozed" ? `until ${untilLabel(t.view?.snoozeUntil, now)}` : t.project;
  const when = st === "working" ? relTime(t.runStartedAt, now) : relTime(t.activityAt ?? t.updatedAt, now);
  return (
    <div class={`one${sel ? " sel" : ""}${open ? " open" : ""}${picked ? " picked" : ""}`} data-id={t.id} onClick={onClick} role="option" aria-selected={sel}>
      <StateIcon state={st} />
      <span class="t ell" title={t.title}>
        {t.title || "Untitled"}
        <i>{sub}</i>
      </span>
      {st === "working" ? <span class="sb tnum">{t.subagents ? `${t.subagents} sub` : ""}</span> : t.pr ? <PrBadge t={t} /> : <span />}
      <span class="age tnum">{when}</span>
    </div>
  );
}

function sectionHint(k: SectionKey, list: ThreadDTO[], now: number): string {
  if (k === "seen" && list.length) return "⇧E settle";
  if (k === "snoozed" && list.length) {
    const next = list
      .map((t) => t.view?.snoozeUntil)
      .filter((x): x is string => !!x)
      .sort()[0];
    return next ? `next: ${untilLabel(next, now)}` : "";
  }
  if (k === "working") {
    const subs = list.reduce((a, t) => a + (t.subagents ?? 0), 0);
    return subs ? `${subs} subagents` : "";
  }
  return "";
}

export function Inbox(p: InboxProps) {
  return (
    <aside class="ib" onMouseDown={p.onFocusZone}>
      <div class="top">
        <span class="ttl">Inbox</span>
        <button class="btn sm" onClick={p.onAdopt} title="Adopt threads from T3 Code">
          Adopt from T3
        </button>
        <button class="btn sm p" onClick={p.onNew} title="New thread (c)">
          New <kbd>c</kbd>
        </button>
      </div>
      <div class="list" role="listbox" aria-label="Threads">
        {SECTIONS.map((s) => {
          const list = p.sec[s.key];
          const limit = p.expanded[s.key] ? list.length : SECTION_LIMIT[s.key];
          const shown = list.slice(0, limit);
          return (
            <div class="secw" id={`sec-${s.key}`}>
              <div class="sec">
                {s.label}
                <span class="n tnum">{count(list.length)}</span>
                <span class="h ell">{sectionHint(s.key, list, p.now)}</span>
              </div>
              {shown.map((t) =>
                s.key === "needs" ? (
                  <NeedsRow t={t} now={p.now} sel={p.selected === t.id} open={p.open === t.id} picked={p.picked.has(t.id)} onClick={() => p.onClick(t.id)} />
                ) : (
                  <OneRow t={t} now={p.now} sel={p.selected === t.id} open={p.open === t.id} picked={p.picked.has(t.id)} onClick={() => p.onClick(t.id)} />
                ),
              )}
              {list.length > shown.length && (
                <button class="more" onClick={() => p.onExpand(s.key)}>
                  Show all {list.length.toLocaleString()} <span class="tnum">({(list.length - shown.length).toLocaleString()} more)</span>
                </button>
              )}
              {list.length === 0 && <div class="empty">{s.key === "needs" ? "Nothing needs you." : "None"}</div>}
            </div>
          );
        })}
      </div>
    </aside>
  );
}
