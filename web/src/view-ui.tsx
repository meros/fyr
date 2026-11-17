// The pinned live view above the conversation (SPEC 0.5 V0.37), desktop and phone. The page runs in the same
// sandbox as HTML previews (no origin; CDN libraries only, V0.47); the app sends it the full state on load and on every change,
// and a press in it becomes a message `from: "view"`.

import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { bridgeMessage, liveViewOf, pressReply, staleLabel, stateMessage, viewDimmed, viewHeight } from "./view.ts";
import { FRAME_ALLOW } from "./previews.ts";
import { FsBar, FsButton, useFrameFullscreen } from "./preview-ui.tsx";
import { useApi } from "./api-context.ts";
import type { Thread, Offer } from "./types.ts";

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

/** V0.37: the full view (the View tab). V0.56: no headline bar in Conversation; the tab is the way in. */
export function LiveViewPanel({ t, now, onPress, onAction, full }: { t: Thread; now: number; onPress(text: string): Promise<boolean>; /** V0.60: run one of the view's actions */ onAction?(o: Offer): Promise<boolean>; full?: boolean }) {
  const api = useApi();
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
  const actionRef = useRef({ onAction, offers: t.live?.offers });
  actionRef.current = { onAction, offers: t.live?.offers };
  const stateRef = useRef(v?.state ?? {});
  // V0.165: full screen in place, the same frame (its bridge and state stay).
  const fs = useFrameFullscreen(box);
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
      } else if (m.kind === "action") {
        // V0.60: only an action the agent declared on the view runs; the press carries its digest.
        const id = m.id;
        const { onAction: run, offers } = actionRef.current;
        const o = offers?.find((x) => x.id === m.action);
        void (o && run ? run(o) : Promise.resolve(false)).then((ok) => {
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
    <div class={`lv${dim ? " dim" : ""}${collapsed ? " shut" : ""}${full ? " full" : ""}${fs.on ? " isfs" : ""}`} ref={box} data-testid="live-view">
      {fs.on ? (
        <FsBar key="bar" title={v.headline || "Live view"} fs={fs} />
      ) : (
        <div key="top" class="lv-top">
          <button class="lv-h" onClick={full ? undefined : toggle} aria-expanded={!collapsed} title={full ? v.updatedAt : collapsed ? "Show the live view" : "Hide the live view"}>
            <span class="lv-k">Live view</span>
            {collapsed && v.headline && <span class="lv-hl ell">{v.headline}</span>}
            <span class="lv-s" title={dim ? "The agent is working; this may change" : v.updatedAt}>
              {dim ? `${stamp} · agent working` : stamp}
            </span>
            {!full && <span class="chev">{collapsed ? "▸" : "▾"}</span>}
          </button>
          {!collapsed && <FsButton onPress={fs.enter} />}
        </div>
      )}
      {!collapsed && (
        // keyed: the header's swap with the bar must never take this wrapper, or the frame reloads and loses its state
        <div key="frame" class="fzw">
          <iframe
            ref={frame}
            class="lv-f"
            src={api.previewUrl(t.hostId ?? "", v.pageId)}
            sandbox="allow-scripts"
            allow={FRAME_ALLOW}
            allowFullScreen
            title={v.headline || "Live view"}
            style={fs.frameStyle(full ? undefined : { height: `${viewHeight(reported, pane)}px` })}
            onLoad={sendState}
          />
        </div>
      )}
    </div>
  );
}
