// The PR tab (SPEC 0.5 V0.43), one component for desktop and phone: the header, one merge status line, the actions,
// then checks (failed first), reviews, unresolved review threads, labels and the description. Merge… opens a small
// dialog; "Ask the agent" buttons send the owner's message through thread.send.

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import { useApi } from "./api-context.ts";
import type { PullRequest, Thread } from "./types.ts";
import { MERGE_METHODS, hasMultipleRepos, headLabel, prChip, shortHead, shortRepo, checkDuration, checksSection, chipStatus, fixCheckMessage, groupChecks, mergeButton, mergeStatus, passedLabel, reviewAskLabel, reviewMessage, reviewWord, threadsByFile, updatedLabel, type MergeMethod, type MergeStatus, type PrCheck, type PrDetail, type PrThread } from "./pr.ts";
import { renderMarkdown } from "./markdown.ts";
import { relTime } from "./format.ts";
import { isAvailable, unavailableReason } from "./store.ts";
import { StateIcon } from "./icons.tsx";
import { Modal } from "./dialogs.tsx";
import { sensitiveLine } from "./sensitive.ts";


interface PrViewProps {
  t: Thread;
  /** all non-dismissed PRs of the thread (V0.multi-pr); the switcher shows when length > 1 */
  prs: PullRequest[];
  /** the currently selected PR */
  pr: PullRequest;
  onSelect(url: string): void;
  now: number;
  /** sends an owner message to the thread (thread.send); resolves true when it was accepted */
  onSend(text: string): Promise<boolean>;
  onDismiss(url: string): void;
  /** opens the Changes tab */
  onChanges(): void;
  phone?: boolean;
}

type Load = { kind: "loading" } | { kind: "error"; error: string } | { kind: "ok"; d: PrDetail };

const STATE_WORD: Record<string, string> = { OPEN: "Open", MERGED: "Merged", CLOSED: "Closed" };

