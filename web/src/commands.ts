// #41: thread commands for both shells. One hook owns sending a command to some threads, the undo of a
// settle, archive or snooze (SPEC U8), the toast that reports it, and the error text when the host says no.
// The desktop keys and buttons and the phone's swipes and sheets all go through it.

import { useEffect, useRef, useState } from "preact/hooks";
import { useApi } from "./api-context.ts";
import { recordUndo, undoCommands, undoKindOf, undoLive, type UndoEntry } from "./store.ts";
import type { SendArgs } from "./thread.tsx";

export interface Toast {
  text: string;
  undo: boolean;
  error?: boolean;
  at: number;
}

export interface ThreadCommands {
  toast: Toast | null;
  /** the last settle, archive or snooze, for Undo */
  undo: UndoEntry | null;
  /** Undo is still offered */
  canUndo(): boolean;
  /** Runs `action` on each thread; resolves to how many the host took. An undoable one offers Undo; failures show as an error. */
  run(action: string, ids: string[], args?: Record<string, unknown>, o?: RunOptions): Promise<number>;
  /** thread.send; false (and the error shown) when the host did not take it */
  send(id: string, args: SendArgs): Promise<boolean>;
  /** V0.45: thread.handover; resolves to the host's error text, or null */
  handover(id: string | null, profile?: string, model?: string, effort?: string): Promise<string | null>;
  doUndo(): Promise<void>;
  /** V0.108: asks Fyr for a new title from the conversation; the title changes when the model answers. */
  regenerateTitle(id: string): Promise<void>;
  notify(text: string, error?: boolean): void;
  showError(text: string): void;
}

export interface RunOptions {
  /** the toast's text for n threads taken, in place of the undo label (V0.162: a bulk settle names the pinned it kept) */
  label?: (n: number) => string;
}

export interface CommandOptions {
  /** how long Undo stays offered (the phone gives it less time) */
  undoMs: number;
  /** after an undo; ok is false when the host refused one of the inverse commands */
  onUndone?(entry: UndoEntry, ok: boolean): void;
  /** V0.207: told of each action the owner runs, before it goes out */
  onRun?(action: string, ids: string[]): void;
  /** The install id of the host that runs a thread (decision 0031: a command names its host). */
  hostOf(threadId: string): string;
}

export function useThreadCommands(o: CommandOptions): ThreadCommands {
  const api = useApi();
  const [toast, setToast] = useState<Toast | null>(null);
  const [undo, setUndo] = useState<UndoEntry | null>(null);
  const opts = useRef(o);
  opts.current = o;
  const undoRef = useRef(undo);
  undoRef.current = undo;

  useEffect(() => {
    if (!toast) return;
    const ms = toast.undo ? opts.current.undoMs : 4000;
    const id = setTimeout(() => setToast((t) => (t === toast ? null : t)), ms);
    return () => clearTimeout(id);
  }, [toast]);

  const notify = (text: string, error = false) => setToast(error ? { text, undo: false, error: true, at: Date.now() } : { text, undo: false, at: Date.now() });
  const showError = (text: string) => notify(text, true);

  const run = async (action: string, ids: string[], args?: Record<string, unknown>, ro?: RunOptions): Promise<number> => {
    if (!ids.length) return 0;
    opts.current.onRun?.(action, ids);
    const replies = await Promise.all(ids.map((id) => api.command(opts.current.hostOf(id), action, id, args).then((r) => ({ id, r }))));
    const ok = replies.filter((x) => x.r.ok).map((x) => x.id);
    const bad = replies.filter((x) => !x.r.ok);
    const kind = undoKindOf(action);
    if (kind && ok.length) {
      const u = recordUndo(kind, ok, Date.now(), ro?.label?.(ok.length));
      setUndo(u);
      setToast({ text: u.label, undo: true, at: Date.now() });
    }
    if (bad.length) showError(bad.length === 1 ? (bad[0].r.error ?? "Failed") : `${bad.length} failed: ${bad[0].r.error ?? ""}`);
    return ok.length;
  };

  const send = async (id: string, args: SendArgs): Promise<boolean> => {
    const r = await api.command(opts.current.hostOf(id), "thread.send", id, { ...args });
    if (!r.ok) showError(r.error ?? "Could not send");
    return r.ok;
  };

  const handover = async (id: string | null, profile?: string, model?: string, effort?: string): Promise<string | null> => {
    if (!id || !profile) return "Pick a profile";
    const r = await api.command(opts.current.hostOf(id), "thread.handover", id, { profile, ...(model ? { model } : {}), ...(effort ? { effort } : {}) }); // V0.model-choice: with its effort
    return r.ok ? null : (r.error ?? "The handover could not start");
  };

  const regenerateTitle = async (id: string) => {
    if (await run("thread.regenerateTitle", [id])) notify("Fyr is writing a new title");
  };

  const doUndo = async () => {
    const entry = undoRef.current;
    const cmds = undoCommands(entry, Date.now());
    if (!entry || !cmds) return;
    setUndo(null);
    const res = await Promise.all(cmds.map((c) => api.command(opts.current.hostOf(c.threadId ?? ""), c.action, c.threadId, c.args)));
    const bad = res.find((r) => !r.ok);
    if (bad) showError(`Undo failed: ${bad.error ?? ""}`);
    else notify("Undone");
    opts.current.onUndone?.(entry, !bad);
  };

  return { toast, undo, canUndo: () => undoLive(undo, Date.now()), run, send, handover, doUndo, regenerateTitle, notify, showError };
}
