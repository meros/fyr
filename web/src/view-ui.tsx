// The pinned live view above the conversation (SPEC 0.5 V0.37), desktop and phone. The page runs in the same
// sandbox as HTML previews (no origin; CDN libraries only, V0.47); the app sends it the full state on load and on every change,
// and a press in it becomes a message `from: "view"`.

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import { bridgeMessage, liveViewOf, pressReply, staleLabel, stateMessage, viewDimmed, viewHeight } from "./view.ts";
import { FRAME_ALLOW, previewUrl } from "./previews.ts";
import type { FyrEvent, ThreadDTO } from "./types.ts";
import { buildConversation, deliveryLabel, viewChat } from "./conversation.ts";
import { liveLine } from "./tasks.ts";

const COLLAPSED_KEY = "fyr.view.collapsed";

function loadCollapsed(): Set<string> {
  try {
    return new Set(JSON.parse(localStorage.getItem(COLLAPSED_KEY) ?? "[]") as string[]);
  } catch {
    return new Set();
  }
}

function saveCollapsed(s: Set<string>) {
  try {
    localStorage.setItem(COLLAPSED_KEY, JSON.stringify([...s].slice(-200)));
  } catch {}
}

/** V0.37: the full view (the View tab), or one headline bar above the conversation that opens it. */
export function LiveViewBar({ t, now, onOpen }: { t: ThreadDTO; now: number; onOpen(): void }) {
  const v = liveViewOf(t);
  if (!v) return null;
  const dim = viewDimmed(t);
  const stamp = staleLabel(v.updatedAt, now);
  return (
    <div class={`lv shut${dim ? " dim" : ""}`} data-testid="live-view-bar">
      <button class="lv-h" onClick={onOpen} title="Open the live view">
        <span class="lv-k">Live view</span>
        {v.headline && <span class="lv-hl ell">{v.headline}</span>}
        <span class="lv-s">{dim ? `${stamp} · agent working` : stamp}</span>
        <span class="chev">▸</span>
      </button>
    </div>
  );
}

export function LiveViewPanel({ t, now, onPress, full }: { t: ThreadDTO; now: number; onPress(text: string): Promise<boolean>; full?: boolean }) {
  const v = liveViewOf(t);
  const frame = useRef<HTMLIFrameElement>(null);
  const box = useRef<HTMLDivElement>(null);
  const [reported, setReported] = useState(0);
  const [pane, setPane] = useState(0);
  const [shut, setCollapsed] = useState(() => loadCollapsed().has(t.id));
  const collapsed = !full && shut;
  const stateJson = JSON.stringify(v?.state ?? {});
  const pressRef = useRef(onPress);
  pressRef.current = onPress;
  const stateRef = useRef(v?.state ?? {});
  stateRef.current = v?.state ?? {};

  useEffect(() => {
    setCollapsed(loadCollapsed().has(t.id));
    setReported(0);
  }, [t.id]);

  const toggle = () => {
    const s = loadCollapsed();
    if (shut) s.delete(t.id);
    else s.add(t.id);
    saveCollapsed(s);
    setCollapsed(!shut);
  };

  const sendState = () => frame.current?.contentWindow?.postMessage(stateMessage(stateRef.current), "*");

  // The pane's height caps the view (60 %).
  useLayoutEffect(() => {
    const host = box.current?.parentElement;
    if (!host) return;
    const measure = () => setPane(host.clientHeight);
    measure();
    const ro = typeof ResizeObserver !== "undefined" ? new ResizeObserver(measure) : null;
    ro?.observe(host);
    return () => ro?.disconnect();
  }, [!!v]);

  useEffect(() => {
    const on = (e: MessageEvent) => {
      const m = bridgeMessage(e.source, frame.current?.contentWindow, e.data);
      if (!m) return;
      if (m.kind === "ready") sendState();
      else if (m.kind === "size") setReported(m.height);
      else if (m.kind === "press") {
        const id = m.id;
        void pressRef.current(m.text).then((ok) => {
          if (id !== null) frame.current?.contentWindow?.postMessage(pressReply(id, ok), "*");
        });
      }
    };
    window.addEventListener("message", on);
    return () => window.removeEventListener("message", on);
  }, []);

  useEffect(sendState, [stateJson, v?.pageId, collapsed]);

  if (!v) return null;
  const dim = viewDimmed(t);
  const stamp = staleLabel(v.updatedAt, now);
  return (
    <div class={`lv${dim ? " dim" : ""}${collapsed ? " shut" : ""}${full ? " full" : ""}`} ref={box} data-testid="live-view">
      <button class="lv-h" onClick={full ? undefined : toggle} aria-expanded={!collapsed} title={full ? v.updatedAt : collapsed ? "Show the live view" : "Hide the live view"}>
        <span class="lv-k">Live view</span>
        {collapsed && v.headline && <span class="lv-hl ell">{v.headline}</span>}
        <span class="lv-s" title={dim ? "The agent is working; this may change" : v.updatedAt}>
          {dim ? `${stamp} · agent working` : stamp}
        </span>
        {!full && <span class="chev">{collapsed ? "▸" : "▾"}</span>}
      </button>
      {!collapsed && (
        <iframe
          ref={frame}
          class="lv-f"
          src={previewUrl(v.pageId, t.host)}
          sandbox="allow-scripts"
          allow={FRAME_ALLOW}
          allowFullScreen
          title={v.headline || "Live view"}
          style={full ? undefined : { height: `${viewHeight(reported, pane)}px` }}
          onLoad={sendState}
        />
      )}
    </div>
  );
}

/** SPEC 0.5 V0.49: the View tab's chat: your last message typed here, its state, and the agent's
 *  spoken reply (or its current step while it works). A tap opens Conversation. */
export function ViewChat({ t, events, now, onOpen }: { t: ThreadDTO; events: FyrEvent[] | undefined; now: number; onOpen(): void }) {
  const outKey = (t.outbox ?? []).map((o) => o.id).join(",");
  const c = useMemo(() => viewChat(buildConversation(events ?? [], { outbox: outKey ? outKey.split(",") : [] })), [events, outKey]);
  if (!c) return null;
  const state = deliveryLabel(c.mine);
  const working = !c.reply && t.running;
  return (
    <button class="vchat" onClick={onOpen} title="Open the conversation">
      <span class="vchat-me">
        <span class="vchat-b">{c.mine.text}</span>
        {state && <span class="vchat-s">{state}</span>}
      </span>
      {c.reply ? (
        <span class="vchat-re">{c.reply}</span>
      ) : working ? (
        <span class="vchat-re vchat-live">{liveLine(t, now)}</span>
      ) : null}
    </button>
  );
}
