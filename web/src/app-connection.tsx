// The connection to the hosts: events, the first selection, the device cache and the push settings it sends. (split from the App of app.tsx; takes and returns plain objects).

// The app shell: state, commands and the keyboard.

import { storedGet, storedSet } from "./prefs.ts";
import { applyTheme, sessionStore, params, PAGE, idle } from "./app-util.tsx";
import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import {
  applySnapshot,
  liveEvent,
  mergeEvents,
  resumeSeq,
  resumed,
  setConn,
  stateForCache,
  upsertThread,
  upsertProject,
  setHosts,
  setProjectOrder,
  setDeploy,
  setStall,
  type Store,
  applyThreadPatch,
  moveHostInStore,
  resolveRef,
} from "./store.ts";
import { moveHostKeys, moveRef, rotations } from "./host-rotation.ts";
import { openView } from "./seen.ts";
import { clearThreadNotification, syncPushHidden, syncPushPrefs, syncPushStatus, registerServiceWorker, watchInstall } from "./pwa.ts";
import { PUSH_PREFS_KEY, parsePushPrefs, rememberOpen, takeOpen, type PushPrefs } from "./push.ts";
import { FILTER_KEY, projectRecords } from "./projects.ts";
import { pushHiddenOf, type PushHidden } from "./hidden.ts";
import { HOSTS_OFF_KEY, parseHostsOff } from "./filters.ts";
import { deviceId, presenceTarget, shouldBeat, type BeatState } from "./presence.ts";
import { cache, mergeEventLists } from "./cache.ts";
import { PHONE_QUERY } from "./motion-ui.tsx";
import { recordOpen } from "./desk-history.ts";
import type { useAppState } from "./app-state.tsx";
import type { useNavHistory } from "./app-history.tsx";

