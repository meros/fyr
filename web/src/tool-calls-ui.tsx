// Tool calls in the conversation (V0.21): cards, fold groups, call rows, subagent timelines, quiet lines and Copy.
import { copySource, copyToClipboard } from "./copy.ts";
import { useApi } from "./api-context.ts";
import { useContext, useEffect, useState } from "preact/hooks";
import { onToolDetails, toolDetails } from "./prefs.ts";
import type { HostId } from "./hostid.ts";
import { createContext } from "preact";
import { agentPrompt, capCalls, cutOutput, friendlyName, fullInput, groupStats, groupSummary, groupTone, segments, type Item, type ToolCall, type SubStep, runLabel, fullOutputOf } from "./conversation.ts";
import { renderMarkdown } from "./markdown.ts";
import { StateIcon } from "./icons.tsx";
import { duration } from "./format.ts";
import { UndIcon } from "./undeclared-ctx.tsx";

/** "Show tool details" (V0.21): every group open and every machine line in full. */
export function useToolDetails(): boolean {
  const [on, setOn] = useState(toolDetails());
  useEffect(() => onToolDetails(setOn), []);
  return on;
}

/** V0.54: the thread's host: its attachments and previews load from there. */
export const HostContext = createContext<HostId | undefined>(undefined);

/** #91: the icon of a group's tone; a recovered group keeps the failed shape and turns amber (.tl.rec in app.css). */
export const TONE_ICON = { failed: "failed", recovered: "failed", running: "working", ok: "settled" } as const;

/** Enter and Space activate a focused group or row (they are divs with role=button). */
export function keyActivate(fn: () => void) {
  return (e: KeyboardEvent) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      e.stopPropagation();
      fn();
    }
  };
}

/** A quiet machine line: one line, the exact text one tap away. */
export function QuietLine({ icon, line, full, fail, und }: { icon: "settled" | "failed" | "seen" | "working"; line: string; full?: string; fail?: boolean; und?: boolean }) {
  const [open, setOpen] = useState(false);
  const more = !!full && full.trim() !== line.trim();
  return (
    <div class={`ql${fail ? " fail" : ""}${und ? " und" : ""}`}>
      <div class={`qlh${more ? " click" : ""}`} role={more ? "button" : undefined} tabIndex={more ? 0 : undefined} aria-expanded={more ? open : undefined} onClick={more ? () => setOpen(!open) : undefined} onKeyDown={more ? keyActivate(() => setOpen(!open)) : undefined}>
        {und ? <UndIcon /> : <StateIcon state={icon} size={12} />}
        <span class="ell">{line}</span>
      </div>
      {open && full && <CopyPre cls="qlx" text={full} />}
    </div>
  );
}

/** The end of a turn: a small mark; a tap shows how long a failed turn ran. A failure keeps its text in one line.
 *  V0.quota-wait: a run the provider's quota stopped waits, so it wears the waiting mark, not the failed one. */
export function RunMark({ it }: { it: Extract<Item, { kind: "run" }> }) {
  const [open, setOpen] = useState(false);
  const facts = it.durationMs !== undefined ? duration(it.durationMs) : "";
  const quota = it.failure?.class === "quota";
  const icon = quota ? "waiting" : it.failure ? "failed" : it.interrupted ? "seen" : "settled";
  const word = quota ? `Waiting for quota: ${it.failure?.message}` : it.failure ? `Failed (${it.failure.class}): ${it.failure.message}` : it.interrupted ? "Interrupted" : "Finished";
  if (!it.compact) {
    return (
      <div class={`sys prose${it.failure && !quota ? " fail" : ""}`}>
        <StateIcon state={icon} size={14} />
        <span>{word}</span>
        {facts && <span class="tnum">{facts}</span>}
      </div>
    );
  }
  // V0.21: words, never a bare symbol.
  const lbl = runLabel(it);
  return (
    <div class={`runmark${it.failure && !quota ? " fail" : ""}`}>
      <button class="rm" aria-expanded={open} title={[lbl.text, lbl.more].filter(Boolean).join(" · ")} onClick={() => lbl.more && setOpen(!open)}>
        {it.failure && <StateIcon state={icon} size={12} />}
        <span class="prose">{lbl.text}</span>
        {open && lbl.more && <span class="rmx tnum">{lbl.more}</span>}
      </button>
    </div>
  );
}

