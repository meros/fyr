// Web Push: the pure parts (SPEC 0.5 V0.15). The browser calls live in pwa.ts, HTTP in api.ts.
import { asObject, parseJson } from "./decode.ts";
import { iconUrl } from "./icon-paths.ts";

/** VAPID public keys come as base64url; pushManager.subscribe wants the raw bytes. */
export function base64UrlToBytes(s: string): Uint8Array<ArrayBuffer> {
  const b64 = s.replace(/-/g, "+").replace(/_/g, "/").replace(/\s+/g, "");
  const padded = b64 + "=".repeat((4 - (b64.length % 4)) % 4);
  if (!/^[A-Za-z0-9+/]*={0,2}$/.test(padded)) throw new Error("not base64url");
  const bin = atob(padded);
  const out = new Uint8Array(new ArrayBuffer(bin.length));
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export type PushStatus = "unsupported" | "off" | "on" | "denied";

export interface PushEnv {
  secure: boolean;
  serviceWorker: boolean;
  pushManager: boolean;
  notification: boolean;
  /** iPhone or iPad: Web Push exists only for a web app added to the home screen */
  ios: boolean;
  standalone: boolean;
  /** V0.mac-notify: Fyr.app's window, which shows pushes as macOS notifications instead of Web Push */
  native?: boolean;
}

/** Why push cannot be turned on here, or null when it can. */
export function unsupportedReason(env: PushEnv): string | null {
  if (env.native) return null;
  if (env.ios && !env.standalone) return "On an iPhone, add Fyr to the Home Screen first: Share, then Add to Home Screen.";
  if (!env.secure) return "Notifications need a secure address: the tailnet https:// address or localhost.";
  if (!env.serviceWorker || !env.pushManager || !env.notification) return "This browser does not support Web Push.";
  return null;
}

export function pushStatus(env: PushEnv, permission: NotificationPermission, subscribed: boolean): PushStatus {
  if (unsupportedReason(env)) return "unsupported";
  if (permission === "denied") return "denied";
  return permission === "granted" && subscribed ? "on" : "off";
}

export const PUSH_TITLE: Record<PushStatus, string> = {
  unsupported: "Notifications are not available here",
  off: "Notifications are off",
  on: "Notifications are on",
  denied: "Notifications are blocked in the browser's site settings",
};

/**
 * Reconcile the browser's subscription with the host (POST /api/push/status). `known` is null when the
 * host could not be asked: keep what the browser has. A subscription the host deleted (after a 404 or
 * 410 from the push service) is dropped locally too, so the bell shows off.
 */
export function reconcilePush(hasSubscription: boolean, known: boolean | null): { unsubscribe: boolean; subscribed: boolean } {
  if (!hasSubscription) return { unsubscribe: false, subscribed: false };
  if (known === false) return { unsubscribe: true, subscribed: false };
  return { unsubscribe: false, subscribed: true };
}

/** A short device label for the subscription list: "Android Chrome", "iPhone Safari", "Linux Firefox". */
export function deviceLabel(ua: string): string {
  const os = /iPhone/.test(ua)
    ? "iPhone"
    : /iPad/.test(ua) || (/Macintosh/.test(ua) && /Mobile/.test(ua))
      ? "iPad"
      : /Android/.test(ua)
        ? "Android"
        : /Mac OS X|Macintosh/.test(ua)
          ? "Mac"
          : /Windows/.test(ua)
            ? "Windows"
            : /Linux|X11/.test(ua)
              ? "Linux"
              : "Device";
  const browser = /Edg\//.test(ua)
    ? "Edge"
    : /Firefox\/|FxiOS/.test(ua)
      ? "Firefox"
      : /Chrome\/|CriOS/.test(ua)
        ? "Chrome"
        : /Safari\//.test(ua)
          ? "Safari"
          : "Browser";
  return `${os} ${browser}`;
}

/** The payload the host sends (SPEC V0.15). */
interface PushPayload {
  title: string;
  body?: string;
  threadId?: string;
  /** V0.client-refs (decision 0031 F15): the thread's host by install id; missing from a front before it */
  host?: string;
  state?: string;
}

/** V0.client-refs: the thread a push is about, as the client names it: `<host>/<threadId>`, or the id alone from an
 *  older front (the app then opens the one thread that has it, decision 0031 C12). */
function payloadRef(p: Pick<PushPayload, "host" | "threadId">): string | undefined {
  if (!p.threadId) return undefined;
  return p.host ? `${p.host}/${p.threadId}` : p.threadId;
}

export function parsePayload(text: string | null | undefined): PushPayload {
  return parseJson(text, decodePayload, { title: "Fyr", body: text ?? undefined }); // not a JSON object: plain text
}

function decodePayload(v: unknown): PushPayload | undefined {
  const j = asObject(v);
  return j && { title: String(j.title || "Fyr"), body: j.body ? String(j.body) : undefined, threadId: j.threadId ? String(j.threadId) : undefined, ...(j.host ? { host: String(j.host) } : {}), state: j.state ? String(j.state) : undefined };
}

/** The options for showNotification (SPEC 0.5 V0.15, design/mobile rule 12). A question stays on screen until handled. */
export function notificationOptions(p: PushPayload, now: number): Record<string, unknown> {
  const id = payloadRef(p);
  const opts: Record<string, unknown> = {
    body: p.body ?? "",
    icon: iconUrl("any192"),
    badge: iconUrl("badge96"),
    // V0.notification-polish: the state it was shown for, so it closes when the thread leaves that state.
    // V0.client-refs: the ref, so the tap opens this host's thread and the app closes it by the id it keeps.
    data: { threadId: id, state: p.state },
    timestamp: now,
    vibrate: [60, 40, 60],
    requireInteraction: p.state === "asking",
    actions: [] as { action: string; title: string }[],
  };
  if (id) {
    // One notification per thread: a newer push replaces the older one and alerts again.
    opts.tag = id;
    opts.renotify = true;
    opts.actions = [{ action: "open", title: p.state === "asking" ? "Answer" : "Open" }];
  }
  return opts;
}

/**
 * V0.notification-polish: a thread's notification closes when the thread leaves the state it was shown for: a question
 * answered elsewhere, a failure retried, a finished thread read or started again. While the thread stays in that state
 * the notification stays, so "Done" is not closed by the next message about a thread that is still done.
 * `shown` is undefined for a notification from a build before this one: only a question or a failure keeps it.
 */
export function notificationIsStale(shown: string | undefined, state: string): boolean {
  if (!shown) return state !== "asking" && state !== "failed";
  if (shown === "quota") return state === "working" || state === "settled" || state === "archived";
  return shown !== state;
}

// ---- opening the thread of a tapped notification (V0.15) ----

type KV = { getItem(k: string): string | null; setItem(k: string, v: string): void; removeItem(k: string): void };
const OPEN_KEY = "fyr.pendingOpen";

/** Keep the thread a tap asked for until it is on screen: a reload for a new build must not lose it. */
export function rememberOpen(st: KV | null, threadId: string): void {
  try {
    st?.setItem(OPEN_KEY, threadId);
  } catch {
    /* storage denied: the in-memory value still works without a reload */
  }
}

/** The thread a tap asked for, once. */
export function takeOpen(st: KV | null): string | null {
  try {
    const v = st?.getItem(OPEN_KEY) ?? null;
    if (v) st?.removeItem(OPEN_KEY);
    return v;
  } catch {
    return null;
  }
}

/** Every window of this origin gets the open message (the installed app and any open tab). */
export function openTargets<W extends { url: string }>(wins: W[], origin: string): W[] {
  return wins.filter((c) => {
    try {
      return new URL(c.url).origin === origin;
    } catch {
      return false;
    }
  });
}

/** V0.119: Settings → This device → Notifications: the row's line under its label, and its switch. */
export function pushRow(status: PushStatus, reason: string | null, tested: boolean, native = false): { desc: string; error: boolean; disabled: boolean; on: boolean; test: boolean } {
  // V0.notification-polish: the line says what arrives; FINISHED_DESC, shown while they are on, says what never does.
  const plain = "When a thread needs you: a question, an approval, a failure.";
  switch (status) {
    case "on":
      return { desc: tested ? "Sent to this device. It should arrive in a few seconds." : plain, error: false, disabled: false, on: true, test: true };
    case "off":
      return { desc: plain, error: false, disabled: false, on: false, test: false };
    case "denied":
      return {
        desc: native
          ? "Blocked in System Settings. Allow notifications for Fyr in System Settings → Notifications, then reload Fyr."
          : "Blocked for this site. Allow them in the browser's site settings, then reload Fyr.",
        error: true,
        disabled: true,
        on: false,
        test: false,
      };
    case "unsupported":
      return { desc: reason ?? "Notifications are not available in this browser.", error: false, disabled: true, on: false, test: false };
  }
}

// ---- V0.notification-polish, V0.quota-reset-wake: what this device is told about ----

export interface PushPrefs {
  /** also a thread that finished and is unread */
  finished: boolean;
  /** also when a provider's quota window resets and a thread still waits */
  quotaReset: boolean;
}

export const PUSH_PREFS_KEY = "fyr.push.prefs";
const DEFAULT_PUSH_PREFS: PushPrefs = { finished: true, quotaReset: false };
export const FINISHED_DESC = "Also when a thread finishes. Nothing arrives for the thread on screen or in a project's quiet hours.";
export const QUOTA_RESET_DESC = "When a provider's quota window resets and a waiting thread still has not resumed.";

/** The stored choice; anything unreadable is the default (finished on, quota resets off until asked for). */
export function parsePushPrefs(raw: string | null | undefined): PushPrefs {
  return parseJson(raw, decodePushPrefs, { ...DEFAULT_PUSH_PREFS });
}

function decodePushPrefs(v: unknown): PushPrefs | undefined {
  const j = asObject(v);
  return j && { finished: j.finished !== false, quotaReset: j.quotaReset === true };
}
