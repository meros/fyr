// SPEC V0.149: the Run card, one component for the desktop and the phone. A run request the agent declared offers
// Run (or Run as root), Copy and Not now; a command found in an agent's prose offers Copy only. The command wraps,
// never scrolls sideways; a Nix store path is shortened in the middle until the owner presses the command.
// The phone differs only in its stylesheet: 44 px targets (touch size).

import { createContext } from "preact";
import { useContext, useState } from "preact/hooks";
import type { Offer, Thread } from "./types.ts";
import { pressArgs } from "./offers.ts";
import { copyToClipboard } from "./copy.ts";
import { cardDomId, cardLook, copyText, dismissArgs, dockRun, firstLine, foldOutput, requests, shortStore, stopArgs } from "./runcard.ts";

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

/** One run request: what runs, where, why, its run's output and the owner's buttons. `docked`: it waits in the dock
 *  (V0.next-step-dock (5)), so the buttons are there and this card points to them: one place to press. */
export function RunCard({ o, t, host, onRun, docked }: { o: Offer; t: Pick<Thread, "runs" | "rootPrompt">; host?: string; onRun: Run; docked?: boolean }) {
  const l = cardLook(o, t.runs, { host, rootPrompt: t.rootPrompt });
  const [all, setAll] = useState(false);
  const out = all ? { text: l.output.replace(/\n$/, ""), hidden: 0 } : foldOutput(l.output);
  return (
    <article id={cardDomId(o.id)} class={`runc ${l.state}${o.root ? " root" : ""}`} aria-label={`${l.title}: ${l.why}`}>
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
      {docked ? (
        <div class="runc-dock">Waiting in the dock below</div>
      ) : (
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
      )}
    </article>
  );
}

/** V0.next-step-dock (3), (4): a run request in the dock above the composer, short: why, the command's first line, the
 *  host, and Run, Copy, Not now and Show in conversation. Run scrolls to the full card, which streams the output.
 *  Desktop and phone share it; the phone's buttons are 44 px (phone.css, touch size). */
export function DockRun({ o, t, host, more, onRun, onShow }: { o: Offer; t: Pick<Thread, "runs" | "rootPrompt">; host?: string; more: number; onRun: Run; onShow(id: string): void }) {
  const l = cardLook(o, t.runs, { host, rootPrompt: t.rootPrompt });
  return (
    <section class={`ask dockrun${o.root ? " root" : ""}`} aria-label={`${l.title}: ${l.why}`}>
      <div class="ah">
        <b>{l.title}</b>
        <span class="ell">{l.folder}</span>
        {more > 0 && <span class="ml">+{more} more</span>}
      </div>
      <div class="ab">
        <div class="q">{l.why}</div>
        <pre class="runc-cmd">{shortStore(firstLine(o.command ?? ""))}</pre>
        {l.rootLine && <div class={`runc-root${t.rootPrompt ? " no" : ""}`}>{l.rootLine}</div>}
        <div class="runc-b">
          <button
            type="button"
            class="btn sm p"
            disabled={!!l.runOff}
            title={l.runOff ?? undefined}
            onClick={() => {
              onRun("action.run", pressArgs("thread", o));
              onShow(o.id);
            }}
          >
            {l.run}
          </button>
          <CopyCmd text={copyText(o)} />
          <button type="button" class="btn sm" onClick={() => onRun("action.dismiss", dismissArgs(o))}>
            {l.close}
          </button>
          <button type="button" class="btn sm ghost dockrun-show" onClick={() => onShow(o.id)}>
            Show in conversation
          </button>
        </div>
      </div>
    </section>
  );
}

/** V0.next-step-dock (3): scroll the conversation to a request's full card; on another tab, open the conversation first. */
export function showCard(id: string, tab: string, setTab: (tab: string) => void) {
  const go = () => document.getElementById(cardDomId(id))?.scrollIntoView({ behavior: "smooth", block: "center" });
  if (tab === "conv") go();
  else {
    setTab("conv");
    setTimeout(go, 50);
  }
}

/** What a card in the conversation needs: the thread and its commands. */
export const RunCards = createContext<{ t: Thread; onRun: Run } | null>(null);

/** V0.run-cards-in-conversation: one request's card, at its place in the conversation; nothing once it is closed. */
export function RunCardAt({ id }: { id: string }) {
  const c = useContext(RunCards);
  const o = c && requests(c.t).find((x) => x.id === id);
  if (!c || !o) return null;
  return <RunCard o={o} t={c.t} host={c.t.host || undefined} onRun={c.onRun} docked={dockRun(c.t)?.o.id === id} />;
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
