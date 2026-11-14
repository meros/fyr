// Thread pane: header with chips and tabs, conversation, docked decision card and composer
// (design/directions/04-rail.html).

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import type { Decision, FyrEvent, ImageArg, Profile, PullRequest, ThreadDTO } from "./types.ts";
import { buildConversation, toolRows, type Item, type ToolCall } from "./conversation.ts";
import { renderMarkdown } from "./markdown.ts";
import { StateIcon, stateClass, stateLabel } from "./icons.tsx";
import { duration, shortModel } from "./format.ts";
import { pick, questionsOf, goTo, type CardState } from "./question.ts";
import { isAvailable, unavailableReason } from "./store.ts";

export function prLabel(pr: PullRequest, short = false): { text: string; cls: string } {
  if (pr.state === "MERGED") return { text: short ? "merged" : `#${pr.number} merged`, cls: "merged" };
  if (pr.state === "CLOSED") return { text: short ? "closed" : `#${pr.number} closed`, cls: "merged" };
  if (pr.conflict) return { text: `#${pr.number} ✗ conflict`, cls: "fail" };
  if (pr.checks === "fail") return { text: `#${pr.number} ✗ CI`, cls: "fail" };
  if (pr.checks === "pass") return { text: `#${pr.number} ✓`, cls: "ok" };
  if (pr.checks === "pending") return { text: `#${pr.number} …`, cls: "" };
  return { text: `#${pr.number}`, cls: "" };
}

export interface AnswerField {
  index: number;
  text: string;
  setText(text: string): void;
  /** Enter: take the text as this question's answer, then submit or move on */
  submit(): void;
}

export interface ThreadPaneProps {
  thread: ThreadDTO;
  mode: "open" | "peek";
  events: FyrEvent[] | undefined;
  profiles: Profile[];
  card: CardState | null;
  answerField: AnswerField | null;
  onCard(cs: CardState): void;
  onAllow(d: Decision, allow: boolean): void;
  onRun(action: string, args?: Record<string, unknown>): void;
  onSnooze(): void;
  onSend(text: string, images: ImageArg[]): Promise<boolean>;
  onAtEnd(atEnd: boolean): void;
  onFocusZone(): void;
  /** Loads older events; absent when the start of the thread is loaded. */
  onEarlier?: () => void;
  /** Phone layout: back to the inbox. */
  onBack?: () => void;
}

type Tab = "conv" | "changes" | "pr";