export function PrView(p: PrViewProps) {
  const { t, prs, pr } = p;
  const source = useApi();
  const multiRepo = hasMultipleRepos(prs);
  const [load, setLoad] = useState<Load>({ kind: "loading" });
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<{ text: string; fail?: boolean } | null>(null);
  const [merging, setMerging] = useState(false);
  const gen = useRef(0);
  const later = useRef<ReturnType<typeof setTimeout> | null>(null);

  const fetchDetail = (fresh: boolean) => {
    const g = ++gen.current;
    setBusy(true);
    source.prDetail(t.hostId, t.id, fresh, pr.url).then((r) => {
      if (g !== gen.current) return;
      setBusy(false);
      if ("error" in r) {
        // Data already shown stays; the failed refresh says so.
        setLoad((cur) => (cur.kind === "ok" ? cur : { kind: "error", error: r.error }));
        if (load.kind === "ok") setNote({ text: `Could not refresh: ${r.error}`, fail: true });
      } else setLoad({ kind: "ok", d: r });
    });
  };

  const snap = `${pr.state}|${pr.checks}|${pr.conflict}|${pr.review}`;
  const lastSnap = useRef(snap);
  /* eslint-disable react-hooks/exhaustive-deps -- a new PR or thread loads from scratch; fetchDetail is new each render and snap is recorded as the baseline here; gen and later are a counter and a timer, not elements, so the cleanup wants their newest values */
  useEffect(() => {
    lastSnap.current = snap;
    setLoad({ kind: "loading" });
    setNote(null);
    fetchDetail(false);
    return () => {
      gen.current++;
      if (later.current) clearTimeout(later.current);
    };
  }, [t.id, pr.url]);
  /* eslint-enable react-hooks/exhaustive-deps */
  // The poller's snapshot changed (checks finished, merged elsewhere): read again.
  /* eslint-disable react-hooks/exhaustive-deps -- refreshes when the PR's snapshot changes; fetchDetail is a new function each render */
  useEffect(() => {
    if (lastSnap.current === snap) return;
    lastSnap.current = snap;
    fetchDetail(true);
  }, [snap]);
  /* eslint-enable react-hooks/exhaustive-deps */

  const refreshSoon = (ms: number) => {
    if (later.current) clearTimeout(later.current);
    later.current = setTimeout(() => fetchDetail(true), ms);
  };

  const run = async (action: string, args: Record<string, unknown>, sent: string) => {
    const r = await source.command(t.hostId, action, t.id, args);
    setNote(r.ok ? { text: sent } : { text: r.error ?? "Failed", fail: true });
    if (r.ok) refreshSoon(4000);
    return r.ok;
  };

  const ask = async (text: string, what: string) => {
    const ok = await p.onSend(text);
    setNote(ok ? { text: `Sent to the agent: ${what}.` } : { text: "The message was not sent.", fail: true });
  };

  const d = load.kind === "ok" ? load.d : null;
  const status: MergeStatus = d ? mergeStatus(d) : chipStatus(pr);
  const open = (d?.state ?? pr.state ?? "OPEN") === "OPEN";
  const done = !open;
  const hostReason = unavailableReason(t, "pr.merge");
  // V0.97: a merge is bound to the head shown here; the host refuses it when the head moved.
  const head = d?.headSha || pr.head;
  const mergeReason = !open ? undefined : !status.canMerge ? status.reason : (hostReason ?? (head ? undefined : "Fyr has not read the PR's head yet"));
  const groups = useMemo(() => groupChecks(d?.checks ?? []), [d]);
  const sendReason = unavailableReason(t, "thread.send");
  const title = d?.title || pr.title || t.title;
  const number = d?.number ?? pr.number;
  // V0.sensitive-pr: the change's reason (the agent's, or the GitHub label), shown in red in the Merge dialog.
  const sensitive = t.changes.find((c) => c.pr?.url === pr.url)?.sensitive ?? "";

  return (
    <div class={`prv${p.phone ? " ph" : ""}`}>
      {prs.length > 1 && (
        <div class="prv-sw" role="tablist" aria-label="Pull requests">
          {prs.map((item) => {
            const chip = prChip(item);
            const on = item.url === pr.url;
            const repo = multiRepo ? shortRepo(item.repo) : "";
            return (
              <button
                key={item.url}
                type="button"
                role="tab"
                aria-selected={on}
                class={`prv-swb ${chip.cls}${on ? " on" : ""}`}
                title={chip.title}
                onClick={() => p.onSelect(item.url)}
              >
                {repo && <span class="prv-swr">{repo}</span>}
                <span>{chip.text}</span>
              </button>
            );
          })}
        </div>
      )}
      <header class="prv-h">
        <h2 class="prv-t">
          <a href={pr.url} target="_blank" rel="noopener noreferrer">
            {title}
          </a>{" "}
          <span class="prv-num">{multiRepo && (d?.repo || pr.repo) ? `${d?.repo || pr.repo}#${number}` : `#${number}`}</span>
        </h2>
        <div class="prv-meta">
          <span class={`prv-pill ${(d?.state ?? pr.state ?? "OPEN").toLowerCase()}`}>{STATE_WORD[d?.state ?? pr.state ?? "OPEN"] ?? d?.state}</span>
          {d?.draft && <span class="prv-pill draft">Draft</span>}
          {d?.head && (
            <span class="prv-br" title={`${d.head} → ${d.base}`}>
              <span class="ell">{d.head}</span>
              <span class="arr">→</span>
              <span>{d.base}</span>
            </span>
          )}
          {d?.author && <span class="prv-by">by {d.author}</span>}
          {d && d.changedFiles !== undefined && (
            <button class="prv-size" title="Open the Changes tab" onClick={p.onChanges}>
              <span class="a">+{d.additions}</span> <span class="d">−{d.deletions}</span> · {d.changedFiles === 1 ? "1 file" : `${d.changedFiles} files`}
            </button>
          )}
        </div>
      </header>

      <div class={`prv-status ${status.tone}`} role="status">
        <span class={status.tone === "busy" ? "spin icw" : "icw"}>
          <StateIcon state={toneIcon(status.tone)} size={16} />
        </span>
        <b class="prv-st">{status.text}</b>
        <span class="prv-upd">{busy ? "Updating…" : d ? updatedLabel(d.fetchedAt, p.now) : ""}</span>
        <button class="btn prv-ref" disabled={busy} onClick={() => fetchDetail(true)}>
          Refresh
        </button>
      </div>

      <div class="prv-acts">
        {open && (
          <button class="btn p prv-merge" disabled={!!mergeReason} title={mergeReason ?? "Choose how to merge"} onClick={() => setMerging(true)}>
            {headLabel("Merge", head)}…
          </button>
        )}
        {open && groups.failed.length > 0 && (
          <button class="btn" disabled={!isAvailable(t, "pr.rerunFailed")} title={unavailableReason(t, "pr.rerunFailed") ?? "Re-run the failed workflow runs of the head commit"} onClick={() => void run("pr.rerunFailed", { pr: pr.url }, "Re-running the failed checks. The result shows in the conversation.")}>
            Re-run failed
          </button>
        )}
        <a class="btn" href={pr.url} target="_blank" rel="noopener noreferrer">
          Open on GitHub
        </a>
        {done && isAvailable(t, "thread.dismissPR") && (
          <button class="btn" title="The thread goes on; this PR is no longer shown" onClick={() => p.onDismiss(pr.url)}>
            Dismiss PR
          </button>
        )}
      </div>
      {open && mergeReason && mergeReason !== status.text && <div class="prv-why">Merge is off: {mergeReason}.</div>}
      {note && <div class={`prv-note${note.fail ? " fail" : ""}`}>{note.text}</div>}

      {load.kind === "loading" && <div class="prv-empty">Loading the PR from GitHub…</div>}
      {load.kind === "error" && (
        <div class="prv-empty fail">
          PR details could not be loaded: {load.error}
          <div class="prv-sub">The status above comes from Fyr's last check.</div>
        </div>
      )}
      {d && (
        <>
          <Checks groups={groups} state={d.state} now={p.now} sendReason={sendReason} onAsk={(c) => void ask(fixCheckMessage(c), `fix ${c.name}`)} />
          {d.reviews && d.reviews.length > 0 && (
            <section class="prv-sec">
              <h3>Reviews</h3>
              <ul class="prv-revs">
                {d.reviews.map((r) => (
                  <li key={r.author}>
                    <span class={`prv-rv ${r.state.toLowerCase()}`}>
                      <b>{r.author}</b> {reviewWord(r.state)}
                    </span>
                    {r.at && <span class="prv-age">{relTime(r.at, p.now)} ago</span>}
                  </li>
                ))}
              </ul>
            </section>
          )}
          <Threads threads={d.threads} error={d.threadsError} now={p.now} sendReason={sendReason} onAsk={() => void ask(reviewMessage(d.threads), d.threads.length === 1 ? "address 1 review comment" : `address ${d.threads.length} review comments`)} />
          {d.labels && d.labels.length > 0 && (
            <section class="prv-sec">
              <h3>Labels</h3>
              <div class="prv-labels">
                {d.labels.map((l) => (
                  <span class="prv-label" key={l.name}>
                    <i style={l.color ? { background: `#${l.color}` } : undefined} />
                    {l.name}
                  </span>
                ))}
              </div>
            </section>
          )}
          <Description body={d.body ?? ""} />
        </>
      )}
      {merging && d && <MergeDialog d={d} sha={head} sensitive={sensitive} onClose={() => setMerging(false)} onMerge={(m) => run("pr.merge", { pr: pr.url, method: m, head }, `Merging #${d.number}. The result shows in the conversation.`)} />}
      {merging && !d && <MergeDialog d={{ url: pr.url, repo: pr.repo ?? "", number: pr.number, title, state: "OPEN", checks: [], threads: [] }} sha={head} sensitive={sensitive} onClose={() => setMerging(false)} onMerge={(m) => run("pr.merge", { pr: pr.url, method: m, head }, `Merging #${pr.number}. The result shows in the conversation.`)} />}
    </div>
  );
}

