// Thread pane: header, conversation, docked decision card and composer.

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import type { Decision, FyrEvent, ImageArg, Profile, PullRequest, ThreadDTO } from "./types.ts";
import { buildConversation, type Item, type ToolCall } from "./conversation.ts";
import { renderMarkdown } from "./markdown.ts";
import { StateIcon, stateClass, stateLabel } from "./icons.tsx";
import { duration, shortModel } from "./format.ts";
import { answerOf, goTo, oneColumn, openOther, pick, questionsOf, setOther, type CardState } from "./question.ts";
import { isAvailable, unavailableReason } from "./store.ts";

export function prLabel(pr: PullRequest): { text: string; cls: string } {
  if (pr.state === "MERGED") return { text: `#${pr.number} merged`, cls: "merged" };
  if (pr.state === "CLOSED") return { text: `#${pr.number} closed`, cls: "merged" };
  if (pr.conflict) return { text: `#${pr.number} conflict`, cls: "fail" };
  if (pr.checks === "fail") return { text: `#${pr.number} ✗ CI`, cls: "fail" };
  if (pr.checks === "pass") return { text: `#${pr.number} ✓`, cls: "ok" };
  if (pr.checks === "pending") return { text: `#${pr.number} …`, cls: "" };
  return { text: `#${pr.number}`, cls: "" };
}

interface ActBtnProps {
  t: ThreadDTO;
  action: string;
  label: string;
  k: string;
  onRun(action: string): void;
  primary?: boolean;
}

function ActBtn({ t, action, label, k, onRun, primary }: ActBtnProps) {
  const reason = unavailableReason(t, action);
  return (
    <button class={`btn${primary ? " p" : ""}`} disabled={!!reason} title={reason ?? `${label} (${k})`} onClick={() => onRun(action)}>
      {label} <kbd>{k}</kbd>
    </button>
  );
}

export interface ThreadPaneProps {
  thread: ThreadDTO;
  mode: "open" | "peek";
  events: FyrEvent[] | undefined;
  profiles: Profile[];
  card: CardState | null;
  focusOther: number;
  onCard(cs: CardState): void;
  onAnswerAsk(d: Decision): void;
  onAllow(d: Decision, allow: boolean): void;
  onRun(action: string, args?: Record<string, unknown>): void;
  onSnooze(): void;
  onSend(text: string, images: ImageArg[]): Promise<boolean>;
  onAtEnd(atEnd: boolean): void;
  onFocusZone(): void;
}

export function ThreadPane(p: ThreadPaneProps) {
  const t = p.thread;
  const profile = p.profiles.find((x) => x.id === t.profile);
  const decision = t.decisions?.[0] ?? null;
  const st = t.attention.state;
  return (
    <section class="th" onMouseDown={p.onFocusZone}>
      <header class="thh">
        <div class="tr">
          <h1 class="tt" title={t.title}>
            {t.title || "Untitled"}
          </h1>
          <div class="acts">
            {st === "failed" && <ActBtn t={t} action="thread.retry" label="Retry" k="r" onRun={(a) => p.onRun(a)} />}
            <button class="btn" disabled={!!unavailableReason(t, "thread.snooze")} title={unavailableReason(t, "thread.snooze") ?? "Snooze (s)"} onClick={p.onSnooze}>
              Snooze <kbd>s</kbd>
            </button>
            <ActBtn t={t} action="thread.settle" label={t.running ? "Settle when done" : "Settle"} k="e" onRun={(a) => p.onRun(a)} />
            <ActBtn t={t} action="thread.archive" label="Archive" k="#" onRun={(a) => p.onRun(a)} />
          </div>
        </div>
        <div class="chips">
          <span class={`chip st ${stateClass(st)}`} title={t.attention.reason || stateLabel(st)}>
            <span class={t.running ? "spin" : ""}>
              <StateIcon state={st} size={14} />
            </span>
            <span class="lab">{stateLabel(st)}</span>
          </span>
          {p.mode === "peek" && <span class="chip peek">Peek · ⏎ opens</span>}
          <span class="chip pj" title={t.cwd}>
            <i class="dot" style={{ background: profile?.color ?? "var(--ink-faint)" }} title={profile?.name ?? t.profile} />
            <span>{t.project || t.cwd}</span>
          </span>
          <ModelChip t={t} profile={profile} onRun={p.onRun} />
          {t.pr && <PrChip pr={t.pr} />}
          {t.settleWhenDone && <span class="chip">settles when done</span>}
          {t.settleWhenMerged && <span class="chip">settles when merged</span>}
          {t.source === "t3" && <span class="chip m">T3</span>}
        </div>
      </header>
      <Conversation events={p.events} onAtEnd={p.onAtEnd} threadId={t.id} />
      {decision && (
        <DecisionCard
          d={decision}
          more={(t.decisions?.length ?? 1) - 1}
          card={p.card}
          focusOther={p.focusOther}
          onCard={p.onCard}
          onAnswerAsk={p.onAnswerAsk}
          onAllow={p.onAllow}
        />
      )}
      <Composer t={t} onSend={p.onSend} onInterrupt={() => p.onRun("thread.interrupt")} hasDecision={!!decision} />
    </section>
  );
}

