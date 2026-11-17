// SPEC V0.149: the Run card, one component for the desktop and the phone. A run request the agent declared offers
// Run (or Run as root), Copy and Not now; a command found in an agent's prose offers Copy only. The command wraps,
// never scrolls sideways; a Nix store path is shortened in the middle until the owner presses the command.
// The phone differs only in its stylesheet: 44 px targets (touch size).

import { useState } from "preact/hooks";
import type { Offer, Thread } from "./types.ts";
import { pressArgs } from "./offers.ts";
import { copyToClipboard } from "./copy.ts";
import { useHosts } from "./host-pick-ui.tsx";
import { cardLook, copyText, dismissArgs, foldOutput, requests, shortStore, stopArgs } from "./runcard.ts";

type Run = (action: string, args?: Record<string, unknown>) => unknown;

/** The command: short until pressed; a press (a tap, a click, Enter) shows and keeps the full text. */
function Command({ text }: { text: string }) {
  const short = shortStore(text);
  const [full, setFull] = useState(false);
  const cut = short !== text;
  return (
    <pre
      class={`runc-cmd${cut ? " cut" : ""}`}
      tabIndex={cut ? 0 : undefined}
      role={cut ? "button" : undefined}
      aria-expanded={cut ? full : undefined}
      title={cut && !full ? "Show the full command" : undefined}
      onClick={() => cut && setFull(!full)}
      onKeyDown={(e) => {
        if (cut && (e.key === "Enter" || e.key === " ")) {
          e.preventDefault();
          setFull(!full);
        }
      }}
    >
      {full ? text : short}
    </pre>
  );
}

function CopyCmd({ text }: { text: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      class="btn sm"
      onClick={async () => {
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

/** One run request: what runs, where, why, its run's output and the owner's buttons. */
export function RunCard({ o, t, host, onRun }: { o: Offer; t: Pick<Thread, "runs" | "rootPrompt">; host?: string; onRun: Run }) {
  const l = cardLook(o, t.runs, { host, rootPrompt: t.rootPrompt });
  const [all, setAll] = useState(false);
  const out = all ? { text: l.output.replace(/\n$/, ""), hidden: 0 } : foldOutput(l.output);
  return (
    <article class={`runc ${l.state}${o.root ? " root" : ""}`} aria-label={`${l.title}: ${l.why}`}>
      <header class="runc-h">
        <span class="runc-t">{l.title}</span>
        <span class="runc-why">{l.why}</span>
      </header>
      <Command text={o.command ?? ""} />
      <div class="runc-dir">in {l.folder}</div>
      {l.rootLine && <div class={`runc-root${t.rootPrompt ? " no" : ""}`}>{l.rootLine}</div>}
      {(l.output || l.state === "running") && (
        <div class="runc-o">
          {out.hidden > 0 && (
            <button type="button" class="runc-more" onClick={() => setAll(true)}>
              {out.hidden} earlier {out.hidden === 1 ? "line" : "lines"}
            </button>
          )}
          <pre class="runc-out" aria-live="polite">
            {out.text || "…"}
          </pre>
        </div>
      )}
      {l.result && <div class={`runc-res ${l.state}`}>{l.result}</div>}
      <div class="runc-b">
        {l.state === "running" ? (
          <button type="button" class="btn sm bad" onClick={() => onRun("action.stop", stopArgs(o))}>
            Stop
          </button>
        ) : (
          <button type="button" class={`btn sm${l.state === "fresh" ? " p" : ""}`} disabled={!!l.runOff} title={l.runOff ?? undefined} onClick={() => onRun("action.run", pressArgs("thread", o))}>
            {l.run}
          </button>
        )}
        <CopyCmd text={copyText(o)} />
        {l.state !== "running" && (
          <button type="button" class="btn sm" onClick={() => onRun("action.dismiss", dismissArgs(o))}>
            {l.close}
          </button>
        )}
      </div>
    </article>
  );
}

/** The thread's run requests under the conversation, oldest first (V0.149 (2)). */
export function RunRequests({ t, onRun }: { t: Thread; onRun: Run }) {
  const hs = useHosts();
  const list = requests(t);
  if (!list.length) return null;
  const host = t.host || hs.self || undefined;
  return (
    <section class="runcs" aria-label="Commands to run">
      {list.map((o) => (
        <RunCard key={o.id} o={o} t={t} host={host} onRun={onRun} />
      ))}
    </section>
  );
}

/** A command in an agent's message (V0.149 (8)): Copy only, since the agent did not declare it. */
export function ProseCommand({ text }: { text: string }) {
  return (
    <article class="runc prose" aria-label="A command in the agent's message">
      <header class="runc-h">
        <span class="runc-t">Command</span>
        <span class="runc-why">Copy only: the agent wrote it in its message instead of sending a Run card.</span>
      </header>
      <Command text={text} />
      <div class="runc-b">
        <CopyCmd text={text} />
      </div>
    </article>
  );
}