function toneIcon(tone: MergeStatus["tone"]) {
  return tone === "ok" ? "settled" : tone === "fail" ? "failed" : tone === "busy" ? "working" : tone === "warn" ? "blocked" : "seen";
}

const CHECK_ICON = { fail: "failed", running: "working", pass: "settled", skipped: "seen" } as const;

function CheckRow({ c, now, children }: { c: PrCheck; now: number; children?: any }) {
  const dur = checkDuration(c, now);
  return (
    <li class={`prv-ck ${c.state}`}>
      <div class="prv-ckl">
        <span class={c.state === "running" ? "spin icw" : "icw"}>
          <StateIcon state={CHECK_ICON[c.state]} size={14} />
        </span>
        <span class="prv-ckn ell" title={c.workflow ? `${c.workflow} / ${c.name}` : c.name}>
          {c.name}
          {c.workflow && <span class="prv-wf"> · {c.workflow}</span>}
        </span>
        {dur && <span class="prv-dur">{dur}</span>}
        {c.url && (
          <a class="prv-det" href={c.url} target="_blank" rel="noopener noreferrer" aria-label={`Details of ${c.name}`}>
            Details
          </a>
        )}
      </div>
      {children}
    </li>
  );
}

function Checks({ groups, state, now, sendReason, onAsk }: { groups: ReturnType<typeof groupChecks>; state?: string; now: number; sendReason?: string; onAsk(c: PrCheck): void }) {
  const [showPassed, setShowPassed] = useState(false);
  const rest = [...groups.passed, ...groups.skipped];
  const total = groups.failed.length + groups.running.length + rest.length;
  const sec = checksSection(state);
  if (!total)
    return (
      <section class="prv-sec">
        <h3>{sec.heading}</h3>
        <div class="prv-sub">No checks on this PR.</div>
      </section>
    );
  return (
    <section class="prv-sec">
      <h3>{sec.heading}</h3>
      <ul class={`prv-cks${sec.history ? " history" : ""}`}>
        {groups.failed.map((c) => (
          <CheckRow key={`f${c.workflow}/${c.name}`} c={c} now={now}>
            {!sec.history && (
              <button class="prv-ask" disabled={!!sendReason} title={sendReason ?? "Sends a message to the agent with the check's link"} onClick={() => onAsk(c)}>
                Ask the agent to fix this check
              </button>
            )}
          </CheckRow>
        ))}
        {groups.running.map((c) => (
          <CheckRow key={`r${c.workflow}/${c.name}`} c={c} now={now} />
        ))}
        {rest.length > 0 && (
          <li class="prv-ck prv-foldrow">
            <button class="prv-ckl prv-more" aria-expanded={showPassed} onClick={() => setShowPassed(!showPassed)}>
              <span class="icw">
                <StateIcon state="settled" size={14} />
              </span>
              <span>
                {passedLabel(groups)} {showPassed ? "▾" : "▸"}
              </span>
            </button>
          </li>
        )}
        {showPassed && rest.map((c) => <CheckRow key={`p${c.workflow}/${c.name}`} c={c} now={now} />)}
      </ul>
    </section>
  );
}

