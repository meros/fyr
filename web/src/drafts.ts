// SPEC 0.5 V0.29: what the owner typed in a thread's composer survives a new
// message, a card that opens, a screen change and a reload (a deploy reloads
// the page). One draft per thread; sending clears it.
import { useState } from "preact/hooks";
import { browserStorage } from "./prefs.ts";

export interface KV {
  getItem(k: string): string | null;
  setItem(k: string, v: string): void;
  removeItem(k: string): void;
}

const PREFIX = "fyr.draft.";

/** V0.client-refs: a draft is kept under its thread's ref (`<hostId>/<id>`), so one thread id on two hosts is two drafts. */
export function draftStore(storage: KV | null) {
  const mem = new Map<string, string>();
  return {
    get(id: string): string {
      if (mem.has(id)) return mem.get(id)!;
      try {
        const v = storage?.getItem(PREFIX + id);
        return v ?? "";
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
      } catch {
        /* private mode or full: the memory copy still keeps it this session */
      }
    },
  };
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