export function ThreadPane(p: ThreadPaneProps) {
  const t = p.thread;
  const [tab, setTab] = useState<Tab>("conv");
  useEffect(() => setTab("conv"), [t.id]);
  const profile = p.profiles.find((x) => x.id === t.profile);
  const decision = t.decisions?.[0] ?? null;
  const st = t.attention.state;
  const settleReason = unavailableReason(t, "thread.settle");
  const snoozeReason = unavailableReason(t, "thread.snooze");
  return (
    <section class="th" onMouseDown={p.onFocusZone}>
      <div class="thh">
        <div class="tr">
          {p.onBack && (
            <button class="btn back" onClick={p.onBack} aria-label="Back to the inbox">
              ‹
            </button>
          )}
          <div class="tt" title={t.title}>
            {t.title || "Untitled"}
          </div>
          <div class="acts">
            {st === "failed" && (
              <button class="btn" disabled={!isAvailable(t, "thread.retry")} title={unavailableReason(t, "thread.retry") ?? "Retry (r)"} onClick={() => p.onRun("thread.retry")}>
                Retry <kbd>r</kbd>
              </button>
            )}
            <button class="btn" disabled={!!snoozeReason} title={snoozeReason ?? "Snooze (s)"} onClick={p.onSnooze}>
              Snooze <kbd>s</kbd>
            </button>
            <button class="btn" disabled={!!settleReason} title={settleReason ?? (t.running ? "Settle when done (e)" : "Settle (e)")} onClick={() => p.onRun("thread.settle")}>
              Settle <kbd>e</kbd>
            </button>
          </div>
        </div>
        <div class="chips">
          <span class={`chip st ${stateClass(st)}`} title={t.attention.reason || stateLabel(st)}>
            <span class={t.running ? "spin" : "icw"}>
              <StateIcon state={st} size={14} />
            </span>
            <span style="color:var(--ink)">{stateLabel(st)}</span>
          </span>
          {p.mode === "peek" && (
            <span class="chip peek">
              <span>Peek · ⏎ opens</span>
            </span>
          )}
          <span class="chip pj" title={`${t.cwd}${profile ? ` · ${profile.name}` : ""}`}>
            {profile && <i class="dot" style={{ background: profile.color }} />}
            <span>{t.project || t.cwd}</span>
          </span>
          {t.branch && (
            <span class="chip m brn" title={t.branch}>
              <span>{t.branch}</span>
            </span>
          )}
          <ModelChip t={t} profile={profile} onRun={p.onRun} />
          {t.pr && <PrChip pr={t.pr} />}
          {(t.settleWhenDone || t.settleWhenMerged) && (
            <span class="chip">
              <span>{t.settleWhenDone ? "settles when done" : "settles when merged"}</span>
            </span>
          )}
        </div>
        <div class="tabs" role="tablist">
          <span class={tab === "conv" ? "on" : ""} role="tab" onClick={() => setTab("conv")}>
            Conversation
          </span>
          <span class={tab === "changes" ? "on" : ""} role="tab" onClick={() => setTab("changes")}>
            Changes
          </span>
          <span class={tab === "pr" ? "on" : ""} role="tab" onClick={() => setTab("pr")}>
            PR
          </span>
        </div>
      </div>
      {tab === "conv" && <Conversation events={p.events} onAtEnd={p.onAtEnd} threadId={t.id} onEarlier={p.onEarlier} />}
      {tab === "changes" && <div class="pane-note">The branch diff comes in a later version (v0.x). The Conversation tab lists every edit.</div>}
      {tab === "pr" && <PrTab t={t} />}
      {tab === "conv" && decision && <DecisionCard d={decision} more={(t.decisions?.length ?? 1) - 1} card={p.card} onCard={p.onCard} onAllow={p.onAllow} />}
      <Composer key={t.id} t={t} onSend={p.onSend} onInterrupt={() => p.onRun("thread.interrupt")} answer={tab === "conv" ? p.answerField : null} />
    </section>
  );
}

function PrTab({ t }: { t: ThreadDTO }) {
  if (!t.pr) return <div class="pane-note">No pull request is linked to this thread.</div>;
  const pr = t.pr;
  return (
    <div class="pane-note prose">
      <p>
        <a href={pr.url} target="_blank" rel="noopener noreferrer">
          {pr.repo ? `${pr.repo}#${pr.number}` : `#${pr.number}`}
        </a>
        {pr.title ? ` · ${pr.title}` : ""}
      </p>
      <p>
        State {pr.state ?? "unknown"} · checks {pr.checks ?? "none"}
        {pr.review ? ` · review ${pr.review}` : ""}
        {pr.conflict ? " · merge conflict" : ""}
      </p>
      <p>Checks, review threads and Merge in this tab come in a later version (v0.x).</p>
    </div>
  );
}

function ModelChip({ t, profile, onRun }: { t: ThreadDTO; profile: Profile | undefined; onRun(a: string, args?: Record<string, unknown>): void }) {
  const models = profile?.models ?? [];
  const reason = unavailableReason(t, "thread.setModel");
  const can = models.length > 1 && !reason;
  return (
    <span class={`chip m${can ? " msel" : ""}`} title={can ? `${t.model} · change model` : reason ? `${t.model} · ${reason}` : t.model}>
      <span>{shortModel(t.model)}</span>
      {can && (
        <select value={t.model} aria-label="Model" onChange={(e) => onRun("thread.setModel", { model: (e.target as HTMLSelectElement).value })}>
          {!models.includes(t.model) && <option value={t.model}>{shortModel(t.model)}</option>}
          {models.map((m) => (
            <option value={m}>{shortModel(m)}</option>
          ))}
        </select>
      )}
    </span>
  );
}

