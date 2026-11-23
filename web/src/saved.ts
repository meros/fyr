// SPEC V0.119: the Saved mark's clock, apart from the hook (settings-rows.tsx useSaved) so a test can run it on a fake
// clock instead of waiting the wall time out (V0.single-sources).

/** How long "Saved" shows after a command answered ok. */
export const SAVED_MS = 2000;

/** set(true) at a mark and set(false) SAVED_MS after the newest one; stop() drops a pending hide (the page closed). */
export function savedClock(set: (shown: boolean) => void): { mark(): void; stop(): void } {
  let t: ReturnType<typeof setTimeout> | undefined;
  return {
    mark() {
      clearTimeout(t);
      set(true);
      t = setTimeout(() => set(false), SAVED_MS);
    },
    stop() {
      clearTimeout(t);
    },
  };
}
