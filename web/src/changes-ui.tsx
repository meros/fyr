// The Changes tab (SPEC 0.5 V0.25): the branch diff against its base, read-only. One component for
// desktop and phone: a summary line with Refresh, one row per file, and the file's diff opened in place.
// V0.65: "New since you looked" next to "Whole branch", a Viewed checkbox per file, the agent's notes for
// this version, and the look recorded when the owner leaves the tab.

import type { HostId } from "./hostid.ts";
import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { useApi } from "./api-context.ts";
import { CiCount, placeNotes, useCiNotes } from "./ci-notes-ui.tsx"; // V0.66
import {
  codeNotes,
  defaultMode,
  isViewed,
  lookOf,
  parsePatch,
  rangeDiffRows,
  sinceLabel,
  sinceLine,
  sortFiles,
  splitPath,
  statusMark,
  statusWord,
  summaryLine,
  viewedLine,
  type ChangedFile,
  type ChangesDTO,
  type FilePatchDTO,
  type Look,
  type Marks,
  type RangeRow,
} from "./changes.ts";
import { shortenPath } from "./paths.ts";
import { relTime } from "./format.ts";
import type { Change } from "./types.ts";
import { SlideMark } from "./motion-ui.tsx";


type PatchState = { kind: "loading" } | { kind: "error"; error: string } | { kind: "ok"; p: FilePatchDTO };
type Mode = "since" | "whole";

/** Lines drawn before "Show all": a 400 KB patch is about 10,000 lines. */
const FIRST_LINES = 1500;

