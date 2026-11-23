// The one fetch of the client (api.ts uses nothing else for a request/response call): a timeout on every call, the
// JSON body read once and never thrown on, the error a reply carries in one place. The long-lived SSE stream is not a
// request and does not come through here.

/** A call that has not answered in this long is given up (a stalled network must not hold a screen's spinner for ever). */
export const REQUEST_TIMEOUT_MS = 30_000;

export interface RequestOptions {
  method?: "GET" | "POST";
  /** A JSON body (sent with Content-Type). */
  body?: unknown;
  /** A body sent as it is (a file), with the headers that name it. */
  raw?: BodyInit;
  headers?: Record<string, string>;
  /** Default REQUEST_TIMEOUT_MS. */
  timeoutMs?: number;
  /** The caller's own cancel, besides the timeout. */
  signal?: AbortSignal;
  keepalive?: boolean;
}

export interface Reply {
  ok: boolean;
  status: number;
  headers: Headers;
  /** The parsed body, or null when there is none or it is not JSON. */
  json: unknown;
}

/** One HTTP call. Resolves with the reply whatever its status; rejects only when the network failed, the call timed
 *  out or the caller cancelled it. Every POST carries X-Fyr (the host's CSRF header). */
export async function request(url: string, o: RequestOptions = {}): Promise<Reply> {
  const method = o.method ?? "GET";
  const headers: Record<string, string> = { ...(method === "GET" ? { Accept: "application/json" } : { "X-Fyr": "1" }), ...o.headers };
  let body: BodyInit | undefined = o.raw;
  if (o.body !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(o.body);
  }
  const timeout = AbortSignal.timeout(o.timeoutMs ?? REQUEST_TIMEOUT_MS);
  const signal = o.signal ? AbortSignal.any([o.signal, timeout]) : timeout;
  const r = await fetch(url, { method, headers, body, signal, ...(o.keepalive ? { keepalive: true } : {}) });
  return { ok: r.ok, status: r.status, headers: r.headers, json: await r.json().catch(() => null) };
}

/** The message a failed reply carries: the host's `error`, else the status. */
export function replyError(r: Pick<Reply, "status" | "json">): string {
  const e = r.json && typeof r.json === "object" ? (r.json as { error?: unknown }).error : undefined;
  return typeof e === "string" && e ? e : `HTTP ${r.status}`;
}