function ModelChip({ t, profile, onRun }: { t: ThreadDTO; profile: Profile | undefined; onRun(a: string, args?: Record<string, unknown>): void }) {
  const models = profile?.models ?? [];
  const reason = unavailableReason(t, "thread.setModel");
  if (models.length < 2 || reason) {
    return (
      <span class="chip m" title={reason ? `${t.model} · ${reason}` : t.model}>
        <span>{shortModel(t.model)}</span>
      </span>
    );
  }
  return (
    <label class="chip m sel" title="Change model">
      <select value={t.model} onChange={(e) => onRun("thread.setModel", { model: (e.target as HTMLSelectElement).value })}>
        {!models.includes(t.model) && <option value={t.model}>{shortModel(t.model)}</option>}
        {models.map((m) => (
          <option value={m}>{shortModel(m)}</option>
        ))}
      </select>
    </label>
  );
}

function PrChip({ pr }: { pr: PullRequest }) {
  const l = prLabel(pr);
  return (
    <a class={`chip prc ${l.cls}`} href={pr.url} target="_blank" rel="noopener noreferrer" title={pr.title || pr.url}>
      <span>{l.text}</span>
    </a>
  );
}

// ---- conversation ----

function Conversation({ events, onAtEnd, threadId }: { events: FyrEvent[] | undefined; onAtEnd(b: boolean): void; threadId: string }) {
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
      return (
        <div class="act">
          {it.calls.map((c) => (
            <ToolLine c={c} key={c.id} />
          ))}
        </div>
      );
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
        <div class={`sys prose ${it.ok ? "ok" : "fail"}`}>
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

function ToolLine({ c, nested }: { c: ToolCall; nested?: boolean }) {
  const [open, setOpen] = useState(false);
  const hasSub = c.subMessages > 0;
  const icon = c.isError ? "failed" : c.done ? "settled" : "working";
  return (
    <>
      <div class={`al${nested ? " nested" : ""}${hasSub ? " click" : ""}`} onClick={hasSub ? () => setOpen(!open) : undefined} title={c.summary}>
        <StateIcon state={icon} size={14} />
        <span class="nm ell">{c.name}</span>
        <span class={`ell${c.isError ? " err" : ""}`}>
          {hasSub ? (
            <span class="s">
              {c.subMessages} subagent {c.subMessages === 1 ? "message" : "messages"} · {c.summary} {open ? "▾" : "▸"}
            </span>
          ) : (
            c.summary
          )}
          {c.isError && <b class="errtag"> error</b>}
        </span>
        <span class="d">{c.done ? duration(c.durationMs) : ""}</span>
      </div>
      {open && (
        <div class="sub">
          {c.subCalls.map((s) => (
            <ToolLine c={s} nested key={s.id} />
          ))}
          {c.subTexts.length > 0 && <div class="subtext prose">{c.subTexts[c.subTexts.length - 1]}</div>}
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
  focusOther: number;
  onCard(cs: CardState): void;
  onAnswerAsk(d: Decision): void;
  onAllow(d: Decision, allow: boolean): void;
}

function Asker({ d }: { d: Decision }) {
  return d.agent ? <span class="ell">asked by subagent “{d.agent}”</span> : <span class="ell">asked by the thread</span>;
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
  if (d.kind === "ask" && p.card && p.card.decisionId === d.id) return <AskCard {...p} card={p.card} />;
  if (d.kind === "approval" || d.kind === "effect") {
    const approval = d.kind === "approval";
    const cmd = approval ? (typeof d.input?.command === "string" ? (d.input.command as string) : null) : null;
    const rest = approval && d.input ? Object.fromEntries(Object.entries(d.input).filter(([k]) => k !== "command")) : null;
    return (
      <div class="ask">
        <div class="ah">
          <StateIcon state="asking" size={14} />
          <b>{approval ? "Approval" : "Fyr action"}</b>
          <Asker d={d} />
          {p.more > 0 && <span class="ml">+{p.more} more</span>}
        </div>
        <div class="ab">
          {approval ? (
            <>
              <div class="q">
                Allow <code>{d.tool}</code>?
              </div>
              <div class="payload">
                {cmd && <pre class="cmd">{cmd}</pre>}
                {rest && Object.keys(rest).length > 0 && <pre>{JSON.stringify(rest, null, 2)}</pre>}
              </div>
            </>
          ) : (
            <>
              <div class="q prose">{effectSentence(d)}</div>
              <div class="payload">
                <pre>{JSON.stringify(d.effect?.args ?? {}, null, 2)}</pre>
              </div>
              {d.effect?.reason && <div class="reason prose">{d.effect.reason}</div>}
              <div class="note">Fyr runs this after you approve. The agent does not wait for it.</div>
            </>
          )}
          <div class="grid2">
            <button class="op op-b" onClick={() => p.onAllow(d, true)}>
              <span class="a">{approval ? "Allow" : "Approve"}</span>
              <kbd>1</kbd>
            </button>
            <button class="op op-b" onClick={() => p.onAllow(d, false)}>
              <span class="a">{approval ? "Deny" : "Decline"}</span>
              <kbd>2</kbd>
            </button>
          </div>
        </div>
      </div>
    );
  }
  return null;
}

function AskCard(p: CardProps & { card: CardState }) {
  const { d, card } = p;
  const qs = questionsOf(d);
  const q = qs[card.index];
  const otherRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (card.otherOpen) otherRef.current?.focus();
  }, [card.otherOpen, card.index, p.focusOther]);
  if (!q) return null;
  const one = oneColumn(q);
  const picks = card.picks[card.index] ?? [];
  const otherChosen = card.other[card.index] !== null;
  const ready = qs.every((_, i) => answerOf(card, d, i) !== null);
  const set = (cs: CardState) => p.onCard(cs);
  return (
    <div class="ask">
      <div class="ah">
        <StateIcon state="asking" size={14} />
        <b>
          Question {card.index + 1} of {qs.length}
        </b>
        {q.header && <span class="qh ell">{q.header}</span>}
        <Asker d={d} />
        <span class="ml">
          {q.multiSelect ? "multi-select · " : ""}
          {qs.length > 1 && (
            <>
              <kbd>Tab</kbd> next
            </>
          )}
          {p.more > 0 && ` · +${p.more} more`}
        </span>
      </div>
      <div class="ab">
        <div class="q prose">{q.question}</div>
        <div class={one ? "grid1" : "grid4"}>
          {q.options.map((o, i) => {
            const on = picks.includes(o.label);
            return (
              <button
                class={`op${on ? " on" : ""}`}
                title={o.label}
                onClick={() => set(pick(card, d, i))}
              >
                <span class="a ell">{o.label}</span>
                <kbd>{i + 1}</kbd>
                <span class="d">{o.description ?? ""}</span>
              </button>
            );
          })}
          <button class={`op${otherChosen ? " on" : ""}`} onClick={() => set(openOther(card))}>
            <span class="a ell">Other…</span>
            <kbd>0</kbd>
            <span class="d">{otherChosen && card.other[card.index] ? card.other[card.index] : "Answer in your own words."}</span>
          </button>
        </div>
        {card.otherOpen && (
          <input
            ref={otherRef}
            class="other"
            data-other="1"
            placeholder={`Answer question ${card.index + 1} in your own words…`}
            value={card.other[card.index] ?? ""}
            onInput={(e) => set(setOther(card, d, (e.target as HTMLInputElement).value))}
          />
        )}
        <div class="afoot">
          <span class="dots">
            {qs.map((_, i) => (
              <button
                class={`qd${i === card.index ? " cur" : ""}${answerOf(card, d, i) !== null ? " done" : ""}`}
                title={`Question ${i + 1}`}
                onClick={() => set(goTo(card, d, i))}
              />
            ))}
          </span>
          <button class="btn p" disabled={!ready} title={ready ? "Send all answers" : "Answer every question first"} onClick={() => p.onAnswerAsk(d)}>
            Answer <kbd>⏎</kbd>
          </button>
        </div>
      </div>
    </div>
  );
}

// ---- composer ----

interface ComposerProps {
  t: ThreadDTO;
  hasDecision: boolean;
  onSend(text: string, images: ImageArg[]): Promise<boolean>;
  onInterrupt(): void;
}

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
    return got.length > 0;
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

function Composer({ t, onSend, onInterrupt, hasDecision }: ComposerProps) {
  const [text, setText] = useState("");
  const img = useImagePaste();
  const ta = useRef<HTMLTextAreaElement>(null);
  const sendReason = unavailableReason(t, "thread.send");
  const canInterrupt = t.running && isAvailable(t, "thread.interrupt");

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

  const placeholder = sendReason
    ? sendReason
    : hasDecision
      ? "Message the thread (the card above takes the answer)…"
      : t.running
        ? "Steer the running thread…"
        : "Message…";

  return (
    <div class="cmpw" onDrop={img.onDrop} onDragOver={img.onDragOver}>
      <ImageStrip images={img.images} onRemove={img.remove} />
      <div class={`cmp${sendReason ? " off" : ""}`}>
        <textarea
          ref={ta}
          id="composer"
          rows={1}
          value={text}
          disabled={!!sendReason}
          placeholder={placeholder}
          title={sendReason}
          onInput={(e) => setText((e.target as HTMLTextAreaElement).value)}
          onPaste={img.onPaste}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
              e.preventDefault();
              void send();
            }
          }}
        />
        {t.running && (
          <button class="btn" disabled={!canInterrupt} title={unavailableReason(t, "thread.interrupt") ?? "Interrupt (Esc)"} onClick={onInterrupt}>
            Interrupt <kbd>Esc</kbd>
          </button>
        )}
        <button class="btn p" disabled={!!sendReason || (!text.trim() && !img.images.length)} title={sendReason ?? "Send (Enter)"} onClick={() => void send()}>
          Send <kbd>⏎</kbd>
        </button>
      </div>
    </div>
  );
}
