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

/** A GET whose reply is text (a file's content, V0.message-links-files): the text, or null when the host refused.
 *  Rejects like request. */
export async function requestText(url: string, signal?: AbortSignal): Promise<string | null> {
  const timeout = AbortSignal.timeout(REQUEST_TIMEOUT_MS);
  const r = await fetch(url, { signal: signal ? AbortSignal.any([signal, timeout]) : timeout });
  return r.ok ? await r.text() : null;
}

/** The message a failed reply carries: the host's `error`, else the status. */
export function replyError(r: Pick<Reply, "status" | "json">): string {
  const e = r.json && typeof r.json === "object" ? (r.json as { error?: unknown }).error : undefined;
  return typeof e === "string" && e ? e : `HTTP ${r.status}`;
}

/** What `upload` answers. `status` is the HTTP status; null when the host could not be reached or the upload stalled
 *  (`stalled`), 0 when the caller cancelled it. */
export interface UploadAnswer {
  status: number | null;
  json: unknown;
  stalled?: boolean;
}

/** One file sent with its progress (XMLHttpRequest: fetch reports no upload progress). It never rejects. A send that
 *  moves no byte and gets no answer for `stallMs` is given up, as `request` gives up a stalled call; the time is counted
 *  from the last progress, so a large file on a slow link is not cut. The caller's `signal` cancels it, and its listener
 *  is removed when the upload ends however it ends. Every upload carries X-Fyr. */
export function upload(url: string, file: Blob, headers: Record<string, string>, onProgress: (loaded: number, total: number) => void, signal?: AbortSignal, stallMs = REQUEST_TIMEOUT_MS): Promise<UploadAnswer> {
  return new Promise<UploadAnswer>((resolve) => {
    const x = new XMLHttpRequest();
    let timer: ReturnType<typeof setTimeout> | undefined;
    let stalled = false;
    const onAbort = () => x.abort();
    const end = (a: UploadAnswer) => {
      clearTimeout(timer);
      signal?.removeEventListener("abort", onAbort);
      resolve(a);
    };
    const arm = () => {
      clearTimeout(timer);
      timer = setTimeout(() => ((stalled = true), x.abort()), stallMs);
    };
    if (signal?.aborted) return resolve({ status: 0, json: null });
    x.open("POST", url);
    x.setRequestHeader("X-Fyr", "1");
    for (const [k, v] of Object.entries(headers)) x.setRequestHeader(k, v);
    x.upload.onprogress = (e) => {
      arm();
      onProgress(e.loaded, e.lengthComputable ? e.total : file.size);
    };
    x.onload = () => {
      let json: unknown = null;
      try {
        json = JSON.parse(x.responseText);
      } catch {
        /* not JSON */
      }
      end({ status: x.status, json });
    };
    x.onerror = () => end({ status: null, json: null });
    x.onabort = () => end(stalled ? { status: null, json: null, stalled: true } : { status: 0, json: null });
    signal?.addEventListener("abort", onAbort);
    arm();
    x.send(file);
  });
}
