// SPEC 0.5 V0.79: the `/` list of the composer, desktop, phone and New thread. The hook fetches the agent's
// commands when the owner types `/` at the start (a thread's own list, else the newest of its provider and
// folder), and the list renders above the field. The keys and the filter are slash.ts's. V0.new-thread-skills: the
// Skills button (SkillsButton) opens the same list with every command, for a field where `/` is hard to type.

import { useEffect, useRef, useState } from "preact/hooks";
import { useApi } from "./api-context.ts";
import type { Api, CommandSource } from "./api.ts";
import { argHint, browseCommands, matchCommands, pickInto, slashKey, slashOpen, slashQuery, type SlashCommand } from "./slash.ts";

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
  /** the field has a source: the Skills button shows */
  available: boolean;
  /** the Skills button opened the list */
  browsing: boolean;
  /** the list was asked for and the provider has none yet */
  empty: boolean;
  items: SlashCommand[];
  index: number;
  /** the picked command and its argument hint while nothing follows the name */
  hint: { name: string; hint: string } | null;
  /** The field's keydown: true when the list took the key (the caller then does nothing else with it). */
  onKeyDown(e: KeyboardEvent): boolean;
  pick(c: SlashCommand): void;
  hover(i: number): void;
  /** The Skills button: open the list of every command, or close it. */
  browse(): void;
}

/** The `/` list for a field; `source` null turns it off (an answer field, a form without a folder). */
export function useSlash(source: CommandSource | null, text: string, setText: (t: string) => void, field?: { current: HTMLTextAreaElement | null }): Slash {
  const api = useApi();
  const key = source ? keyOf(source) : "";
  const [list, setList] = useState<SlashCommand[]>(() => (key ? (cache.get(key)?.list ?? []) : []));
  const [index, setIndex] = useState(0);
  const [dismissedAt, setDismissedAt] = useState<string | null>(null);
  const [browsing, setBrowsing] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const srcRef = useRef(source);
  srcRef.current = source;
  const typing = source !== null && slashQuery(text) !== null;

  const wanted = source !== null && (typing || browsing);

  useEffect(() => {
    setList(key ? (cache.get(key)?.list ?? []) : []);
    setLoaded(!!key && cache.has(key));
  }, [key]);
  useEffect(() => {
    const s = srcRef.current;
    if (!wanted || !s) return;
    let live = true;
    void load(api, s).then((l) => {
      if (!live) return;
      setList(l);
      setLoaded(true);
    });
    return () => {
      live = false;
    };
  }, [api, wanted, key]);

  const q = source ? slashQuery(text) : null;
  const typed = source !== null && slashOpen(text, list, dismissedAt);
  const items = typed && q !== null ? matchCommands(list, q) : browsing && source ? browseCommands(list) : [];
  const open = typed || (browsing && items.length > 0);
  useEffect(() => setIndex(0), [q, browsing]);

  const pick = (c: SlashCommand) => {
    setText(pickInto(text, c));
    setDismissedAt(null);
    setBrowsing(false);
    const el = field?.current;
    if (el) {
      el.focus();
      requestAnimationFrame(() => el.setSelectionRange(el.value.length, el.value.length));
    }
  };
  return {
    open,
    available: source !== null,
    browsing: browsing && source !== null,
    empty: browsing && source !== null && loaded && list.length === 0,
    browse: () => setBrowsing((b) => !b),
    items: open ? items : [],
    index: Math.min(index, Math.max(items.length - 1, 0)),
    hint: source ? argHint(text, list) : null,
    hover: setIndex,
    pick,
    onKeyDown(e) {
      if (!open && browsing && e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        setBrowsing(false);
        return true;
      }
      if (!open || e.isComposing || e.shiftKey || e.metaKey || e.ctrlKey || e.altKey) return false;
      const r = slashKey(e.key, items, index);
      if (!r) return false;
      e.preventDefault();
      e.stopPropagation();
      if (r.kind === "move") setIndex(r.index);
      else if (r.kind === "pick") pick(r.command);
      else {
        setDismissedAt(text);
        setBrowsing(false);
      }
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
  if (s.empty) {
    return (
      <div class={`slash-hint${phone ? " m" : ""}`} role="status">
        No skills seen for this agent yet: they show once a thread of it has started.
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

/** V0.new-thread-skills: opens the list of every command and skill, so picking one needs no `/` (a phone keyboard
 *  has it on its second page). `big` is the phone's attach-row tile. Absent when the form has no provider yet. */
export function SkillsButton({ s, big, after }: { s: Slash; big?: boolean; after?(): void }) {
  if (!s.available) return null;
  return (
    <button
      type="button"
      class={big ? "m-capb skills" : "btn sm skills"}
      aria-expanded={s.browsing}
      onMouseDown={(e) => e.preventDefault()}
      onClick={() => {
        s.browse();
        after?.();
      }}
    >
      <span class="skg" aria-hidden="true">
        /
      </span>
      Skills
    </button>
  );
}
