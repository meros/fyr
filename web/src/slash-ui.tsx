// SPEC 0.5 V0.79: the `/` list of the composer, desktop, phone and New thread. The hook fetches the agent's
// commands when the owner types `/` at the start (a thread's own list, else the newest of its provider and
// folder), and the list renders above the field. The keys and the filter are slash.ts's.

import { useEffect, useRef, useState } from "preact/hooks";
import { useApi } from "./api-context.ts";
import type { Api, CommandSource } from "./api.ts";
import { argHint, matchCommands, pickText, slashKey, slashOpen, slashQuery, type SlashCommand } from "./slash.ts";

/** A list stays fresh this long; a session start can change it, so an older one is asked for again. */
const FRESH_MS = 30_000;

const cache = new Map<string, { at: number; list: SlashCommand[] }>();
const inFlight = new Map<string, Promise<SlashCommand[]>>();

function keyOf(s: CommandSource): string {
  return "threadId" in s ? `t\u0000${s.threadId}` : `f\u0000${s.host ?? ""}\u0000${s.profile}\u0000${s.cwd}`;
}

function load(api: Api, s: CommandSource): Promise<SlashCommand[]> {
  const k = keyOf(s);
  const hit = cache.get(k);
  if (hit && Date.now() - hit.at < FRESH_MS) return Promise.resolve(hit.list);
  let p = inFlight.get(k);
  if (!p) {
    p = api
      .commands(s)
      .then((r) => {
        if (!r.ok) return hit?.list ?? [];
        cache.set(k, { at: Date.now(), list: r.data });
        return r.data;
      })
      .finally(() => inFlight.delete(k));
    inFlight.set(k, p);
  }
  return p;
}

interface Slash {
  open: boolean;
  items: SlashCommand[];
  index: number;
  /** the picked command and its argument hint while nothing follows the name */
  hint: { name: string; hint: string } | null;
  /** The field's keydown: true when the list took the key (the caller then does nothing else with it). */
  onKeyDown(e: KeyboardEvent): boolean;
  pick(c: SlashCommand): void;
  hover(i: number): void;
}

/** The `/` list for a field; `source` null turns it off (an answer field, a form without a folder). */
export function useSlash(source: CommandSource | null, text: string, setText: (t: string) => void, field?: { current: HTMLTextAreaElement | null }): Slash {
  const api = useApi();
  const key = source ? keyOf(source) : "";
  const [list, setList] = useState<SlashCommand[]>(() => (key ? (cache.get(key)?.list ?? []) : []));
  const [index, setIndex] = useState(0);
  const [dismissedAt, setDismissedAt] = useState<string | null>(null);
  const srcRef = useRef(source);
  srcRef.current = source;
  const typing = source !== null && slashQuery(text) !== null;

  useEffect(() => {
    setList(key ? (cache.get(key)?.list ?? []) : []);
  }, [key]);
  useEffect(() => {
    const s = srcRef.current;
    if (!typing || !s) return;
    let live = true;
    void load(api, s).then((l) => live && setList(l));
    return () => {
      live = false;
    };
  }, [typing, key]);

  const q = source ? slashQuery(text) : null;
  const items = q === null ? [] : matchCommands(list, q);
  const open = source !== null && slashOpen(text, list, dismissedAt);
  useEffect(() => setIndex(0), [q]);

  const pick = (c: SlashCommand) => {
    setText(pickText(c));
    setDismissedAt(null);
    const el = field?.current;
    if (el) {
      el.focus();
      requestAnimationFrame(() => el.setSelectionRange(el.value.length, el.value.length));
    }
  };
  return {
    open,
    items: open ? items : [],
    index: Math.min(index, Math.max(items.length - 1, 0)),
    hint: source ? argHint(text, list) : null,
    hover: setIndex,
    pick,
    onKeyDown(e) {
      if (!open || e.isComposing || e.shiftKey || e.metaKey || e.ctrlKey || e.altKey) return false;
      const r = slashKey(e.key, items, index);
      if (!r) return false;
      e.preventDefault();
      e.stopPropagation();
      if (r.kind === "move") setIndex(r.index);
      else if (r.kind === "pick") pick(r.command);
      else setDismissedAt(text);
      return true;
    },
  };
}

/** The list above the field, or the picked command's argument hint. `phone` makes 44 px rows. */
export function SlashList({ s, phone }: { s: Slash; phone?: boolean }) {
  if (s.open) {
    return (
      <div class={`slash${phone ? " m" : ""}`} role="listbox" aria-label="Commands and skills" onMouseDown={(e) => e.preventDefault()}>
        {s.items.map((c, i) => (
          <button type="button" role="option" aria-selected={i === s.index} class={`slash-i${i === s.index ? " on" : ""}`} onMouseEnter={() => s.hover(i)} onClick={() => s.pick(c)}>
            <span class="slash-n">/{c.name}</span>
            {c.argumentHint && <span class="slash-a">{c.argumentHint}</span>}
            {c.description && <span class="slash-d">{c.description}</span>}
          </button>
        ))}
      </div>
    );
  }
  if (s.hint) {
    return (
      <div class={`slash-hint${phone ? " m" : ""}`}>
        <span class="slash-n">/{s.hint.name}</span> <span class="slash-a">{s.hint.hint}</span>
      </div>
    );
  }
  return null;
}