export function ChangesView({ host, threadId, changes }: { host: HostId; threadId: string; changes?: Change[] }) {
  const source = useApi();
  const [data, setData] = useState<ChangesDTO | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [open, setOpen] = useState<Set<string>>(new Set());
  const [patches, setPatches] = useState<Record<string, PatchState>>({});
  const [mode, setMode] = useState<Mode>("whole");
  const [marks, setMarks] = useState<Marks>({});
  const gen = useRef(0);
  // V0.65: the look to record when the owner leaves the tab: what it showed last.
  const shown = useRef<{ threadId: string; look: Look } | null>(null);

  const flush = () => {
    const s = shown.current;
    shown.current = null;
    if (s) void source.command(host, "thread.changesSeen", s.threadId, { head: s.look.head, files: s.look.files }).catch(() => {});
  };

  const load = (first = false) => {
    const g = ++gen.current;
    setLoading(true);
    setError(null);
    setPatches({});
    void source.changes(host, threadId).then((r) => {
      if (g !== gen.current) return;
      setLoading(false);
      if (!r.ok) return setError(r.error);
      const c = r.data;
      setData(c);
      setMarks({});
      const look = lookOf(c);
      if (look) shown.current = { threadId, look };
      const m = first ? defaultMode(c) : mode;
      if (first) setMode(m);
      // Files that stay open reload their diff.
      const list = m === "since" && c.since ? c.since.files : (c.files ?? []);
      for (const path of open) if (list.some((f) => f.path === path)) fetchPatch(path, m, c, g);
    });
  };

  const fetchPatch = (path: string, m: Mode, c: ChangesDTO | null = data, g = gen.current) => {
    const key = `${m}:${path}`;
    // The since diff runs from the seen head; a seen commit that is gone falls back to the branch diff.
    const since = m === "since" && c?.since && c.since.mode !== "missing" ? c.since.head : undefined;
    setPatches((p) => ({ ...p, [key]: { kind: "loading" } }));
    void source.filePatch(host, threadId, path, since).then((r) => {
      if (g === gen.current) setPatches((s) => ({ ...s, [key]: r.ok ? { kind: "ok", p: r.data } : { kind: "error", error: r.error } }));
    });
  };

  /* eslint-disable react-hooks/exhaustive-deps -- the diff reloads only when the thread changes; load and flush are new each render, and gen is a counter, not an element, so the cleanup wants its newest value */
  useEffect(() => {
    setData(null);
    setOpen(new Set());
    setMarks({});
    load(true);
    const hide = () => document.visibilityState === "hidden" && flush();
    document.addEventListener("visibilitychange", hide);
    return () => {
      document.removeEventListener("visibilitychange", hide);
      flush();
      gen.current++;
    };
  }, [threadId]);
  /* eslint-enable react-hooks/exhaustive-deps */

  const since = data?.since;
  const showSince = mode === "since" && !!since && since.files.length > 0;
  const files = useMemo(() => sortFiles((showSince ? since?.files : data?.files) ?? []), [data, since, showSince]);
  const notes = useMemo(() => codeNotes(changes, data?.branch), [changes, data?.branch]);
  const m: Mode = showSince ? "since" : "whole";

  const toggle = (f: ChangedFile) => {
    const next = new Set(open);
    if (next.has(f.path)) next.delete(f.path);
    else {
      next.add(f.path);
      const st = patches[`${m}:${f.path}`];
      if (!st || st.kind === "error") fetchPatch(f.path, m);
    }
    setOpen(next);
  };

  const setViewed = (f: ChangedFile, on: boolean) => {
    const blob = f.blob ?? "";
    setMarks((x) => ({ ...x, [f.path]: on ? blob : null }));
    if (on && open.has(f.path)) {
      const next = new Set(open);
      next.delete(f.path);
      setOpen(next);
    }
    void source.command(host, "thread.fileViewed", threadId, { path: f.path, blob, viewed: on }).catch(() => setMarks((x) => ({ ...x, [f.path]: on ? null : blob })));
  };

  const head = (text: string) => (
    <div class="chg-head">
      <span class="chg-sum ell" title={text}>
        {text}
      </span>
      <button class="btn chg-ref" disabled={loading} onClick={() => load()}>
        {loading ? "Loading…" : "Refresh"}
      </button>
    </div>
  );

  if (error && !data) {
    return (
      <div class="chg">
        {head("Changes could not be loaded")}
        <div class="chg-empty">{error}</div>
      </div>
    );
  }
  if (!data) {
    return (
      <div class="chg">
        <div class="chg-empty">Loading changes…</div>
      </div>
    );
  }
  if (!data.git) {
    // #72: without git there is no diff to refresh; one quiet line, and the change cards above keep the tab's height.
    return (
      <div class="chg nogit">
        <div class="chg-empty">No git checkout in this folder, so there is no diff.</div>
      </div>
    );
  }
  const now = Date.now();
  const vline = viewedLine(files, marks);
  return (
    <div class="chg">
      {head(summaryLine(data))}
      {error && <div class="chg-empty fail">{error}</div>}
      {since && since.files.length > 0 ? (
        <div class="chg-modes">
          <div class="chg-seg" role="group" aria-label="Which changes">
            <SlideMark at={m} />
            <button class={`chg-mode${m === "since" ? " on" : ""}`} aria-pressed={m === "since"} onClick={() => setMode("since")}>
              {sinceLabel(since)}
            </button>
            <button class={`chg-mode${m === "whole" ? " on" : ""}`} aria-pressed={m === "whole"} onClick={() => setMode("whole")}>
              Whole branch
            </button>
          </div>
          {vline && <span class="chg-vcount tnum">{vline}</span>}
        </div>
      ) : (
        vline && (
          <div class="chg-modes">
            {since && <span class="chg-since ell">Nothing new since you looked {relTime(since.at, now)} ago</span>}
            <span class="chg-vcount tnum">{vline}</span>
          </div>
        )
      )}
      {showSince && since && <div class="chg-since">{sinceLine(since, now)}</div>}
      {showSince && since?.mode === "rebased" && since.rangeDiff && <RangeDiff rows={rangeDiffRows(since.rangeDiff)} truncated={!!since.rangeDiffTruncated} />}
      {notes && (
        <section class="chg-notes" aria-label="What this version changes">
          <h4>What this version changes</h4>
          <ul>
            {notes.revisions.map((r) => (
              <li key={r.id}>
                <code class="chg-rid">{r.id.replace(/^sha256:/, "").slice(0, 7)}</code>
                <span class="chg-rs">{r.summary}</span>
                <span class="chg-ra">{relTime(r.at, now)}</span>
              </li>
            ))}
          </ul>
        </section>
      )}
      {files.length === 0 && <div class="chg-empty">{data.base ? `No changes against ${data.base}.` : "No uncommitted changes."}</div>}
      <div class="chg-list">
        {files.map((f) => (
          <FileRow key={f.path} f={f} open={open.has(f.path)} viewed={isViewed(f, marks)} state={patches[`${m}:${f.path}`]} onToggle={() => toggle(f)} onViewed={(on) => setViewed(f, on)} />
        ))}
      </div>
      {data.truncated && <div class="chg-empty">Only the first 1,000 files are listed.</div>}
    </div>
  );
}

const RANGE_WORD: Record<RangeRow["kind"], string> = { same: "kept", changed: "changed", added: "added", dropped: "dropped" };