function Threads({ threads, error, now, sendReason, onAsk }: { threads: PrThread[]; error?: string; now: number; sendReason?: string; onAsk(): void }) {
  const api = useApi();
  const files = useMemo(() => threadsByFile(threads), [threads]);
  const [open, setOpen] = useState<Set<string>>(() => new Set(threads.length <= 3 ? files.map((f) => f.path) : []));
  if (error)
    return (
      <section class="prv-sec">
        <h3>Review comments</h3>
        <div class="prv-sub fail">Review threads could not be loaded: {error}</div>
      </section>
    );
  if (!threads.length) return null;
  const toggle = (path: string) => {
    const n = new Set(open);
    if (n.has(path)) n.delete(path);
    else n.add(path);
    setOpen(n);
  };
  return (
    <section class="prv-sec">
      <h3>{threads.length === 1 ? "1 unresolved review comment" : `${threads.length} unresolved review comments`}</h3>
      <button class="btn prv-askall" disabled={!!sendReason} title={sendReason ?? "Sends one message that lists every comment"} onClick={onAsk}>
        {reviewAskLabel(threads.length)}
      </button>
      <div class="prv-files">
        {files.map((f) => (
          <div class={`prv-file${open.has(f.path) ? " open" : ""}`} key={f.path}>
            <button class="prv-fh" aria-expanded={open.has(f.path)} onClick={() => toggle(f.path)} title={f.path}>
              <span class="prv-fp ell">{f.path}</span>
              <span class="prv-fc">{f.threads.length}</span>
              <span class="prv-chev">{open.has(f.path) ? "▾" : "▸"}</span>
            </button>
            {open.has(f.path) &&
              f.threads.map((th, i) => (
                <div class="prv-th" key={i}>
                  <div class="prv-thh">
                    <span>{th.line ? `line ${th.line}` : "file"}</span>
                    {th.outdated && <span class="prv-out">outdated</span>}
                    {th.url && (
                      <a href={th.url} target="_blank" rel="noopener noreferrer">
                        On GitHub
                      </a>
                    )}
                  </div>
                  {th.comments.map((c, k) => (
                    <div class="prv-cm" key={k}>
                      <div class="prv-cma">
                        <b>{c.author}</b>
                        {c.at && <span class="prv-age">{relTime(c.at, now)} ago</span>}
                      </div>
                      <div class="md prose" dangerouslySetInnerHTML={{ __html: renderMarkdown(c.body, undefined, api.attachmentUrl) }} />
                    </div>
                  ))}
                </div>
              ))}
          </div>
        ))}
      </div>
    </section>
  );
}

