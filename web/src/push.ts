// Web Push: the pure parts (SPEC 0.5 V0.15). The browser calls live in pwa.ts, HTTP in api.ts.

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
}

/** Why push cannot be turned on here, or null when it can. */
export function unsupportedReason(env: PushEnv): string | null {
  if (env.ios && !env.standalone) return "On iPhone and iPad, add Fyr to the home screen first (Share, then Add to Home Screen), then turn on notifications in the installed app.";
  if (!env.secure) return "Notifications need a secure address: the tailnet https:// address or localhost.";
  if (!env.serviceWorker || !env.pushManager || !env.notification) return "This browser does not support Web Push.";
  return null;
}

export function pushStatus(env: PushEnv, permission: NotificationPermission, subscribed: boolean): PushStatus {
  if (unsupportedReason(env)) return "unsupported";
  if (permission === "denied") return "denied";
  return permission === "granted" && subscribed ? "on" : "off";
}

export type PushEvent =
  | { type: "permission"; result: NotificationPermission }
  | { type: "subscribed" }
  | { type: "unsubscribed" }
  | { type: "failed" };

/** Transitions of the bell. Turning on: permission, then subscribed (or failed); turning off: unsubscribed. */
export function pushNext(s: PushStatus, e: PushEvent): PushStatus {
  if (s === "unsupported") return s;
  switch (e.type) {
    case "permission":
      return e.result === "denied" ? "denied" : s;
    case "subscribed":
      return "on";
    case "unsubscribed":
      return s === "denied" ? s : "off";
    case "failed":
      return s === "on" ? "on" : s === "denied" ? "denied" : "off";
  }
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
export interface PushPayload {
  title: string;
  body?: string;
  threadId?: string;
  state?: string;
}

export function parsePayload(text: string | null | undefined): PushPayload {
  try {
    const j = JSON.parse(text ?? "");
    if (j && typeof j === "object") return { title: String(j.title || "Fyr"), body: j.body ? String(j.body) : undefined, threadId: j.threadId ? String(j.threadId) : undefined, state: j.state ? String(j.state) : undefined };
  } catch {
    /* plain text */
  }
  return { title: "Fyr", body: text ?? undefined };
}

/** The options for showNotification (SPEC 0.5 V0.15, design/mobile rule 12). A question stays on screen until handled. */
export function notificationOptions(p: PushPayload, now: number): Record<string, unknown> {
  const opts: Record<string, unknown> = {
    body: p.body ?? "",
    icon: "/icons/icon-192.png",
    badge: "/icons/badge-96.png",
    data: { threadId: p.threadId },
    timestamp: now,
    vibrate: [60, 40, 60],
    requireInteraction: p.state === "asking",
    actions: [] as { action: string; title: string }[],
  };
  if (p.threadId) {
    // One notification per thread: a newer push replaces the older one and alerts again.
    opts.tag = p.threadId;
    opts.renotify = true;
    opts.actions = [{ action: "open", title: p.state === "asking" ? "Answer" : "Open" }];
  }
  return opts;
}

/** A thread's notification closes once the thread no longer needs the user (answered elsewhere, retried, settled). */
export function shouldClearNotification(state: string): boolean {
  return state !== "asking" && state !== "failed";
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