/** V0.34: a Copy button that says "Copied" for a moment. */
export function CopyButton({ text, label = "Copy", cls = "" }: { text: string; label?: string; cls?: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      class={`cpy ${cls}`}
      aria-label={label}
      title={label}
      onClick={async (e) => {
        e.stopPropagation();
        if (await copyToClipboard(text)) {
          setDone(true);
          setTimeout(() => setDone(false), 1200);
        }
      }}
    >
      {done ? "Copied" : "Copy"}
    </button>
  );
}

/** A preformatted block with Copy; `copy` is the full text when the shown one is cut. */
function CopyPre({ cls, text, copy }: { cls: string; text: string; copy?: string }) {
  return (
    <div class="codeb">
      <CopyButton text={copySource({ kind: "block", text: copy ?? text })} label="Copy" />
      <pre class={cls}>{text}</pre>
    </div>
  );
}


function callIcon(c: ToolCall) {
  return c.isError ? "failed" : c.done ? "settled" : "working";
}

/** One bordered card for consecutive tool calls: folded summary lines, with subagent rows as they were (V0.16). */
export function ToolCard({ calls, nested }: { calls: ToolCall[]; nested?: boolean }) {
  return (
    <div class={nested ? "act nested" : "act"}>
      {segments(calls).map((s) => (s.kind === "agents" ? <AgentsRow calls={s.calls} /> : <FoldGroup calls={s.calls} />))}
    </div>
  );
}

/** "Read 3 files · ran 2 commands", total time, an error mark; a tap opens the rows. */
function FoldGroup({ calls }: { calls: ToolCall[] }) {
  const details = useToolDetails();
  const [open, setOpen] = useState<boolean | null>(null);
  const [all, setAll] = useState(false);
  const isOpen = open ?? details;
  const st = groupStats(calls);
  const showAll = () => setAll(true);
  const toggle = () => setOpen(!isOpen);
  const text = calls.length === 1 ? calls[0].label : groupSummary(calls);
  return (
    <>
      <div class={`tl grp${groupTone(st) === "recovered" ? " rec" : ""}`} role="button" tabIndex={0} aria-expanded={isOpen} data-keys="self" title={text} onClick={toggle} onKeyDown={keyActivate(toggle)}>
        <StateIcon state={TONE_ICON[groupTone(st)]} size={14} />
        <span class="tlt ell">
          {text}
          {st.failed > 0 && <b class="tlerr"> · {st.failed} failed</b>}
        </span>
        <span class="d">
          {st.durationMs !== null ? duration(st.durationMs) : ""} {isOpen ? "▾" : "▸"}
        </span>
      </div>
      {isOpen &&
        capCalls(calls, all).map((r) =>
          r.kind === "more" ? (
            <div class="tl row more" key="more" role="button" tabIndex={0} data-keys="self" title="Show every call" onClick={showAll} onKeyDown={keyActivate(showAll)}>
              <span class="tlm">⋯</span>
              <span class="tlt ell">{r.label}</span>
              <span class="d" />
            </div>
          ) : (
            <CallRow c={r.call} key={r.call.id} />
          ),
        )}
    </>
  );
}

export const ThreadIdContext = createContext("");

