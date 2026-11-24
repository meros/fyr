// V0.message-links-files: a file path in a message is a link when the thread's host has the file, and a press opens
// it: text in the file viewer (at its line), an image in the image viewer. One viewer for desktop and phone.

import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { useApi } from "./api-context.ts";
import { formatSize } from "./attach.ts";
import { MAX_VIEW_BYTES, fileLines, knownFile, learnFiles, type FileStat } from "./filelinks.ts";
import { useLayer } from "./fullscreen.ts";
import type { HostId } from "./hostid.ts";
import { baseName, tildePath } from "./paths.ts";
import { DownloadLink, openImages } from "./preview-ui.tsx";
import { CopyButton } from "./tool-calls-ui.tsx";

/** The file the viewer shows. */
interface OpenFile {
  host: HostId;
  threadId: string;
  /** the path as the message wrote it */
  path: string;
  stat: FileStat;
  /** where its bytes are read (Download) */
  src: string;
  line?: number;
}

let setOpenGlobal: ((o: OpenFile | null) => void) | null = null;

/** How long a new path waits before the host is asked: a streaming message names a path a character at a time. */
const ASK_DELAY_MS = 250;

function activate(el: HTMLElement, stat: FileStat) {
  if (el.classList.contains("on")) return;
  el.classList.add("on");
  el.setAttribute("role", "link");
  el.tabIndex = 0;
  el.title = `Open ${tildePath(stat.abs)}`;
}

/**
 * Turns the marked paths (`.md-path`, markdown.ts) inside `box` into links, for those the thread's host has. The
 * markup is set as HTML, so the links are made on the elements; `html` names the render they belong to.
 */
export function useFileLinks(box: { current: HTMLElement | null }, html: string, host: HostId | undefined, threadId: string) {
  const api = useApi();
  useLayoutEffect(() => {
    const el = box.current;
    if (!el || !host || !threadId || !html.includes("md-path")) return;
    const nodes = [...el.querySelectorAll<HTMLElement>(".md-path[data-path]")];
    if (!nodes.length) return;
    const mark = () => {
      const missing: string[] = [];
      for (const n of nodes) {
        const stat = knownFile(host, threadId, n.dataset.path ?? "");
        if (stat) activate(n, stat);
        else if (stat === undefined) missing.push(n.dataset.path ?? "");
      }
      return missing;
    };
    const missing = mark();
    if (!missing.length) return;
    let live = true;
    const timer = setTimeout(() => {
      void learnFiles(host, threadId, missing, (paths) => api.fileStat(host, threadId, paths)).then(() => {
        if (live) mark();
      });
    }, ASK_DELAY_MS);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [html, host, threadId]); // eslint-disable-line react-hooks/exhaustive-deps
}

/** A press (or Enter) on a path link opens its file. Put on the element that holds the rendered markdown. */
export function openFileLink(e: Event, host: HostId | undefined, threadId: string, fileUrl: (host: HostId | undefined, threadId: string, path: string) => string): void {
  if (e instanceof KeyboardEvent && e.key !== "Enter") return;
  const el = (e.target as Element | null)?.closest?.(".md-path.on") as HTMLElement | null;
  const path = el?.dataset.path;
  if (!el || !path || !host) return;
  const stat = knownFile(host, threadId, path);
  if (!stat) return;
  e.preventDefault();
  e.stopPropagation();
  const src = fileUrl(host, threadId, path);
  const name = baseName(stat.abs);
  if (stat.kind === "image") openImages([{ key: src, src, title: name, name }], 0);
  else setOpenGlobal?.({ host, threadId, path, stat, src, line: Number(el.dataset.line) || undefined });
}

type Body = { state: "loading" } | { state: "text"; lines: string[] } | { state: "note"; note: string };

/** The file viewer, mounted once beside the image viewer. */
export function FileViewer() {
  const api = useApi();
  const [open, setOpen] = useState<OpenFile | null>(null);
  const [body, setBody] = useState<Body>({ state: "loading" });
  const hit = useRef<HTMLElement>(null);
  const scroller = useRef<HTMLDivElement>(null);
  useEffect(() => {
    setOpenGlobal = setOpen;
    return () => {
      setOpenGlobal = null;
    };
  }, []);
  const close = useLayer(!!open, () => setOpen(null));

  useEffect(() => {
    if (!open) return;
    const { stat } = open;
    if (stat.kind !== "text") return setBody({ state: "note", note: "This file is not text. Download it to open it." });
    if (stat.size > MAX_VIEW_BYTES) return setBody({ state: "note", note: `This file is ${formatSize(stat.size)}, too large to show here. Download it to read it.` });
    setBody({ state: "loading" });
    const stop = new AbortController();
    void api.fileText(open.host, open.threadId, open.path, stop.signal).then((r) => {
      if (stop.signal.aborted) return;
      setBody(r.ok ? { state: "text", lines: fileLines(r.data) } : { state: "note", note: "The file could not be read: it is gone, or its host does not answer." });
    });
    return () => stop.abort();
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  // The line the message points at is in the middle of the screen once the text is there.
  useLayoutEffect(() => {
    // Only down: scrollIntoView would also scroll sideways to the end of a long line.
    const box = scroller.current;
    if (body.state === "text" && box && hit.current) box.scrollTop = hit.current.offsetTop - box.clientHeight / 2;
  }, [body]);

  if (!open) return null;
  const { stat, src, line } = open;
  const shown = tildePath(stat.abs);
  const at = body.state === "text" && line && line <= body.lines.length ? line : 0;
  return (
    <div class="pv-full fv" role="dialog" aria-label={baseName(stat.abs)}>
      <div class="pv-fh">
        <span class="pv-ft fv-path" title={stat.abs}>
          {/* the name stays in view when the path is cut: the folder shortens from its start (it is laid out right to
              left; the marks keep a leading ~ and the last / in their places) */}
          <span class="fv-dir">{"\u200e" + shown.slice(0, shown.length - baseName(shown).length) + "\u200e"}</span>
          <span class="fv-name">
            {baseName(shown)}
            {line ? `:${line}` : ""}
          </span>
        </span>
        <CopyButton text={stat.abs} label="Copy path" cls="fv-cp" />
        <DownloadLink cls="pv-dl pv-x" src={src} name={baseName(stat.abs)} />
        <button class="pv-x" aria-label="Close" onClick={close}>
          ×
        </button>
      </div>
      {body.state === "text" ? (
        <div class="fv-body" tabIndex={0} ref={scroller}>
          <pre class="fv-nums" aria-hidden="true">
            {body.lines.map((_, i) => i + 1).join("\n")}
          </pre>
          <pre class="fv-text">
            {at ? (
              <>
                {at > 1 ? body.lines.slice(0, at - 1).join("\n") + "\n" : ""}
                <mark class="fv-hit" ref={hit}>
                  {body.lines[at - 1] || " "}
                </mark>
                {at < body.lines.length ? "\n" + body.lines.slice(at).join("\n") : ""}
              </>
            ) : (
              body.lines.join("\n")
            )}
          </pre>
        </div>
      ) : (
        <div class="fv-note">{body.state === "loading" ? "Loading…" : body.note}</div>
      )}
    </div>
  );
}
