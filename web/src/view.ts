// Live views (SPEC 0.5 V0.37, decision 0010): the pinned view's model, the bridge messages between the view's iframe
// and the app (MCP Apps names), and the inbox row's headline. Pure.

import type { Change, Thread } from "./types.ts";
import { threadPrs } from "./offers.ts";

interface LiveViewDTO {
  pageId: string;
  state: Record<string, unknown>;
  updatedAt: string;
  /** the last view_* call, view_unchanged included */
  checkedAt: string;
  /** state.headline when it is a string */
  headline: string;
}

const str = (v: unknown): string => (typeof v === "string" ? v : "");
const obj = (v: unknown): Record<string, unknown> => (v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {});

/** The thread's view, or null when it has no page yet. */
export function liveViewOf(t: Pick<Thread, "live" | "liveView">): LiveViewDTO | null {
  if (!t.liveView) return null; // turned off: the server keeps the last page, but the View tab and the headline go
  const v = obj(t.live);
  const pageId = str(v.pageId);
  if (!pageId) return null;
  return { pageId, state: obj(v.state), updatedAt: str(v.updatedAt), checkedAt: str(v.checkedAt), headline: str(v.headline) };
}

/** The inbox row's line when nothing more urgent shows: the view's headline, else the last assistant text. */
export function rowSummary(t: Pick<Thread, "live" | "liveView" | "summary">): string {
  return liveViewOf(t)?.headline || t.summary || "";
}

/** Dimmed while a run goes that has not touched the view yet: what it shows may be old. */
export function viewDimmed(t: Pick<Thread, "running" | "runStartedAt" | "live" | "liveView">): boolean {
  if (!t.running) return false;
  const v = liveViewOf(t);
  if (!v) return false;
  const run = Date.parse(t.runStartedAt ?? "");
  const checked = Date.parse(v.checkedAt);
  if (Number.isNaN(run)) return false;
  return Number.isNaN(checked) || checked < run;
}

/** "updated just now", "updated 3 min ago". */
export function staleLabel(updatedAt: string, now: number): string {
  const t = Date.parse(updatedAt);
  if (Number.isNaN(t)) return "";
  const sec = Math.max(0, Math.floor((now - t) / 1000));
  if (sec < 45) return "updated just now";
  const min = Math.round(sec / 60);
  if (min < 60) return `updated ${min} min ago`;
  const h = Math.floor(min / 60);
  if (h < 24) return `updated ${h} h ago`;
  return `updated ${Math.floor(h / 24)} d ago`;
}

/** The view takes at most this share of the pane. */
const VIEW_MAX_SHARE = 0.6;
/** Before the page reports its height. */
const VIEW_START = 120;
const VIEW_MIN = 40;

/** The frame's height: what the page reports, at least 40 px, at most 60 % of the pane. */
export function viewHeight(reported: number, pane: number): number {
  const h = reported > 0 ? Math.ceil(reported) : VIEW_START;
  const cap = pane > 0 ? Math.max(VIEW_MIN, Math.floor(pane * VIEW_MAX_SHARE)) : 480;
  return Math.max(VIEW_MIN, Math.min(h, cap));
}

/** A press carries at most this much text. */
export const PRESS_MAX = 2000;

type BridgeIn = { kind: "ready" } | { kind: "size"; height: number } | { kind: "press"; text: string; id: string | number | null } | { kind: "action"; action: string; id: string | number | null };

/**
 * A message from the view's iframe, or null. Only the view's own frame counts (`source === frame`): any other window,
 * the app's own messages and malformed JSON-RPC are ignored.
 */
export function bridgeMessage(source: unknown, frame: unknown, data: unknown): BridgeIn | null {
  if (!frame || source !== frame) return null;
  const d = obj(data);
  if (d.jsonrpc !== "2.0" || typeof d.method !== "string") return null;
  const p = obj(d.params);
  switch (d.method) {
    case "ui/notifications/initialized":
      return { kind: "ready" };
    case "ui/notifications/size-changed": {
      const h = Number(p.height);
      return Number.isFinite(h) && h >= 0 && h < 100_000 ? { kind: "size", height: h } : null;
    }
    case "ui/message": {
      if (p.role !== undefined && p.role !== "user") return null;
      const blocks = Array.isArray(p.content) ? p.content : [p.content];
      const text = blocks
        .map((b) => obj(b))
        .filter((b) => b.type === "text")
        .map((b) => str(b.text))
        .join("\n")
        .trim();
      if (!text || text.length > PRESS_MAX) return null;
      const id = typeof d.id === "string" || typeof d.id === "number" ? d.id : null;
      return { kind: "press", text, id };
    }
    case "ui/action": {
      // V0.60: data-action / fyr.run(id) runs the view's declared action with that id.
      const action = str(p.action).trim();
      if (!action || action.length > 100) return null;
      const id = typeof d.id === "string" || typeof d.id === "number" ? d.id : null;
      return { kind: "action", action, id };
    }
  }
  return null;
}

/** The host's message with the full state, sent on load and on every change. */
export function stateMessage(state: Record<string, unknown>) {
  return { jsonrpc: "2.0", method: "ui/notifications/state", params: { state } };
}

/** The reply to a press, so a page that waits for one can go on. */
export function pressReply(id: string | number, ok: boolean) {
  return ok ? { jsonrpc: "2.0", id, result: {} } : { jsonrpc: "2.0", id, error: { code: -32000, message: "Not sent" } };
}

/** SPEC 0.5 V0.37: the tab a thread opens on. A live view opens on View; an explicit
 *  choice wins; View does not exist without a view; PR exists whenever the thread has any non-dismissed PR. */
export function defaultTab(t: Pick<Thread, "live" | "liveView" | "pr"> & { changes?: Change[] }, requested: string | undefined): string {
  const has = !!liveViewOf(t);
  if (requested === "view") return has ? "view" : "conv";
  if (requested === "pr" && !threadPrs(t).length) return "conv"; // V0.36: no PR, no PR tab
  return requested ?? (has ? "view" : "conv");
}

type ThreadTab = "view" | "conv" | "changes" | "pr";

const TAB_LABEL: Record<ThreadTab, string> = { view: "View", conv: "Conversation", changes: "Changes", pr: "PR" };

/** V0.multi-pr: the PR tab reads "PRs (N)" when the thread tracks more than one non-dismissed PR. */
export function tabLabel(t: Pick<Thread, "pr"> & { changes?: Change[] }, tab: ThreadTab): string {
  if (tab === "pr") {
    const n = threadPrs(t).length;
    return n > 1 ? `PRs (${n})` : "PR";
  }
  return TAB_LABEL[tab];
}

/** #41: the tabs a thread has, in order, on desktop and phone: View only with a view (V0.37), PR with any non-dismissed PR (V0.36, V0.multi-pr). */
export function threadTabs(t: Pick<Thread, "live" | "liveView" | "pr"> & { changes?: Change[] }): ThreadTab[] {
  return [...(liveViewOf(t) ? (["view"] as const) : []), "conv", "changes", ...(threadPrs(t).length ? (["pr"] as const) : [])];
}

/** SPEC 0.5 V0.37: the tabs a question or approval card shows on; never hidden behind View. */
export function showsDecision(tab: string): boolean {
  return tab === "conv" || tab === "view";
}

/** 13.2, #88: the composer is the question card's answer field wherever the card shows, so Other… always works. */
export function composerAnswers(tab: string): boolean {
  return showsDecision(tab);
}