function Description({ body }: { body: string }) {
  const api = useApi();
  const text = body.replace(/<!--[\s\S]*?-->/g, "").trim();
  const ref = useRef<HTMLDivElement>(null);
  const [all, setAll] = useState(false);
  const [long, setLong] = useState(false);
  useLayoutEffect(() => {
    const el = ref.current;
    if (el) setLong(el.scrollHeight > el.clientHeight + 4);
  }, [text]);
  return (
    <section class="prv-sec">
      <h3>Description</h3>
      {text ? (
        <>
          <div ref={ref} class={`md prose prv-desc${all ? "" : " cut"}`} dangerouslySetInnerHTML={{ __html: renderMarkdown(text, undefined, api.attachmentUrl) }} />
          {(long || all) && (
            <button class="prv-fold" onClick={() => setAll(!all)}>
              {all ? "Show less ▴" : "Show all ▾"}
            </button>
          )}
        </>
      ) : (
        <div class="prv-sub">No description.</div>
      )}
    </section>
  );
}

function MergeDialog({ d, sha, sensitive, onClose, onMerge }: { d: PrDetail; sha?: string; sensitive?: string; onClose(): void; onMerge(m: MergeMethod): Promise<boolean> }) {
  const [method, setMethod] = useState<MergeMethod>("squash");
  const [sending, setSending] = useState(false);
  const btn = useRef<HTMLButtonElement>(null);
  /* eslint-disable react-hooks/exhaustive-deps -- the focus and the Escape handler are set once, when the dialog opens; onClose is the parent's callback */
  useEffect(() => {
    btn.current?.focus();
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    document.addEventListener("keydown", key, true);
    return () => document.removeEventListener("keydown", key, true);
  }, []);
  /* eslint-enable react-hooks/exhaustive-deps */
  const go = async () => {
    setSending(true);
    await onMerge(method);
    onClose();
  };
  return (
    <Modal title={`Merge #${d.number}`} onClose={onClose}>
      <div class="db prv-dlg" onKeyDown={(e) => e.stopPropagation()}>
        {sensitive && (
          <p class="chsens" role="note">
            {sensitiveLine(sensitive)}
          </p>
        )}
        <p class="lede">
          {d.head && d.base ? (
            <>
              Merges <code>{d.head}</code> into <code>{d.base}</code> on GitHub.
            </>
          ) : (
            "Merges this PR on GitHub."
          )}{" "}
          {sha && (
            <>
              Only commit <code>{shortHead(sha)}</code>: if the branch moves first, Fyr refuses.{" "}
            </>
          )}
          The result shows in the conversation.
        </p>
        <div class="prv-methods" role="radiogroup" aria-label="Merge method">
          {MERGE_METHODS.map((m) => (
            <label class={`prv-m${method === m.id ? " on" : ""}`} key={m.id}>
              <input type="radio" name="merge-method" value={m.id} checked={method === m.id} onChange={() => setMethod(m.id)} />
              <span>
                <b>{m.label}</b>
                <span class="prv-mh">{m.hint}</span>
              </span>
            </label>
          ))}
        </div>
      </div>
      <div class="df prv-df">
        <button class="btn" onClick={onClose}>
          Cancel
        </button>
        <button ref={btn} class={`btn p${sensitive ? " danger" : ""}`} disabled={sending} onClick={() => void go()}>
          {sending ? "Merging…" : mergeButton(method, d.number)}
        </button>
      </div>
    </Modal>
  );
}