function PrChip({ pr }: { pr: PullRequest }) {
  const l = prLabel(pr);
  return (
    <a class={`chip pr ${l.cls}`} href={pr.url} target="_blank" rel="noopener noreferrer" title={pr.title || pr.url}>
      {l.text}
    </a>
  );
}

// ---- conversation ----

function Conversation({ events, onAtEnd, threadId, onEarlier }: { events: FyrEvent[] | undefined; onAtEnd(b: boolean): void; threadId: string; onEarlier?: () => void }) {
  const items = useMemo(() => buildConversation(events ?? []), [events]);
  const ref = useRef<HTMLDivElement>(null);
  const stick = useRef(true);

  const check = () => {
    const el = ref.current;
    if (!el) return;
    const atEnd = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    stick.current = atEnd;
    onAtEnd(atEnd);
  };

  useLayoutEffect(() => {
    stick.current = true;
  }, [threadId]);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (stick.current) el.scrollTop = el.scrollHeight;
    check();
  }, [items, threadId]);

  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => {
      if (stick.current) el.scrollTop = el.scrollHeight;
      check();
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  return (
    <div class="convo" ref={ref} onScroll={check}>
      <div class="convo-in">
        {events === undefined && <div class="sys">Loading…</div>}
        {events !== undefined && items.length === 0 && <div class="sys">No messages yet.</div>}
        {onEarlier && events && events.length > 0 && (
          <button class="btn ghost earlier" onClick={onEarlier}>
            Show earlier messages
          </button>
        )}
        {items.map((it) => (
          <ItemView key={it.key} it={it} />
        ))}
      </div>
    </div>
  );
}

function ItemView({ it }: { it: Item }) {
  switch (it.kind) {
    case "user":
      return (
        <div class="you prose">
          {it.text}
          {it.images > 0 && (
            <span class="imgchip">
              {it.images} {it.images === 1 ? "image" : "images"}
            </span>
          )}
        </div>
      );
    case "assistant":
      return <div class="cl prose md" dangerouslySetInnerHTML={{ __html: renderMarkdown(it.text) }} />;
    case "tools":
      return <ToolCard calls={it.calls} />;
    case "run":
      if (it.failure)
        return (
          <div class="sys fail prose">
            <StateIcon state="failed" size={14} />
            <span>
              Failed ({it.failure.class}): {it.failure.message}
            </span>
          </div>
        );
      return (
        <div class="sys">
          <span>{it.interrupted ? "Interrupted" : "Run ended"}</span>
          {it.durationMs !== undefined && <span class="tnum">{duration(it.durationMs)}</span>}
          {it.costUsd !== undefined && <span class="tnum">${it.costUsd.toFixed(2)}</span>}
        </div>
      );
    case "effect":
      return (
        <div class={`sys prose ${it.ok ? "" : "fail"}`}>
          <StateIcon state={it.ok ? "settled" : "failed"} size={14} />
          <span>
            <code>{it.action}</code> {it.text}
          </span>
        </div>
      );
    case "answer":
      return (
        <div class="sys prose">
          <span>Answered: {it.text}</span>
        </div>
      );
  }
}

function callIcon(c: ToolCall) {
  return c.isError ? "failed" : c.done ? "settled" : "working";
}

