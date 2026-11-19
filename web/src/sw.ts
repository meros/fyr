// Service worker (SPEC 0.5 V0.15), built to dist/sw.js and served at /sw.js with scope /.
// - Precaches the app shell in a cache named after the build.
// - Navigations and the shell: network first, the cached copy when offline.
// - Fonts and icons: cache first (they do not change within a build).
// - Never touches /api or /mcp.
// - Shows pushes and opens the thread on a tap.

import { parsePayload, notificationOptions, openTargets } from "./push.ts";

declare const __BUILD_ID__: string;
declare const __PRECACHE__: string[];

// Minimal service-worker types (the project compiles against the DOM lib).
interface ExtEvent extends Event {
  waitUntil(p: Promise<unknown>): void;
}
interface FetchEvt extends ExtEvent {
  request: Request;
  respondWith(r: Response | Promise<Response>): void;
}
interface PushEvt extends ExtEvent {
  data: { text(): string } | null;
}
interface WindowClientLike {
  url: string;
  focus(): Promise<unknown>;
  postMessage(m: unknown): void;
}
interface NotificationEvt extends ExtEvent {
  notification: { close(): void; data?: { threadId?: string } };
}
interface SWGlobal {
  location: Location;
  registration: { showNotification(title: string, opts: Record<string, unknown>): Promise<void>; scope: string };
  clients: {
    matchAll(o: { type: string; includeUncontrolled: boolean }): Promise<WindowClientLike[]>;
    openWindow(url: string): Promise<unknown>;
    claim(): Promise<void>;
  };
  skipWaiting(): Promise<void>;
  addEventListener(type: string, fn: (e: any) => void): void;
}

const sw = self as unknown as SWGlobal;
const CACHE = `fyr-shell-${__BUILD_ID__}`;
const SHELL = __PRECACHE__;

const isImmutable = (path: string) => path.startsWith("/fonts/") || path.startsWith("/icons/");
const isPrivate = (path: string) => path === "/api" || path.startsWith("/api/") || path === "/mcp" || path.startsWith("/mcp/");

sw.addEventListener("install", (e: ExtEvent) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)));
});

sw.addEventListener("activate", (e: ExtEvent) => {
  e.waitUntil(
    (async () => {
      for (const k of await caches.keys()) if (k.startsWith("fyr-shell-") && k !== CACHE) await caches.delete(k);
      await sw.clients.claim();
    })(),
  );
});

sw.addEventListener("message", (e: MessageEvent) => {
  if (e.data && e.data.type === "skipWaiting") void sw.skipWaiting();
});

async function networkFirst(req: Request, fallbackPath?: string): Promise<Response> {
  const cache = await caches.open(CACHE);
  try {
    const res = await fetch(req);
    if (res.ok && res.type === "basic") {
      // A navigation refreshes the cached shell page; other requests refresh themselves.
      if (!fallbackPath) void cache.put(req, res.clone());
      else if (/^\/(index\.html)?$/.test(new URL(req.url).pathname)) void cache.put(fallbackPath, res.clone());
    }
    return res;
  } catch (err) {
    const hit = (await cache.match(req, { ignoreSearch: !!fallbackPath })) ?? (fallbackPath ? await cache.match(fallbackPath) : undefined);
    if (hit) return hit;
    throw err;
  }
}

async function cacheFirst(req: Request): Promise<Response> {
  const cache = await caches.open(CACHE);
  const hit = await cache.match(req);
  if (hit) return hit;
  const res = await fetch(req);
  if (res.ok) void cache.put(req, res.clone());
  return res;
}

sw.addEventListener("fetch", (e: FetchEvt) => {
  const req = e.request;
  if (req.method !== "GET") return;
  const url = new URL(req.url);
  if (url.origin !== sw.location.origin || isPrivate(url.pathname)) return; // straight to the network
  if (req.mode === "navigate") {
    e.respondWith(networkFirst(req, "/index.html"));
    return;
  }
  e.respondWith(isImmutable(url.pathname) ? cacheFirst(req) : networkFirst(req));
});

sw.addEventListener("push", (e: PushEvt) => {
  const p = parsePayload(e.data ? e.data.text() : null);
  e.waitUntil(sw.registration.showNotification(p.title, notificationOptions(p, Date.now())));
});

sw.addEventListener("notificationclick", (e: NotificationEvt) => {
  e.notification.close();
  // V0.client-refs: the thread's ref (`<hostId>/<threadId>`); a notification from an older build has the id alone.
  const threadId = e.notification.data?.threadId;
  e.waitUntil(
    (async () => {
      const wins = openTargets(await sw.clients.matchAll({ type: "window", includeUncontrolled: true }), sw.location.origin);
      if (wins.length) {
        // Tell every Fyr window; focus the most recently used one (matchAll lists it first).
        if (threadId) for (const w of wins) w.postMessage({ type: "open", threadId });
        await wins[0].focus();
        return;
      }
      await sw.clients.openWindow(threadId ? `/?open=${encodeURIComponent(threadId)}` : "/");
    })(),
  );
});
