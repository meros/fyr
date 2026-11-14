// The pinned live view above the conversation (SPEC 0.5 V0.37), desktop and phone. The page runs in the same
// sandbox as HTML previews (no origin, no network); the app sends it the full state on load and on every change,
// and a press in it becomes a message `from: "view"`.

import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { bridgeMessage, liveViewOf, pressReply, staleLabel, stateMessage, viewDimmed, viewHeight } from "./view.ts";
import { previewUrl } from "./previews.ts";
import type { ThreadDTO } from "./types.ts";

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

export function LiveViewPanel({ t, now, onPress }: { t: ThreadDTO; now: number; onPress(text: string): Promise<boolean> }) {
  const v = liveViewOf(t);
  const frame = useRef<HTMLIFrameElement>(null);
  const box = useRef<HTMLDivElement>(null);
  const [reported, setReported] = useState(0);
  const [pane, setPane] = useState(0);
  const [collapsed, setCollapsed] = useState(() => loadCollapsed().has(t.id));
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
    if (collapsed) s.delete(t.id);
    else s.add(t.id);
    saveCollapsed(s);
    setCollapsed(!collapsed);
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
    <div class={`lv${dim ? " dim" : ""}${collapsed ? " shut" : ""}`} ref={box} data-testid="live-view">
      <button class="lv-h" onClick={toggle} aria-expanded={!collapsed} title={collapsed ? "Show the live view" : "Hide the live view"}>
        <span class="lv-k">Live view</span>
        {collapsed && v.headline && <span class="lv-hl ell">{v.headline}</span>}
        <span class="lv-s" title={dim ? "The agent is working; this may change" : v.updatedAt}>
          {dim ? `${stamp} · agent working` : stamp}
        </span>
        <span class="chev">{collapsed ? "▸" : "▾"}</span>
      </button>
      {!collapsed && (
        <iframe
          ref={frame}
          class="lv-f"
          src={previewUrl(v.pageId)}
          sandbox="allow-scripts"
          title={v.headline || "Live view"}
          style={{ height: `${viewHeight(reported, pane)}px` }}
          onLoad={sendState}
        />
      )}
    </div>
  );
}
