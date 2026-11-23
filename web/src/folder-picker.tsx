// The host folder picker (SPEC 0.5 V0.42): one field (a word searches, a path browses and completes), then This
// project, Recent, Repos found and Browse with breadcrumbs and New folder. A dialog on the desktop, a sheet on the phone.

import type { HostId } from "./hostid.ts";
import { createContext } from "preact";
import { useContext, useEffect, useRef, useState } from "preact/hooks";
import type { FsDir, FsListing } from "./types.ts";
import type { Result } from "./api.ts";
import { breadcrumbs, complete, completions, filterSections, folderNameError, isPathQuery, joinPath, splitPath, visibleDirs, type PickerSection } from "./folders.ts";
import { tildePath } from "./paths.ts";

/** The host calls the picker needs (api.ts implements them; none rejects). */
export interface FsApi {
  fsList(path: string): Promise<Result<FsListing>>;
  fsRecent(): Promise<Result<FsDir[]>>;
  fsRepos(): Promise<Result<FsDir[]>>;
  fsMkdir(parent: string, name: string): Promise<Result<string>>;
}

export interface PickOptions {
  title?: string;
  /** rows above Recent, e.g. the project's other folders */
  first?: { title: string; dirs: FsDir[] };
  /** V0.132: the host whose folders it browses, by install id; missing: nothing to browse (slice 2b: no default host) */
  host?: HostId;
}

/** Opens the picker where it belongs (a dialog or a sheet); null when the owner closed it. */
export interface FolderPick {
  pick(o?: PickOptions): Promise<string | null>;
}
export const FolderPickContext = createContext<FolderPick>({ pick: async () => null });
export const useFolderPick = () => useContext(FolderPickContext);

const FolderIcon = ({ repo }: { repo?: boolean }) => (
  <span class={`fpi${repo ? " repo" : ""}`} aria-hidden="true">
    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      {repo ? (
        <>
          <circle cx="6" cy="6" r="2.5" />
          <circle cx="6" cy="18" r="2.5" />
          <circle cx="18" cy="9" r="2.5" />
          <path d="M6 8.5v7M18 11.5c0 3-4 3.5-10 5" />
        </>
      ) : (
        <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
      )}
    </svg>
  </span>
);