/** V0.65: the commits after a rebase, as range-diff pairs them; a changed one folds its interdiff. */
function RangeDiff({ rows, truncated }: { rows: RangeRow[]; truncated: boolean }) {
  if (!rows.length) return null;
  return (
    <section class="chg-range" aria-label="Commits since you looked">
      {rows.map((r, i) =>
        r.body ? (
          <details key={i} class={`chg-rr ${r.kind}`}>
            <summary>
              <span class="chg-rk">{RANGE_WORD[r.kind]}</span>
              <span class="chg-rsub ell">{r.subject}</span>
            </summary>
            <pre class="chg-rbody">{r.body}</pre>
          </details>
        ) : (
          <div key={i} class={`chg-rr ${r.kind}`}>
            <span class="chg-rk">{RANGE_WORD[r.kind]}</span>
            <span class="chg-rsub ell">{r.subject}</span>
          </div>
        ),
      )}
      {truncated && <div class="chg-note">The list is cut at 400 KB.</div>}
    </section>
  );
}

function FileRow({ f, open, viewed, state, onToggle, onViewed }: { f: ChangedFile; open: boolean; viewed: boolean; state: PatchState | undefined; onToggle(): void; onViewed(on: boolean): void }) {
  const { dir, name } = splitPath(f.path);
  const title = f.oldPath ? `${f.path} (renamed from ${f.oldPath})` : f.path;
  return (
    <div class={`chg-item${open ? " open" : ""}${viewed ? " viewed" : ""}`}>
      <div class="chg-row">
        <button class="chg-file" aria-expanded={open} title={title} onClick={onToggle}>
          <span class={`chg-st ${f.status}`} title={statusWord(f.status)} aria-label={statusWord(f.status)}>
            {statusMark(f.status)}
          </span>
          <span class="chg-path">
            {dir && <span class="chg-dir">{dir}</span>}
            <span class="chg-name">{name}</span>
          </span>
          <span class="chg-n tnum">
            {f.binary ? (
              <span class="chg-bin">binary</span>
            ) : (
              <>
                <span class="a">+{f.additions}</span> <span class="d">−{f.deletions}</span>
              </>
            )}
          </span>
          <CiCount path={f.path} />
        </button>
        <label class="chg-vw" title="Viewed">
          <input type="checkbox" checked={viewed} aria-label={`Viewed ${f.path}`} onChange={(e) => onViewed((e.currentTarget as HTMLInputElement).checked)} />
          <span class="chg-vwt">Viewed</span>
        </label>
      </div>
      {open && <FileDiff f={f} state={state} />}
    </div>
  );
}

function FileDiff({ f, state }: { f: ChangedFile; state: PatchState | undefined }) {
  const [all, setAll] = useState(false);
  const ci = useCiNotes(f.path);
  const lines = useMemo(() => (state?.kind === "ok" ? parsePatch(state.p.patch).filter((l) => l.kind !== "meta") : []), [state]);
  const renamed = f.oldPath ? <div class="chg-note">Renamed from {shortenPath(f.oldPath, 80)}</div> : null;
  if (!state || state.kind === "loading") return <div class="chg-note">Loading the diff…</div>;
  if (state.kind === "error") return <div class="chg-note fail">{state.error}</div>;
  if (state.p.binary || f.binary)
    return (
      <>
        {renamed}
        <div class="chg-note">Binary file, not shown.</div>
      </>
    );
  if (!lines.length)
    return (
      <>
        {renamed}
        <div class="chg-note">{f.oldPath ? "The content did not change." : "No changes in this file now; Refresh to update the list."}</div>
      </>
    );
  const shown = placeNotes(all ? lines : lines.slice(0, FIRST_LINES), ci);
  return (
    <>
      {renamed}
      <div class="chg-diff" role="region" aria-label={`Diff of ${f.path}`} tabIndex={0}>
        <div class="chg-lines">
          {shown.map((l, i) =>
            l.kind === "hunk" ? (
              <div key={i} class="dl h">
                <span class="ln" />
                <span class="ln" />
                <span class="tx">{l.text}</span>
              </div>
            ) : l.kind === "note" ? (
              <div key={i} class="dl nt">
                <span class="ln" />
                <span class="ln" />
                <span class="tx">{l.text}</span>
              </div>
            ) : (
              <div key={i} class={`dl ${l.kind}`}>
                <span class="ln">{l.old ?? ""}</span>
                <span class="ln">{l.new ?? ""}</span>
                <span class="tx">
                  <span class="sg">{l.kind === "add" ? "+" : l.kind === "del" ? "−" : " "}</span>
                  {l.text}
                </span>
              </div>
            ),
          )}
        </div>
      </div>
      {!all && lines.length > FIRST_LINES && (
        <button class="btn chg-more" onClick={() => setAll(true)}>
          Show all {lines.length.toLocaleString("en-US")} lines
        </button>
      )}
      {state.p.truncated && <div class="chg-note">The diff is cut at 400 KB.</div>}
    </>
  );
}
