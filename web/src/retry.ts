// Riding out a host restart (SPEC 0.5 V0.13): a self-deploy leaves the host
// unreachable for a few seconds. Commands carry a commandId and the host
// applies each id once (I7), so the client sends the same request again.

/** 502, 503, 504 (Tailscale Serve with no daemon) or a network error (null). */
export function isTransient(status: number | null): boolean {
  return status === null || status === 502 || status === 503 || status === 504;
}

/** Waits between tries: about 19.5 s in all. */
export const RETRY_WAITS = [500, 1000, 1500, 2000, 3000, 3500, 4000, 4000];

/** Calls send until it gets a non-transient status or the waits run out. */
export async function sendWithRetry(
  send: () => Promise<number | null>,
  wait: (ms: number) => Promise<void> = (ms) => new Promise((r) => setTimeout(r, ms)),
  onRetry?: () => void,
): Promise<{ status: number | null; gaveUp: boolean }> {
  let status = await send();
  for (const ms of RETRY_WAITS) {
    if (!isTransient(status)) return { status, gaveUp: false };
    onRetry?.();
    await wait(ms);
    status = await send();
  }
  return { status, gaveUp: isTransient(status) };
}