export function FolderPicker({ fs, first, onPick, autoFocus = true }: { fs: FsApi; first?: PickOptions["first"]; onPick(path: string): void; autoFocus?: boolean }) {
  const [q, setQ] = useState("");
  const [home, setHome] = useState("");
  const [listing, setListing] = useState<FsListing | null>(null);
  const [recent, setRecent] = useState<FsDir[]>([]);
  const [repos, setRepos] = useState<FsDir[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [naming, setNaming] = useState<string | null>(null);
  const field = useRef<HTMLInputElement>(null);
  const box = useRef<HTMLDivElement>(null);
  const want = useRef("");

  const go = async (path: string, thenQ?: string) => {
    want.current = path;
    const r = await fs.fsList(path);
    if (want.current !== path) return;
    if (!r.ok) return setErr(r.error);
    setListing(r.data);
    setErr(null);
    if (thenQ !== undefined) setQ(thenQ);
    if (!home && (path === "~" || path === "")) setHome(r.data.path);
  };

  /* eslint-disable react-hooks/exhaustive-deps -- the first listings load once, when the picker opens; autoFocus applies to that first render and fs is the caller's file API for the picker's life */
  useEffect(() => {
    if (autoFocus) field.current?.focus();
    void fs.fsList("~").then((r) => (r.ok ? (setHome(r.data.path), setListing((cur) => cur ?? r.data)) : setErr(r.error)));
    void fs.fsRecent().then((r) => setRecent(r.ok ? r.data : []));
    void fs.fsRepos().then((r) => setRepos(r.ok ? r.data : []));
  }, []);
  /* eslint-enable react-hooks/exhaustive-deps */

  const pathMode = isPathQuery(q);
  const typed = pathMode ? splitPath(q) : null;
  // In path mode the listing follows the typed folder.
  /* eslint-disable react-hooks/exhaustive-deps -- follows a typed folder (its path) only; the listing it loads must not trigger it again, and go is a new function each render */
  useEffect(() => {
    if (!typed) return;
    const target = typed.dir;
    if (listing && tildePath(listing.path, home) === tildePath(target.replace(/^~(?=\/|$)/, home), home)) return;
    void go(target);
  }, [typed?.dir]);
  /* eslint-enable react-hooks/exhaustive-deps */
  const here = listing ? tildePath(listing.path, home) : "~";
  const listingMatches = !!(typed && listing && tildePath(listing.path, home) === tildePath(typed.dir.replace(/^~(?=\/|$)/, home), home));
  const children = !listing ? [] : typed ? (listingMatches ? completions(listing.dirs, typed.prefix) : []) : visibleDirs(listing.dirs);

  const sections: PickerSection[] = pathMode
    ? []
    : filterSections(
        [
          ...(first?.dirs.length ? [{ key: "first", title: first.title, dirs: first.dirs }] : []),
          { key: "recent", title: "Recent", dirs: recent },
          { key: "repos", title: "Repos found", dirs: repos ?? [] },
        ],
        q,
        home,
      );
  const browseRows = pathMode ? children : q.trim() ? filterSections([{ key: "b", title: "", dirs: children }], q, home)[0]?.dirs ?? [] : children;

  const enter = (d: FsDir) => {
    setNaming(null);
    if (pathMode) {
      setQ(joinPath(typed!.dir, d.name) + "/");
      field.current?.focus();
    } else void go(d.path, "");
  };
  const rows = () => [...(box.current?.querySelectorAll<HTMLElement>("[data-row]") ?? [])];
  const onFieldKey = (e: KeyboardEvent) => {
    if (e.key === "Tab" && pathMode && !e.shiftKey && listingMatches) {
      const c = complete(q, listing!.dirs);
      if (c !== q) {
        e.preventDefault();
        setQ(c);
      }
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      rows()[0]?.focus();
    } else if (e.key === "Enter" && !e.isComposing) {
      e.preventDefault();
      if (pathMode) {
        if (!listingMatches) return;
        if (typed!.prefix) {
          const m = completions(listing!.dirs, typed!.prefix);
          const exact = m.find((d) => d.name === typed!.prefix) ?? (m.length === 1 ? m[0] : undefined);
          if (exact) enter(exact);
        } else onPick(listing!.path);
      } else {
        const firstRow = rows()[0];
        firstRow?.click();
      }
    }
  };
  const onListKey = (e: KeyboardEvent) => {
    const el = document.activeElement as HTMLElement;
    if (e.key === "ArrowRight" && el?.dataset.enter) {
      e.preventDefault();
      el.click();
      return;
    }
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    e.preventDefault();
    const items = rows();
    const i = items.indexOf(el) + (e.key === "ArrowDown" ? 1 : -1);
    if (i < 0) field.current?.focus();
    else items[Math.min(i, items.length - 1)]?.focus();
  };

  const makeFolder = async () => {
    if (!listing || naming === null) return;
    const bad = folderNameError(naming, listing.dirs.map((d) => d.name));
    if (bad) return setErr(bad);
    const r = await fs.fsMkdir(listing.path, naming.trim());
    if (!r.ok) return setErr(r.error);
    setNaming(null);
    setErr(null);
    await go(r.data, pathMode ? tildePath(r.data, home) + "/" : "");
  };

  const crumbs = listing ? breadcrumbs(listing.path, home) : [];
  return (
    <div class="fpick">
      <input
        ref={field}
        class="pq"
        type="search"
        value={q}
        placeholder="Search, or type a path like ~/src"
        aria-label="Search folders or type a path"
        spellcheck={false}
        autocapitalize="off"
        onInput={(e) => setQ((e.target as HTMLInputElement).value)}
        onKeyDown={onFieldKey}
      />
      <div class="fps" ref={box} onKeyDown={onListKey}>
        {sections.map((s) => (
          <section class="fsec" data-sec={s.key}>
            <h4>{s.title}</h4>
            {s.dirs.map((d) => (
              <button class="fpr" data-row onClick={() => onPick(d.path)} title={tildePath(d.path, home)}>
                <FolderIcon repo={d.repo} />
                <span class="plt">
                  <span class="ell pln">{d.name}</span>
                  <span class="ell pls2">{tildePath(d.path, home)}</span>
                </span>
              </button>
            ))}
          </section>
        ))}
        {!pathMode && repos === null && <div class="fsec plempty">Looking for repos…</div>}
        <section class="fsec browse" data-sec="browse">
          <h4>Browse</h4>
          <div class="fcrumbs" aria-label="Where you are">
            {crumbs.map((c, i) => (
              <>
                {i > 0 && <span class="fsep">›</span>}
                {i === crumbs.length - 1 ? (
                  <b class="ell">{c.label}</b>
                ) : (
                  <button class="fcrumb" onClick={() => void go(c.path, pathMode ? tildePath(c.path, home) + "/" : "")}>
                    {c.label}
                  </button>
                )}
              </>
            ))}
          </div>
          {browseRows.map((d) => (
            <button class="fpr" data-row data-enter="1" onClick={() => enter(d)} title={`Open ${tildePath(d.path, home)}`}>
              <FolderIcon repo={d.repo} />
              <span class="plt">
                <span class="ell pln">{d.name}</span>
              </span>
              <span class="fgo" aria-hidden="true">
                ›
              </span>
            </button>
          ))}
          {listing && !browseRows.length && <div class="plempty">{q.trim() && !pathMode ? `Nothing here matches “${q.trim()}”.` : typed?.prefix ? `No folder starts with “${typed.prefix}”.` : "No folders inside."}</div>}
          {naming === null ? (
            <div class="fbar">
              <button class="btn sm" onClick={() => setNaming("")} disabled={!listing}>
                New folder
              </button>
              <button class="btn sm p" data-use onClick={() => listing && onPick(listing.path)} disabled={!listing}>
                Use {here}
              </button>
            </div>
          ) : (
            <div class="fbar">
              <input
                class="fname"
                value={naming}
                autoFocus
                placeholder={`New folder in ${here}`}
                aria-label={`New folder in ${here}`}
                spellcheck={false}
                autocapitalize="off"
                onInput={(e) => setNaming((e.target as HTMLInputElement).value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.isComposing) (e.preventDefault(), void makeFolder());
                  if (e.key === "Escape") (e.preventDefault(), e.stopPropagation(), setNaming(null));
                }}
              />
              <button class="btn sm" onClick={() => setNaming(null)}>
                Cancel
              </button>
              <button class="btn sm p" onClick={() => void makeFolder()}>
                Make
              </button>
            </div>
          )}
        </section>
        {err && <div class="err prose">{err}</div>}
      </div>
    </div>
  );
}
