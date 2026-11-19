// SPEC 0.5 V0.29: what the owner typed in a thread's composer survives a new
// message, a card that opens, a screen change and a reload (a deploy reloads
// the page). One draft per thread; sending clears it.
import { useState } from "preact/hooks";

export interface KV {
  getItem(k: string): string | null;
  setItem(k: string, v: string): void;
  removeItem(k: string): void;
}

const PREFIX = "fyr.draft.";

/** V0.client-refs: a draft is kept under its thread's ref (`<hostId>/<id>`). One written before, under the host's own id,
 *  that the migration could not place (no single host had the thread then) is read as a fallback for one release, and
 *  goes once the draft is written under the ref. */
function oldKey(id: string): string | null {
  const i = id.indexOf("/");
  return i > 0 ? PREFIX + id.slice(i + 1) : null;
}

export function draftStore(storage: KV | null) {
  const mem = new Map<string, string>();
  return {
    get(id: string): string {
      if (mem.has(id)) return mem.get(id)!;
      try {
        const v = storage?.getItem(PREFIX + id);
        if (v !== null && v !== undefined) return v;
        const old = oldKey(id);
        return (old && storage?.getItem(old)) || "";
      } catch {
        return "";
      }
    },
    set(id: string, text: string) {
      if (text) mem.set(id, text);
      else mem.delete(id);
      try {
        if (text) storage?.setItem(PREFIX + id, text);
        else storage?.removeItem(PREFIX + id);
        const old = oldKey(id);
        if (old) storage?.removeItem(old);
      } catch {
        /* private mode or full: the memory copy still keeps it this session */
      }
    },
  };
}

function browserStorage(): KV | null {
  try {
    return typeof localStorage !== "undefined" ? localStorage : null;
  } catch {
    return null;
  }
}

const drafts = draftStore(browserStorage());

/** The composer text of one thread, kept outside the component. */
export function useDraft(threadId: string): [string, (t: string) => void] {
  const [text, setLocal] = useState(() => drafts.get(threadId));
  const set = (t: string) => {
    drafts.set(threadId, t);
    setLocal(t);
  };
  return [text, set];
}
