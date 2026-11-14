// Browser glue for the PWA: service worker registration and updates, the install prompt, and
// turning Web Push on and off. HTTP goes through api.ts.

import type { Api } from "./api.ts";
import { base64UrlToBytes, deviceLabel, pushStatus, type PushEnv, type PushStatus } from "./push.ts";

let waiting: ServiceWorker | null = null;

export function isStandalone(): boolean {
  const nav = navigator as Navigator & { standalone?: boolean };
  return nav.standalone === true || (typeof matchMedia === "function" && matchMedia("(display-mode: standalone)").matches);
}

export function pushEnv(): PushEnv {
  const ua = navigator.userAgent;
  return {
    secure: window.isSecureContext,
    serviceWorker: "serviceWorker" in navigator,
    pushManager: "PushManager" in window,
    notification: "Notification" in window,
    ios: /iPhone|iPad|iPod/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1),
    standalone: isStandalone(),
  };
}

function watchWaiting(reg: ServiceWorkerRegistration) {
  if (reg.waiting && navigator.serviceWorker.controller) waiting = reg.waiting;
  reg.addEventListener("updatefound", () => {
    const w = reg.installing;
    w?.addEventListener("statechange", () => {
      if (w.state === "installed" && navigator.serviceWorker.controller) waiting = w;
    });
  });
}

/** Register /sw.js; a waiting new worker takes over on the next navigation. */
export function registerServiceWorker(onOpen: (threadId: string) => void): void {
  if (!("serviceWorker" in navigator) || !window.isSecureContext) return;
  navigator.serviceWorker.addEventListener("message", (e: MessageEvent) => {
    if (e.data?.type === "open" && typeof e.data.threadId === "string") onOpen(e.data.threadId);
  });
  navigator.serviceWorker
    .register("/sw.js", { scope: "/" })
    .then((reg) => {
      watchWaiting(reg);
      // The next navigation (reload, link, closing the tab) gets the new worker.
      window.addEventListener("pagehide", () => waiting?.postMessage({ type: "skipWaiting" }));
    })
    .catch(() => {
      /* no offline shell; the app works without it */
    });
}

/** Reload for a new build (the buildId rule): activate a waiting worker first, so the reload gets the new shell. */
export function reloadForNewBuild(): void {
  const sw = "serviceWorker" in navigator ? navigator.serviceWorker : null;
  if (!waiting || !sw) {
    location.reload();
    return;
  }
  let done = false;
  const go = () => {
    if (done) return;
    done = true;
    location.reload();
  };
  sw.addEventListener("controllerchange", go);
  waiting.postMessage({ type: "skipWaiting" });
  setTimeout(go, 1500);
}

async function registration(): Promise<ServiceWorkerRegistration | null> {
  if (!("serviceWorker" in navigator)) return null;
  return (await navigator.serviceWorker.getRegistration("/")) ?? null;
}

export async function currentPushStatus(): Promise<PushStatus> {
  const env = pushEnv();
  const perm = "Notification" in window ? Notification.permission : "default";
  let subscribed = false;
  try {
    const reg = await registration();
    subscribed = !!(reg && (await reg.pushManager.getSubscription()));
  } catch {
    /* treat as not subscribed */
  }
  return pushStatus(env, perm, subscribed);
}

export type PushResult = { status: PushStatus; error?: string };

export async function enablePush(api: Api): Promise<PushResult> {
  const perm = await Notification.requestPermission();
  if (perm !== "granted") return { status: perm === "denied" ? "denied" : "off" };
  try {
    const reg = await navigator.serviceWorker.ready;
    const key = await api.pushKey();
    const sub = (await reg.pushManager.getSubscription()) ?? (await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: base64UrlToBytes(key) }));
    const r = await api.pushSubscribe(sub.toJSON(), deviceLabel(navigator.userAgent));
    if (!r.ok) {
      await sub.unsubscribe().catch(() => false);
      return { status: "off", error: r.error ?? "The host did not store the subscription" };
    }
    return { status: "on" };
  } catch (e) {
    return { status: "off", error: e instanceof Error ? e.message : String(e) };
  }
}

export async function disablePush(api: Api): Promise<PushResult> {
  try {
    const reg = await registration();
    const sub = reg ? await reg.pushManager.getSubscription() : null;
    if (sub) {
      const endpoint = sub.endpoint;
      await sub.unsubscribe();
      const r = await api.pushUnsubscribe(endpoint);
      if (!r.ok) return { status: "off", error: r.error };
    }
    return { status: "off" };
  } catch (e) {
    return { status: "on", error: e instanceof Error ? e.message : String(e) };
  }
}

// ---- install prompt ----

interface BeforeInstallPromptEvent extends Event {
  prompt(): Promise<void>;
  userChoice: Promise<{ outcome: "accepted" | "dismissed" }>;
}

let deferred: BeforeInstallPromptEvent | null = null;

/** Calls `onChange(true)` when the browser offers an install, `onChange(false)` once installed. */
export function watchInstall(onChange: (canInstall: boolean) => void): void {
  window.addEventListener("beforeinstallprompt", (e) => {
    e.preventDefault();
    deferred = e as BeforeInstallPromptEvent;
    onChange(!isStandalone());
  });
  window.addEventListener("appinstalled", () => {
    deferred = null;
    onChange(false);
  });
}

export async function promptInstall(): Promise<boolean> {
  if (!deferred) return false;
  const d = deferred;
  deferred = null;
  await d.prompt();
  return (await d.userChoice).outcome === "accepted";
}