/** One bordered card for consecutive tool calls; the name column fits the longest name. */
function ToolCard({ calls, nested }: { calls: ToolCall[]; nested?: boolean }) {
  const rows = toolRows(calls);
  const longest = Math.max(4, ...rows.map((r) => (r.kind === "agents" ? r.calls[0].name.length : r.call.name.length)));
  const nmw = Math.min(140, Math.ceil(longest * 7.4));
  return (
    <div class={nested ? "act nested" : "act"} style={{ "--nmw": `${nmw}px` }}>
      {rows.map((r) => (r.kind === "agents" ? <AgentsRow calls={r.calls} /> : <ToolLine c={r.call} />))}
    </div>
  );
}

function ToolLine({ c }: { c: ToolCall }) {
  return (
    <div class="al" title={c.summary}>
      <StateIcon state={callIcon(c)} size={14} />
      <span class="nm ell">{c.name}</span>
      <span class={`ell${c.isError ? " err" : ""}`}>{c.summary}</span>
      <span class="d">{c.done ? duration(c.durationMs) : ""}</span>
    </div>
  );
}

function AgentsRow({ calls }: { calls: ToolCall[] }) {
  const [open, setOpen] = useState(false);
  const failed = calls.some((c) => c.isError);
  const done = calls.every((c) => c.done);
  const ms = done ? Math.max(...calls.map((c) => c.durationMs ?? 0)) : null;
  const desc = calls[0].summary;
  const n = calls.length;
  return (
    <>
      <div class="al click" onClick={() => setOpen(!open)} title={calls.map((c) => c.summary).join("\n")}>
        <StateIcon state={failed ? "failed" : done ? "settled" : "working"} size={14} />
        <span class="nm ell">{calls[0].name}</span>
        <span class="s ell">
          {n} {n === 1 ? "subagent" : "subagents"} · {desc} {open ? "▾" : "▸"}
        </span>
        <span class="d">{ms !== null ? duration(ms) : ""}</span>
      </div>
      {open && (
        <div class="sub">
          {calls.map((c) => (
            <div class="subw">
              <div class="subh ell">
                {c.summary} · {c.subMessages} {c.subMessages === 1 ? "message" : "messages"}
              </div>
              {c.subCalls.length > 0 && <ToolCard calls={c.subCalls} nested />}
              {c.subTexts.length > 0 && <div class="subtext prose">{c.subTexts[c.subTexts.length - 1]}</div>}
            </div>
          ))}
        </div>
      )}
    </>
  );
}

// ---- decision card ----

interface CardProps {
  d: Decision;
  more: number;
  card: CardState | null;
  onCard(cs: CardState): void;
  onAllow(d: Decision, allow: boolean): void;
}

function asker(d: Decision): string {
  return d.agent ? `asked by subagent “${d.agent}”` : "asked by this thread";
}

export function effectSentence(d: Decision): string {
  const e = d.effect;
  if (!e) return "";
  if (e.action === "pr.merge") {
    const pr = String(e.args?.pr ?? "");
    const num = /\/pull\/(\d+)/.exec(pr)?.[1];
    const method = e.args?.method ? ` with ${String(e.args.method)}` : "";
    return `Merge ${num ? "#" + num : pr}${method}`;
  }
  return e.action;
}

