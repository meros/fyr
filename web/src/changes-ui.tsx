// The Changes tab (SPEC 0.5 V0.25): the branch diff against its base, read-only. One component for
// desktop and phone: a summary line with Refresh, one row per file, and the file's diff opened in place.

import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { parsePatch, shortenPath, sortFiles, splitPath, statusMark, statusWord, summaryLine, type ChangedFile, type ChangesDTO, type FilePatchDTO } from "./changes.ts";

export interface ChangesSource {
  changes(threadId: string): Promise<ChangesDTO>;
  filePatch(threadId: string, path: string): Promise<FilePatchDTO>;
}

let source: ChangesSource | null = null;
/** The app hands in the API once (api.ts stays the only HTTP module). */
export function setChangesSource(s: ChangesSource): void {
  source = s;
}

type PatchState = { kind: "loading" } | { kind: "error"; error: string } | { kind: "ok"; p: FilePatchDTO };

/** Lines drawn before "Show all": a 400 KB patch is about 10,000 lines. */
const FIRST_LINES = 1500;

function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

export function ChangesView({ threadId }: { threadId: string }) {
  const [data, setData] = useState<ChangesDTO | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [open, setOpen] = useState<Set<string>>(new Set());
  const [patches, setPatches] = useState<Record<string, PatchState>>({});
  const gen = useRef(0);

  const load = () => {
    if (!source) return;
    const g = ++gen.current;
    setLoading(true);
    setError(null);
    setPatches({});
    source.changes(threadId).then(
      (c) => {
        if (g !== gen.current) return;
        setData(c);
        setLoading(false);
        // Files that stay open reload their diff.
        for (const path of open) if (c.files?.some((f) => f.path === path)) fetchPatch(path, g);
      },
      (e) => {
        if (g !== gen.current) return;
        setError(message(e));
        setLoading(false);
      },
    );
  };

  const fetchPatch = (path: string, g = gen.current) => {
    if (!source) return;
    setPatches((m) => ({ ...m, [path]: { kind: "loading" } }));
    source.filePatch(threadId, path).then(
      (p) => g === gen.current && setPatches((m) => ({ ...m, [path]: { kind: "ok", p } })),
      (e) => g === gen.current && setPatches((m) => ({ ...m, [path]: { kind: "error", error: message(e) } })),
    );
  };

  useEffect(() => {
    setData(null);
    setOpen(new Set());
    load();
    return () => {
      gen.current++;
    };
  }, [threadId]);

  const toggle = (f: ChangedFile) => {
    const next = new Set(open);
    if (next.has(f.path)) next.delete(f.path);
    else {
      next.add(f.path);
      if (!patches[f.path] || patches[f.path].kind === "error") fetchPatch(f.path);
    }
    setOpen(next);
  };

  const files = useMemo(() => sortFiles(data?.files ?? []), [data]);

  const head = (text: string) => (
    <div class="chg-head">
      <span class="chg-sum ell" title={text}>
        {text}
      </span>
      <button class="btn chg-ref" disabled={loading} onClick={load}>
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
    return (
      <div class="chg">
        {head("Not a git checkout")}
        <div class="chg-empty">This thread's folder is not a git checkout.</div>
      </div>
    );
  }
  return (
    <div class="chg">
      {head(summaryLine(data))}
      {error && <div class="chg-empty fail">{error}</div>}
      {files.length === 0 && <div class="chg-empty">{data.base ? `No changes against ${data.base}.` : "No uncommitted changes."}</div>}
      <div class="chg-list">
        {files.map((f) => (
          <FileRow key={f.path} f={f} open={open.has(f.path)} state={patches[f.path]} onToggle={() => toggle(f)} />
        ))}
      </div>
      {data.truncated && <div class="chg-empty">Only the first 1,000 files are listed.</div>}
    </div>
  );
}

function FileRow({ f, open, state, onToggle }: { f: ChangedFile; open: boolean; state: PatchState | undefined; onToggle(): void }) {
  const { dir, name } = splitPath(f.path);
  const title = f.oldPath ? `${f.path} (renamed from ${f.oldPath})` : f.path;
  return (
    <div class={`chg-item${open ? " open" : ""}`}>
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
      </button>
      {open && <FileDiff f={f} state={state} />}
    </div>
  );
}

function FileDiff({ f, state }: { f: ChangedFile; state: PatchState | undefined }) {
  const [all, setAll] = useState(false);
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
  const shown = all ? lines : lines.slice(0, FIRST_LINES);
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
