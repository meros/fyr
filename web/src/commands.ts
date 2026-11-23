// #41: thread commands for both shells. One hook owns sending a command to some threads, the undo of a
// settle, archive or snooze (SPEC U8), the toast that reports it, and the error text when the host says no.
// The desktop keys and buttons and the phone's swipes and sheets all go through it.

import type { HostId } from "./hostid.ts";
import type { CommandReply } from "./types.ts";
import { useEffect, useRef, useState } from "preact/hooks";
import { useApi } from "./api-context.ts";
import { recordUndo, undoCommands, undoKindOf, undoLive, type UndoEntry } from "./store.ts";
import { createSendDedupe } from "./send-guard.ts";
import type { CrossHostHandoverArgs } from "./handover.ts";
import { type SendArgs } from "./composer.tsx";

interface Toast {
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
  /** `run`, resolving to the ids the host took: what a screen may let go (V0.app-shell-tidy, `leaveAfterHost`). */
  took(action: string, ids: string[], args?: Record<string, unknown>, o?: RunOptions): Promise<string[]>;
  /** thread.send; false (and the error shown) when the host did not take it */
  send(id: string, args: SendArgs): Promise<boolean>;
  /** V0.45: thread.handover; resolves to the host's error text, or null */
  handover(id: string | null, profile?: string, model?: string, effort?: string): Promise<string | null>;
  /** Decision 0039, V0.trans-host-handover: front.threadHandover across hosts; resolves to new threadId or error text */
  handoverHost(id: string | null, target: CrossHostHandoverArgs): Promise<{ threadId?: string; error?: string }>;
  doUndo(): Promise<void>;
  /** V0.108: asks Fyr for a new title from the conversation; the title changes when the model answers. */
  regenerateTitle(id: string): Promise<void>;
  notify(text: string, error?: boolean): void;
  showError(text: string): void;
}

interface RunOptions {
  /** the toast's text for n threads taken, in place of the undo label (V0.162: a bulk settle names the pinned it kept) */
  label?: (n: number) => string;
}

interface CommandOptions {
  /** how long Undo stays offered (the phone gives it less time) */
  undoMs: number;
  /** after an undo; ok is false when the host refused one of the inverse commands */
  onUndone?(entry: UndoEntry, ok: boolean): void;
  /** V0.207: told of each action the owner runs, before it goes out */
  onRun?(action: string, ids: string[]): void;
  /** The install id of the host that runs a thread (decision 0031: a command names its host); none for a thread the
   *  store does not have, which then gets no command. */
  hostIdOf(threadId: string): HostId | undefined;
}

export function useThreadCommands(o: CommandOptions): ThreadCommands {
  const api = useApi();
  const [toast, setToast] = useState<Toast | null>(null);
  const [undo, setUndo] = useState<UndoEntry | null>(null);
  const opts = useRef(o);
  opts.current = o;
  const undoRef = useRef(undo);
  undoRef.current = undo;
  const dedupeSend = useRef(createSendDedupe<boolean>()).current;

  useEffect(() => {
    if (!toast) return;
    const ms = toast.undo ? opts.current.undoMs : 4000;
    const id = setTimeout(() => setToast((t) => (t === toast ? null : t)), ms);
    return () => clearTimeout(id);
  }, [toast]);

  const notify = (text: string, error = false) => setToast(error ? { text, undo: false, error: true, at: Date.now() } : { text, undo: false, at: Date.now() });
  const showError = (text: string) => notify(text, true);

  /** A thread's command to its own host; a thread whose host is not known gets none, and says so. */
  const to = (id: string | undefined, action: string, args?: Record<string, unknown>): Promise<CommandReply> => {
    const host = id ? opts.current.hostIdOf(id) : undefined;
    return host ? api.command(host, action, id, args) : Promise.resolve({ ok: false, error: "This thread's host is not one Fyr follows now" });
  };

  const took = async (action: string, ids: string[], args?: Record<string, unknown>, ro?: RunOptions): Promise<string[]> => {
    if (!ids.length) return [];
    opts.current.onRun?.(action, ids);
    const replies = await Promise.all(ids.map((id) => to(id, action, args).then((r) => ({ id, r }))));
    const ok = replies.filter((x) => x.r.ok).map((x) => x.id);
    const bad = replies.filter((x) => !x.r.ok);
    const kind = undoKindOf(action);
    if (kind && ok.length) {
      const u = recordUndo(kind, ok, Date.now(), ro?.label?.(ok.length));
      setUndo(u);
      setToast({ text: u.label, undo: true, at: Date.now() });
    }
    if (bad.length) showError(bad.length === 1 ? (bad[0].r.error ?? "Failed") : `${bad.length} failed: ${bad[0].r.error ?? ""}`);
    return ok;
  };
  const run = async (action: string, ids: string[], args?: Record<string, unknown>, ro?: RunOptions): Promise<number> => (await took(action, ids, args, ro)).length;

  const send = (id: string, args: SendArgs): Promise<boolean> =>
    dedupeSend(id, args, async () => {
      const r = await to(id, "thread.send", { ...args });
      if (!r.ok) showError(r.error ?? "Could not send");
      return r.ok;
    });

  const handover = async (id: string | null, profile?: string, model?: string, effort?: string): Promise<string | null> => {
    if (!id || !profile) return "Pick a profile";
    const r = await to(id, "thread.handover", { profile, ...(model ? { model } : {}), ...(effort ? { effort } : {}) }); // V0.model-choice: with its effort
    return r.ok ? null : (r.error ?? "The handover could not start");
  };

  const handoverHost = async (id: string | null, target: CrossHostHandoverArgs): Promise<{ threadId?: string; error?: string }> => {
    if (!id || !target.profile || !target.toHost) return { error: "Pick a host and provider" };
    const fromHost = opts.current.hostIdOf(id);
    if (!fromHost) return { error: "This thread's host is not one Fyr follows now" };
    const r = await api.frontCommand("front.threadHandover", {
      fromHost,
      threadId: id,
      toHost: target.toHost,
      profile: target.profile,
      ...(target.model ? { model: target.model } : {}),
      ...(target.effort !== undefined ? { effort: target.effort } : {}),
      ...(target.cwd ? { cwd: target.cwd } : {}),
      ...(target.projectId ? { projectId: target.projectId } : {}),
      ...(target.useWorktree ? { useWorktree: true } : {}),
    });
    if (!r.ok) return { error: r.error ?? "The handover could not start" };
    const tid = (r.threadId || (r.result && typeof r.result.threadId === "string" ? r.result.threadId : undefined)) as string | undefined;
    const warn = r.result && typeof r.result.sourceWarning === "string" ? r.result.sourceWarning : "";
    if (warn) notify(warn, true);
    return { threadId: tid };
  };

  const regenerateTitle = async (id: string) => {
    if (await run("thread.regenerateTitle", [id])) notify("Fyr is writing a new title");
  };

  const doUndo = async () => {
    const entry = undoRef.current;
    const cmds = undoCommands(entry, Date.now());
    if (!entry || !cmds) return;
    setUndo(null);
    const res = await Promise.all(cmds.map((c) => to(c.threadId, c.action, c.args)));
    const bad = res.find((r) => !r.ok);
    if (bad) showError(`Undo failed: ${bad.error ?? ""}`);
    else notify("Undone");
    opts.current.onUndone?.(entry, !bad);
  };

  return { toast, undo, canUndo: () => undoLive(undo, Date.now()), run, took, send, handover, handoverHost, doUndo, regenerateTitle, notify, showError };
}