function DecisionCard(p: CardProps) {
  const { d } = p;
  if (d.kind === "ask") return p.card && p.card.decisionId === d.id ? <AskCard {...p} card={p.card} /> : null;
  const approval = d.kind === "approval";
  const cmd = approval && typeof d.input?.command === "string" ? (d.input.command as string) : null;
  const rest = approval && d.input ? Object.fromEntries(Object.entries(d.input).filter(([k]) => k !== "command")) : {};
  return (
    <div class="ask">
      <div class="ah">
        <StateIcon state="asking" size={14} />
        <b>{approval ? "Approval" : "Fyr action"}</b>
        <span class="ell">{asker(d)}</span>
        {p.more > 0 && <span class="ml">+{p.more} more</span>}
      </div>
      <div class="ab">
        <div class="q">{approval ? <>Allow {d.tool}?</> : effectSentence(d)}</div>
        <div class="payload">
          {cmd && <pre class="cmd">{cmd}</pre>}
          {approval && Object.keys(rest).length > 0 && <pre>{JSON.stringify(rest, null, 2)}</pre>}
          {!approval && <pre>{JSON.stringify(d.effect?.args ?? {}, null, 2)}</pre>}
        </div>
        <div class="grid4">
          <button class="op" onClick={() => p.onAllow(d, true)}>
            <span class="a ell">{approval ? "Allow" : "Approve"}</span>
            <kbd>1</kbd>
            <span class="d">{approval ? "Run it once." : `${d.effect?.reason ? d.effect.reason + " " : ""}Fyr runs it; the agent does not wait.`}</span>
          </button>
          <button class="op" onClick={() => p.onAllow(d, false)}>
            <span class="a ell">{approval ? "Deny" : "Decline"}</span>
            <kbd>2</kbd>
            <span class="d">{approval ? "The agent is told no and continues." : "Nothing is run."}</span>
          </button>
        </div>
      </div>
    </div>
  );
}

function AskCard(p: CardProps & { card: CardState }) {
  const { d, card } = p;
  const qs = questionsOf(d);
  const q = qs[card.index];
  if (!q) return null;
  const picks = card.picks[card.index] ?? [];
  const other = card.other[card.index];
  return (
    <div class="ask">
      <div class="ah">
        <StateIcon state="asking" size={14} />
        <b class="qn" onClick={() => qs.length > 1 && p.onCard(goTo(card, d, (card.index + 1) % qs.length))} title={qs.length > 1 ? "Next question (Tab)" : undefined}>
          Question {card.index + 1} of {qs.length}
        </b>
        <span class="ell">{asker(d)}</span>
        <span class="ml">
          {p.more > 0 && `+${p.more} more · `}
          <kbd>Tab</kbd> multi-select next
        </span>
      </div>
      <div class="ab">
        <div class="q">{q.question}</div>
        <div class="grid4">
          {q.options.map((o, i) => (
            <button class={`op${picks.includes(o.label) ? " on" : ""}`} title={`${o.label}${o.description ? `\n${o.description}` : ""}`} onClick={() => p.onCard(pick(card, d, i))}>
              <span class="a ell">{o.label}</span>
              <kbd>{i + 1}</kbd>
              <span class="d">{o.description ?? ""}</span>
            </button>
          ))}
          <button class={`op${other ? " on" : ""}`} onClick={() => document.getElementById("composer")?.focus()} title="Answer in your own words in the field below (0)">
            <span class="a ell">Other…</span>
            <kbd>0</kbd>
            <span class="d">{other ?? ""}</span>
          </button>
        </div>
      </div>
    </div>
  );
}

// ---- composer ----

export function readImages(files: Iterable<File>): Promise<ImageArg[]> {
  const list = [...files].filter((f) => f.type.startsWith("image/"));
  return Promise.all(
    list.map(
      (f) =>
        new Promise<ImageArg>((resolve, reject) => {
          const r = new FileReader();
          r.onload = () => {
            const s = String(r.result);
            resolve({ mediaType: f.type, data: s.slice(s.indexOf(",") + 1) });
          };
          r.onerror = () => reject(r.error);
          r.readAsDataURL(f);
        }),
    ),
  );
}

export function ImageStrip({ images, onRemove }: { images: ImageArg[]; onRemove(i: number): void }) {
  if (!images.length) return null;
  return (
    <div class="thumbs">
      {images.map((im, i) => (
        <span class="thumb">
          <img src={`data:${im.mediaType};base64,${im.data}`} alt={`Image ${i + 1}`} />
          <button title="Remove image" aria-label="Remove image" onClick={() => onRemove(i)}>
            ×
          </button>
        </span>
      ))}
    </div>
  );
}

