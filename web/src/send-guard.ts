// V0.quota-auto-continue: synchronous in-flight submit guard and concurrent
// thread.send deduplication, shared by the desktop and phone composers and
// useThreadCommands. Pure so both fronts stay in sync and unit tests run in Node.

/** Synchronous re-entrancy guard for composer / form submit handlers.
 *  Uses a plain closure flag (not async UI state) so rapid double-taps,
 *  multi-clicks, or key repeats within the same millisecond cannot launch
 *  duplicate commands before a re-render disables the button. */
export interface SubmitGuard {
  /** Whether a submit is currently in flight. */
  busy(): boolean;
  /** Runs `fn` unless a submit is already in flight; returns false when skipped. */
  run(fn: () => Promise<boolean>): Promise<boolean>;
}

export function createSubmitGuard(onBusy?: (busy: boolean) => void): SubmitGuard {
  let active = false;
  return {
    busy: () => active,
    async run(fn) {
      if (active) return false;
      active = true;
      onBusy?.(true);
      try {
        return await fn();
      } finally {
        active = false;
        onBusy?.(false);
      }
    },
  };
}

/** Deduplicates identical concurrent in-flight thread.send calls on the same
 *  thread so rapid duplicate UI events reuse one request (and one commandId)
 *  instead of dispatching back-to-back copies. Once the in-flight request
 *  settles, subsequent intentional sends with the same text proceed normally. */
export function createSendDedupe<T>() {
  const inFlight = new Map<string, Promise<T>>();
  return (threadId: string, args: { text: string; attachments?: string[]; from?: string }, fn: () => Promise<T>): Promise<T> => {
    const key = JSON.stringify([threadId, args.text, args.attachments ?? null, args.from ?? null]);
    const existing = inFlight.get(key);
    if (existing) return existing;
    const p = fn().finally(() => {
      if (inFlight.get(key) === p) inFlight.delete(key);
    });
    inFlight.set(key, p);
    return p;
  };
}