export function useConnection(c: Pick<ReturnType<typeof useAppState>, "api" | "rawStore" | "setStore" | "fullStore" | "hostOfId" | "deviceHidden" | "store" | "setLastOpen" | "view" | "setView" | "setZone" | "setDialog" | "setSnoozeTargets" | "setTracker" | "theme" | "setTick" | "nav" | "pushSt" | "setPushSt" | "setCanInstall" | "setPendingOpen" | "setProjectFilterState" | "setHostsOffState" | "gate"> & Pick<ReturnType<typeof useNavHistory>, "navReplace">) {
  const { api, rawStore, setStore, fullStore, hostOfId, deviceHidden, store, setLastOpen, view, setView, setZone, setDialog, setSnoozeTargets, setTracker, theme, setTick, nav, pushSt, setPushSt, setCanInstall, setPendingOpen, setProjectFilterState, setHostsOffState, gate, navReplace } = c;
  // ---- connection ----
  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  useEffect(() => {
    registerServiceWorker((id) => {
      rememberOpen(sessionStore(), id);
      setPendingOpen(id);
    });
    // A tap that arrived just before a reload for a new build (V0.15).
    const carried = takeOpen(sessionStore());
    if (carried) {
      rememberOpen(sessionStore(), carried);
      setPendingOpen(carried);
    }
    watchInstall((can) => setCanInstall(can));
    // Reconcile with the host at start and whenever the app comes back to the foreground.
    void syncPushStatus(api).then(setPushSt);
    document.addEventListener("visibilitychange", () => {
      if (document.visibilityState === "visible") void syncPushStatus(api).then(setPushSt);
    });
    // V0.38: stream messages are applied together, once per frame (a short timer while the page is
    // hidden), so a burst of updates is one render, not one per message.
    let queue: ((s: Store) => Store)[] = [];
    let scheduled = false;
    const flush = () => {
      scheduled = false;
      const fns = queue;
      queue = [];
      if (fns.length) setStore((s) => fns.reduce((acc, f) => f(acc), s));
    };
    const batch = (fn: (s: Store) => Store) => {
      queue.push(fn);
      if (scheduled) return;
      scheduled = true;
      if (document.hidden || typeof requestAnimationFrame !== "function") setTimeout(flush, 250);
      else requestAnimationFrame(flush);
    };
    api.start({
      state: (dto) => {
        flush();
        setStore((s) => applySnapshot(s, dto, Date.now()));
      },
      thread: (t) => {
        clearThreadNotification(t.id, t.attention.state);
        batch((s) => upsertThread(s, t));
      },
      // V0.38: a patch against our copy; when it does not fit, ask for the whole thread.
      threadPatch: (p) => {
        const st = (p.patch as { attention?: { state?: string } }).attention?.state;
        if (st) clearThreadNotification(p.id, st);
        batch((s) => {
          const r = applyThreadPatch(s, p);
          if (r.needFull) queueMicrotask(() => void api.thread(p.host, p.id).then((t) => t && setStore((x) => upsertThread(x, t))));
          return r.store;
        });
      },
      event: (e) => batch((s) => liveEvent(s, e)),
      project: (p) => batch((s) => upsertProject(s, p)),
      projectOrder: (ids) => batch((s) => setProjectOrder(s, ids)),
      deploy: (p) => batch((s) => setDeploy(s, p)),
      stall: (host, raw) => batch((s) => setStall(s, host, raw)),
      hosts: (hs) => batch((s) => setHosts(s, hs)),
      hostMarks: (m) => batch((s) => ({ ...s, hostMarks: m })), // V0.171
      conn: (c) => batch((s) => setConn(s, c)),
    });
    const iv = setInterval(() => setTick(Date.now()), 15_000);
    return () => clearInterval(iv);
  }, []);

  // A link names a record by the host's own id; once the records are here it names the ref (decision 0031 C12).
  useEffect(() => {
    if (!store.loaded) return;
    const s = nav.screen;
    if (s.name === "thread") {
      const r = resolveRef(Object.values(store.threads), s.id);
      if (r !== s.id) navReplace({ ...nav, screen: { ...s, id: r } });
    } else if (s.name === "project") {
      const r = resolveRef(projectRecords(store.projects), s.id);
      if (r !== s.id) navReplace({ ...nav, screen: { ...s, id: r } });
    }
  }, [store.loaded, nav.screen.name, "id" in nav.screen ? nav.screen.id : ""]);

  // The filters read again what a host's rotation moved.
  const rereadFilters = () => {
    setProjectFilterState(storedGet(FILTER_KEY));
    setHostsOffState(parseHostsOff(storedGet(HOSTS_OFF_KEY)));
  };
  // Decision 0031 2.1, 2.5: a host listed with a new id and the one it had (idWas): what the device keeps under the old
  // id moves to the new one (the keys, the stored timelines, the store, the thread on screen).
  const rotationKey = rotations(fullStore.hostList).map((r) => `${r.was}>${r.now}`).join(",");
  useEffect(() => {
    if (!rotationKey) return;
    for (const { was, now } of rotations(fullStore.hostList)) {
      let kv: Storage | null = null;
      try {
        kv = localStorage;
      } catch {
        /* storage blocked */
      }
      if (moveHostKeys(kv, was, now)) rereadFilters();
      void cache.moveHost(was, now);
      setStore((s) => moveHostInStore(s, was, now));
      setView((v) => (v && moveRef(v.id, was, now) !== v.id ? { ...v, id: moveRef(v.id, was, now) } : v));
      setLastOpen((id) => (id ? moveRef(id, was, now) : id));
      const sc = nav.screen;
      if ((sc.name === "thread" || sc.name === "project") && moveRef(sc.id, was, now) !== sc.id) navReplace({ ...nav, screen: { ...sc, id: moveRef(sc.id, was, now) } });
    }
  }, [rotationKey]);

  // Initial selection and ?open= for screenshots.
  const didInit = useRef(false);
  useEffect(() => {
    if (!store.loaded || didInit.current) return;
    didInit.current = true;
    const want = params.get("open") ? resolveRef(Object.values(store.threads), params.get("open")!) : null;
    const first = want && store.threads[want] ? want : null;
    // On a phone the inbox comes first; V0.status-home: the desktop starts on the status screen. Both open a thread only
    // when asked (?open=).
    if (first) {
      const phone = typeof window.matchMedia === "function" && window.matchMedia(PHONE_QUERY).matches;
      if (!phone) recordOpen(first, "auto");
      setLastOpen(first);
      setView({ id: first, mode: "open" });
      setZone("thread");
      setTracker((tr) => openView(tr, first, "open", Date.now(), document.hasFocus()));
    }
    const d = params.get("dialog");
    if (d === "new" || d === "settings" || d === "changes") setDialog(d);
    if (d === "snooze" && first) {
      setSnoozeTargets([first]);
      setDialog("snooze");
    }
  }, [store.loaded]);

  // Threads whose timeline has older events on the host (API v0 paging).
  const [earlier, setEarlier] = useState<Record<string, boolean>>({});
  const loadEarlier = (id: string) => {
    const first = store.events[id]?.[0]?.seq;
    const host = hostOfId(id);
    if (!first || !host) return;
    void api.eventsBefore(host, id, first, PAGE).then((evs) => {
      setEarlier((m) => ({ ...m, [id]: evs.length >= PAGE }));
      setStore((s) => mergeEvents(s, id, evs));
    });
  };

  // Load the open thread's events; after a reconnect fetch what was missed.
  const openId = view && !gate.gated ? view.id : null;

  // Presence (V0.20): tell the host which thread is shown while visible.
  const beatRef = useRef<BeatState>({ last: "", at: 0 });
  useEffect(() => {
    const device = deviceId();
    const tick = () => {
      const target = presenceTarget(openId, document.visibilityState);
      const now = Date.now();
      if (shouldBeat(beatRef.current, target, now)) {
        beatRef.current = { last: target, at: now };
        const host = target ? hostOfId(target) : undefined;
        void api.presence(host ? { host, threadId: target } : null, device);
      }
    };
    tick();
    const iv = setInterval(tick, 5_000);
    document.addEventListener("visibilitychange", tick);
    window.addEventListener("pagehide", tick);
    return () => {
      clearInterval(iv);
      document.removeEventListener("visibilitychange", tick);
      window.removeEventListener("pagehide", tick);
    };
  }, [openId]);
  useEffect(() => {
    if (!openId || !store.loaded) return;
    let cancelled = false;
    // After a reconnect, from where the timeline stood when the stream dropped (review 2026-11-15).
    const after = resumeSeq(store, openId);
    const known = !!store.events[openId];
    const id = openId;
    void (async () => {
      // V0.38: the device's copy draws at once; the host sends only what is newer.
      const stored = !known && api.cache ? await cache.events(id) : null;
      if (cancelled) return;
      if (stored?.length) setStore((s) => (s.events[id] ? s : mergeEvents(s, id, stored, true)));
      const from = known ? after : stored?.length ? stored[stored.length - 1].seq : 0;
      try {
        const host = hostOfId(id);
        if (!host) throw new Error("no host"); // a thread the store does not keep: nothing to read it from
        const evs = await api.events(host, id, from, PAGE);
        if (cancelled) return;
        if (!known && !stored?.length) setEarlier((m) => ({ ...m, [id]: evs.length >= PAGE }));
        if (!known && stored?.length) {
          const merged = mergeEventLists(stored, evs, PAGE, evs.length >= PAGE);
          if (evs.length >= PAGE) setEarlier((m) => ({ ...m, [id]: true }));
          setStore((s) => mergeEvents(s, id, merged, true));
        } else setStore((s) => resumed(mergeEvents(s, id, evs, !known), id, from));
      } catch {
        if (!cancelled && !known && !stored?.length) setStore((s) => mergeEvents(s, id, [], true));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [openId, store.loaded, store.conn === "live"]);

  // V0.38: keep the open thread's events and the state on the device, when the browser is idle.
  const openEvents = openId ? store.events[openId] : undefined;
  useEffect(() => {
    if (!openId || !openEvents || !api.cache) return;
    const id = openId;
    const t = setTimeout(() => idle(() => void cache.saveEvents(id, openEvents.slice(-PAGE))), 2000);
    return () => clearTimeout(t);
  }, [openId, openEvents]);
  useEffect(() => {
    if (!store.loaded || !api.cache) return;
    const t = setTimeout(
      () =>
        idle(() =>
          void cache.saveState(stateForCache(rawStore, new Date().toISOString())),
        ),
      2000,
    );
    return () => clearTimeout(t);
  }, [rawStore.hosts, rawStore.hostList, rawStore.loaded]);

  // V0.207: the front leaves this device out of the pushes of what it hides. Sent again when push turns on and on a
  // reconnect, so a change made offline arrives.
  const pushHidden = useMemo(() => JSON.stringify(pushHiddenOf(fullStore.hostList, fullStore.projects, deviceHidden)), [fullStore.hostList, fullStore.projects, deviceHidden]);
  useEffect(() => {
    if (pushSt !== "on" || !fullStore.loaded || fullStore.conn === "offline" || !api.pushes) return;
    void syncPushHidden(api, JSON.parse(pushHidden) as PushHidden);
  }, [pushHidden, pushSt, fullStore.loaded, fullStore.conn]);

  // V0.notification-polish: what this device is told about, kept in this browser and sent to the front like the hidden
  // list: again when push turns on and on a reconnect.
  const [pushPrefs, setPushPrefsState] = useState<PushPrefs>(() => parsePushPrefs(storedGet(PUSH_PREFS_KEY)));
  const setPushPrefs = (p: PushPrefs) => {
    setPushPrefsState(p);
    storedSet(PUSH_PREFS_KEY, JSON.stringify(p)); // blocked storage: the choice holds until a reload
  };
  useEffect(() => {
    if (pushSt !== "on" || !fullStore.loaded || fullStore.conn === "offline" || !api.pushes) return;
    void syncPushPrefs(api, pushPrefs);
  }, [pushPrefs.finished, pushPrefs.quotaReset, pushSt, fullStore.loaded, fullStore.conn]);


  return { earlier, loadEarlier, pushPrefs, setPushPrefs };
}