export function useImagePaste() {
  const [images, setImages] = useState<ImageArg[]>([]);
  const add = async (files: Iterable<File>) => {
    const got = await readImages(files);
    if (got.length) setImages((cur) => [...cur, ...got]);
  };
  const onPaste = (e: ClipboardEvent) => {
    const files = [...(e.clipboardData?.files ?? [])];
    if (files.some((f) => f.type.startsWith("image/"))) {
      e.preventDefault();
      void add(files);
    }
  };
  const onDrop = (e: DragEvent) => {
    const files = [...(e.dataTransfer?.files ?? [])];
    if (files.length) {
      e.preventDefault();
      void add(files);
    }
  };
  const onDragOver = (e: DragEvent) => {
    if (e.dataTransfer?.types.includes("Files")) e.preventDefault();
  };
  return { images, setImages, onPaste, onDrop, onDragOver, remove: (i: number) => setImages((c) => c.filter((_, j) => j !== i)) };
}

interface ComposerProps {
  t: ThreadDTO;
  answer: AnswerField | null;
  onSend(text: string, images: ImageArg[]): Promise<boolean>;
  onInterrupt(): void;
}

/**
 * Enter sends (steers a running thread). While a question is open the composer is the answer field
 * for the active question's "Other" (U15): Enter answers, ⌘Enter queues the text as a message instead.
 */
function Composer({ t, answer, onSend, onInterrupt }: ComposerProps) {
  const [draft, setDraft] = useState("");
  const img = useImagePaste();
  const ta = useRef<HTMLTextAreaElement>(null);
  const sendReason = unavailableReason(t, "thread.send");
  const text = answer ? answer.text : draft;
  const setText = answer ? answer.setText : setDraft;
  const disabled = !answer && !!sendReason;

  useLayoutEffect(() => {
    const el = ta.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 200) + "px";
  }, [text]);

  const send = async () => {
    if (sendReason || (!text.trim() && !img.images.length)) return;
    const ok = await onSend(text, img.images);
    if (ok) {
      setText("");
      img.setImages([]);
    }
  };

  const placeholder = answer
    ? `Answer question ${answer.index + 1} in your own words…`
    : sendReason
      ? sendReason
      : t.running
        ? "Steer the running thread…"
        : "Message…";

  return (
    <div class="cmpw" onDrop={img.onDrop} onDragOver={img.onDragOver}>
      <ImageStrip images={img.images} onRemove={img.remove} />
      <div class={`cmp${disabled ? " off" : ""}`}>
        <textarea
          ref={ta}
          id="composer"
          class="in"
          rows={1}
          value={text}
          disabled={disabled}
          placeholder={placeholder}
          title={disabled ? sendReason : undefined}
          data-other={answer ? "1" : undefined}
          onInput={(e) => setText((e.target as HTMLTextAreaElement).value)}
          onPaste={img.onPaste}
          onKeyDown={(e) => {
            if (e.key !== "Enter" || e.shiftKey || e.isComposing) return;
            e.preventDefault();
            e.stopPropagation();
            if (answer && !(e.metaKey || e.ctrlKey)) answer.submit();
            else void send();
          }}
        />
        {answer && !sendReason && <span class="hint">⌘⏎ queue</span>}
        {t.running && (
          <button class="btn" disabled={!isAvailable(t, "thread.interrupt")} title={unavailableReason(t, "thread.interrupt") ?? "Interrupt (Esc)"} onClick={onInterrupt}>
            Interrupt <kbd>Esc</kbd>
          </button>
        )}
        {answer ? (
          <button class="btn p" title="Answer (Enter)" onClick={answer.submit}>
            Answer <kbd>⏎</kbd>
          </button>
        ) : (
          <button class="btn p" disabled={!!sendReason || (!text.trim() && !img.images.length)} title={sendReason ?? "Send (Enter)"} onClick={() => void send()}>
            Send <kbd>⏎</kbd>
          </button>
        )}
      </div>
    </div>
  );
}