/** One call: its label; a tap shows the exact input and output (output cut at 200 lines). */
function CallRow({ c }: { c: ToolCall }) {
  const api = useApi();
  const [open, setOpen] = useState(false);
  const [all, setAll] = useState(false);
  const threadId = useContext(ThreadIdContext);
  const hostId = useContext(HostContext);
  const [whole, setWhole] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const loadWhole = async () => {
    if (!c.cutSeq || !hostId) return;
    setLoading(true);
    const ev = await api.fullEvent(hostId, threadId, c.cutSeq).catch(() => null);
    const text = ev ? fullOutputOf(ev, c.id) : null;
    setLoading(false);
    if (text !== null) {
      setWhole(text);
      setAll(true);
    }
  };
  const toggle = () => setOpen(!open);
  const full = whole ?? c.output;
  const out = all ? { text: full, hidden: 0 } : cutOutput(full);
  return (
    <div class={`tlr${open ? " open" : ""}`}>
      <div class="tl row" role="button" tabIndex={0} aria-expanded={open} data-keys="self" onClick={toggle} onKeyDown={keyActivate(toggle)}>
        <StateIcon state={callIcon(c)} size={14} />
        <span class={`tlt ell${c.isError ? " err" : ""}`}>{c.label}</span>
        <span class="d">{c.done ? duration(c.durationMs) : "running"}</span>
      </div>
      {open && (
        <div class="tld">
          <div class="tldh">
            {friendlyName(c.name)}
            {c.isError ? " · failed" : ""}
          </div>
          <CopyPre cls="tlin" text={fullInput(c.name, c.input)} />
          {c.output ? (
            <>
              <CopyPre cls={`tlout${c.isError ? " err" : ""}`} text={out.text} copy={c.output} />
              {c.cutSeq && whole === null && (
                <button class="btn sm tlall" disabled={loading} onClick={loadWhole}>
                  {loading ? "Loading…" : "Load the full output"}
                </button>
              )}
              {out.hidden > 0 && (
                <button class="btn sm tlall" onClick={() => setAll(true)}>
                  Show all ({out.hidden.toLocaleString()} more lines)
                </button>
              )}
            </>
          ) : (
            <div class="tldh">{c.done ? "No output" : "Running…"}</div>
          )}
        </div>
      )}
    </div>
  );
}

/** V0.subagent-timeline: the prompt a subagent was given: three lines, a tap shows it whole (desktop and phone alike). */
function SubPrompt({ text }: { text: string }) {
  const [open, setOpen] = useState(false);
  if (!text) return null;
  return (
    <button class={`subprompt prose${open ? " open" : ""}`} aria-expanded={open} onClick={() => setOpen(!open)} title={open ? "Fold the prompt" : "Show the whole prompt"}>
      <span class="subpl">Prompt</span>
      {text}
    </button>
  );
}

/** V0.subagent-timeline: a subagent's prompt, then its texts and tool calls in order; in an Agent row and in the agents strip. */
export function SubTimeline({ c }: { c: ToolCall }) {
  return (
    <>
      <SubPrompt text={agentPrompt(c)} />
      {c.sub.map((s: SubStep, i) =>
        s.kind === "calls" ? <ToolCard key={i} calls={s.calls} nested /> : <div key={i} class="subtext prose md" dangerouslySetInnerHTML={{ __html: renderMarkdown(s.text) }} />,
      )}
    </>
  );
}

function AgentsRow({ calls }: { calls: ToolCall[] }) {
  const [open, setOpen] = useState(false);
  const failed = calls.some((c) => c.isError);
  const done = calls.every((c) => c.done);
  const ms = done ? Math.max(...calls.map((c) => c.durationMs ?? 0)) : null;
  const desc = calls[0].description || calls[0].summary;
  const n = calls.length;
  return (
    <>
      <button class="al click" aria-expanded={open} onClick={() => setOpen(!open)} title={calls.map((c) => c.description || c.summary).join("\n")}>
        <StateIcon state={failed ? "failed" : done ? "settled" : "working"} size={14} />
        <span class="nm ell">{calls[0].name}</span>
        <span class="s ell">
          {n} {n === 1 ? "subagent" : "subagents"} · {desc} {open ? "▾" : "▸"}
        </span>
        <span class="d">{ms !== null ? duration(ms) : ""}</span>
      </button>
      {open && (
        <div class="sub">
          {calls.map((c) => (
            <div class="subw">
              <div class="subh ell">
                {c.description || c.summary} · {c.subMessages} {c.subMessages === 1 ? "message" : "messages"}
              </div>
              <SubTimeline c={c} />
            </div>
          ))}
        </div>
      )}
    </>
  );
}

