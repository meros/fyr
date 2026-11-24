// The conversation: paced entries, folds, messages and their delivery line (V0.33, V0.41), on desktop and phone.
import { copySource, copyToClipboard } from "./copy.ts";
import { Boundary, CrashProbe, crashProbe } from "./boundary.tsx";
import { useApi } from "./api-context.ts";
import { useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import { createContext } from "preact";
import { reducedMotion } from "./motion-ui.tsx";
import type { FyrEvent, Thread } from "./types.ts";
import { conversationCache, pace, type Entry, deliveryLabel, type Item } from "./conversation.ts";
import { renderMarkdown } from "./markdown.ts";
import { StateIcon } from "./icons.tsx";
import { clockTime, duration } from "./format.ts";
import { dividerLine, rungDone } from "./handover.ts";
import { ref } from "./hostfeed.ts";
import { profilesOf } from "./hosts.ts";
import type { HostId } from "./hostid.ts";
import { ProfilesContext } from "./host-pick-ui.tsx";
import { FromThreadMessage, useDirectory } from "./project-ui.tsx";
import { SentAttachments } from "./attach-ui.tsx";
import type { ComponentChildren } from "preact";
import { PreviewCardView, openMarkdownImage } from "./preview-ui.tsx";
import type { PreviewCard } from "./previews.ts";
import { NextSteps, ProseCommand, RunCardAt, RunCards } from "./runcard-ui.tsx";
import { cardShows, cardsKey, placeCards, requests, splitProse, waitingRequests } from "./runcard.ts";
import { JUMP_EVENT, LAND, leftAt, looseKey, newSince, pickStop, stopsOf, type JumpAsk, type Placed, type StopKind } from "./jumps.ts";
import { type SendArgs } from "./composer.tsx";
import { TONE_ICON, HostContext, useToolDetails, RunMark, ToolCard, CopyButton, QuietLine, keyActivate, ThreadIdContext } from "./tool-calls-ui.tsx";
import { TASK_ICON } from "./agents-strip.tsx";


/** SPEC 0.5 V0.33: what a message's Cancel and Send again do. */
interface DeliveryActions {
  cancel(messageId: string): Promise<unknown>;
  resend(args: SendArgs): Promise<unknown>;
}
const DeliveryCtx = createContext<DeliveryActions | null>(null);


export function Conversation({ events, onAtEnd, threadId, onEarlier, cwd, delivery, outbox, host, strip, cards }: { events: FyrEvent[] | undefined; outbox?: string[]; onAtEnd(b: boolean): void; threadId: string; onEarlier?: () => void; cwd?: string; delivery?: DeliveryActions; host?: HostId; strip?: ComponentChildren; cards?: { t: Thread; onRun(action: string, args?: Record<string, unknown>): unknown } }) {
  const details = useToolDetails();
  const api = useApi();
  const outKey = outbox ? outbox.join(",") : null;
  // V0.event-store: one builder per thread; a live event at the end feeds only itself.
  const build = useMemo(conversationCache, [threadId]);
  const all = useMemo(() => build(events ?? [], { details, cwd, host, urls: api, outbox: outKey === null ? undefined : outKey ? outKey.split(",") : [] }), [api, build, events, details, cwd, host, outKey]);
  // V0.33: messages the agent has not read stand after "Agent is here".
  // V0.run-cards-in-conversation: a Run card stands where the agent asked, on desktop and phone.
  const openCards = cards ? requests(cards.t).filter((o) => cardShows(o, cards.t.runs)).map((o) => o.id).join("\n") : "";
  const [items, pending, looseCards] = useMemo(() => {
    const placed = placeCards(all.filter((i) => !(i.kind === "user" && i.pending)), openCards ? openCards.split("\n") : []);
    return [placed.items, all.filter((i) => i.kind === "user" && i.pending), placed.loose] as const;
  }, [all, openCards]);
  const followCards = cards ? cardsKey(cards.t) : "";
  const previews = useMemo(() => items.flatMap((it) => (it.kind === "preview" ? [it.card] : [])), [items]);
  // V0.41: outcome at full weight, the work quiet and folded.
  const entries = useMemo(() => pace(items, { details }), [items, details]);
  const ref = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  // V0.scroll-to-bottom: away from the end, an arrow takes you back there (desktop and phone).
  const [away, setAway] = useState(false);
  // V0.conversation-jumps: the stops (the owner's messages, the Run cards that wait for him), where the owner left the
  // end (for "3 new" on the arrow), the stop a jump just marked, and which jumps have somewhere to go.
  const waitKey = cards ? waitingRequests(cards.t).map((o) => o.id).join("\n") : "";
  const stops = useMemo(() => {
    const waiting = new Set(waitKey ? waitKey.split("\n") : []);
    return new Map(stopsOf(entries, pending, looseCards, waiting).map((s) => [s.key, s.kind]));
  }, [entries, pending, looseCards, waitKey]);
  const itemsNow = useRef(items);
  itemsNow.current = items;
  const [mark, setMark] = useState<string | null>(null);
  const [hit, setHit] = useState<string | null>(null);
  const landed = useRef<string | null>(null);
  const [can, setCan] = useState({ prev: false, next: false, own: false, needs: 0 });

  /** The stops as drawn, in the content's coordinates, and the visible part of it. */
  const measure = (el: HTMLElement) => {
    const box = el.getBoundingClientRect();
    const placed: Placed[] = [...el.querySelectorAll<HTMLElement>("[data-stop]")].map((x) => {
      const r = x.getBoundingClientRect();
      return { key: x.dataset.jk ?? "", kind: x.dataset.stop as StopKind, top: r.top - box.top + el.scrollTop, bottom: r.bottom - box.top + el.scrollTop };
    });
    return { placed, view: { top: el.scrollTop, bottom: el.scrollTop + el.clientHeight } };
  };

  const check = () => {
    const el = ref.current;
    if (!el) return;
    const atEnd = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    stick.current = atEnd;
    setAway(!atEnd);
    onAtEnd(atEnd);
    setMark((m) => (atEnd ? null : m ?? leftAt(itemsNow.current)));
    if (atEnd) return;
    const { placed, view } = measure(el);
    const n = {
      prev: !!pickStop(placed, "own", -1, view, landed.current),
      next: !!pickStop(placed, "own", 1, view, landed.current),
      own: placed.some((s) => s.kind === "own"),
      needs: placed.filter((s) => s.kind === "needs").length,
    };
    // A scroll calls this on every frame: a new object only when something changed, so nothing redraws for nothing.
    setCan((c) => (c.prev === n.prev && c.next === n.next && c.own === n.own && c.needs === n.needs ? c : n));
  };

  const jump = ({ kind, dir }: JumpAsk) => {
    const el = ref.current;
    if (!el) return;
    const { placed, view } = measure(el);
    const key = pickStop(placed, kind, dir, view, landed.current, kind === "needs");
    const s = key && placed.find((x) => x.key === key);
    if (!s) return;
    landed.current = s.key;
    stick.current = false;
    el.scrollTo({ top: Math.max(0, s.top - LAND), behavior: reducedMotion() ? "auto" : "smooth" });
    setHit(s.key);
    check(); // a stop already in view moves nothing, so no scroll event says where the next jump goes
  };
  const jumpNow = useRef(jump);
  jumpNow.current = jump;

  useEffect(() => {
    // The mark is brief: long enough to find it, gone before it is noise.
    if (!hit) return;
    const t = setTimeout(() => setHit(null), 1600);
    return () => clearTimeout(t);
  }, [hit]);

  useEffect(() => {
    // Desktop keys (app-keyboard.tsx); the phone has no keyboard and uses the buttons.
    const on = (e: Event) => jumpNow.current((e as CustomEvent<JumpAsk>).detail);
    window.addEventListener(JUMP_EVENT, on);
    return () => window.removeEventListener(JUMP_EVENT, on);
  }, []);

  const fresh = mark === null ? 0 : newSince(items, mark);

  const toEnd = () => {
    const el = ref.current;
    if (!el) return;
    stick.current = true;
    setAway(false);
    setMark(null);
    el.scrollTo({ top: el.scrollHeight, behavior: reducedMotion() ? "auto" : "smooth" });
  };

  useLayoutEffect(() => {
    stick.current = true;
    setAway(false);
    setMark(null);
    setHit(null);
    landed.current = null;
  }, [threadId]);

  /* eslint-disable react-hooks/exhaustive-deps -- follows new content (all), a new thread or new cards; onAtEnd is the parent's callback, which only updates the seen-tracker through a setter */
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    // V0.38: at the bottom, follow new content without reading the layout first (a read forces a
    // layout of the whole conversation on every update); away from the bottom, nothing moves.
    if (stick.current) {
      el.scrollTop = 1e9;
      setAway(false);
      onAtEnd(true);
    }
  }, [all, threadId, followCards]);
  /* eslint-enable react-hooks/exhaustive-deps */

  /* eslint-disable react-hooks/exhaustive-deps -- one ResizeObserver for the view's life; the check it keeps reads the element and refs, and the onAtEnd it calls updates the seen-tracker through a setter */
  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => {
      if (stick.current) el.scrollTop = el.scrollHeight;
      check();
    });
    ro.observe(el);
    // The content grows after the first paint (late markdown, images, folded work): follow it while at the bottom.
    if (el.firstElementChild) ro.observe(el.firstElementChild);
    return () => ro.disconnect();
  }, []);
  /* eslint-enable react-hooks/exhaustive-deps */

  // V0.34: Copy on code blocks inside rendered markdown (their buttons are plain HTML).
  const onCodeCopy = async (e: MouseEvent) => {
    const b = (e.target as Element | null)?.closest?.("[data-cpy]") as HTMLButtonElement | null;
    if (!b) return;
    e.stopPropagation();
    const code = b.parentElement?.querySelector("pre")?.textContent ?? "";
    if (await copyToClipboard(copySource({ kind: "block", text: code }))) {
      b.textContent = "Copied";
      setTimeout(() => (b.textContent = "Copy"), 1200);
    }
  };
  return (
    <ThreadIdContext.Provider value={threadId}>
    <HostContext.Provider value={host}>
    <RunCards.Provider value={cards ?? null}>
    <div class="convo" ref={ref} onScroll={check} onClick={onCodeCopy}>
      <div class="convo-in">
        {/* V0.75 (2), #64: the filmstrip scrolls with the conversation, so a thread without previews has no empty band */}
        {strip && <div class="cv-strip">{strip}</div>}
        {events === undefined && <div class="sys">Loading…</div>}
        {events !== undefined && items.length === 0 && pending.length === 0 && <div class="sys">No messages yet.</div>}
        {onEarlier && events && events.length > 0 && (
          <button class="btn ghost earlier" onClick={onEarlier}>
            Show earlier messages
          </button>
        )}
        {crashProbe() && (
          <Boundary name="This message">
            <CrashProbe />
          </Boundary>
        )}
        <DeliveryCtx.Provider value={delivery ?? null}>
          {looseCards.map((id) => (
            <Boundary key={`card-${id}`} name="This command">
              <div class={`cv out${hit === looseKey(id) ? " cv-hit" : ""}`} {...stopAttrs(looseKey(id), stops.get(looseKey(id)))}>
                <RunCardAt id={id} />
              </div>
            </Boundary>
          ))}
          {entries.map((en) => (
            <Boundary key={en.key} name="This message">
              <EntryView en={en} previews={previews} stop={stops.get(en.key)} hit={hit === en.key} />
            </Boundary>
          ))}
          {/* V0.next-step-chips: the optional next steps, under the newest message */}
          <Boundary name="The next steps">
            <NextSteps />
          </Boundary>
          {/* V0.message-before-start: a new thread's first message waits alone (the start may wait for the machine); no line above nothing */}
          {pending.length > 0 && items.length > 0 && (
            <div class="here" role="separator" aria-label="The agent has read everything above this line">
              <span>Agent is here</span>
            </div>
          )}
          {pending.map((it) => (
            <Boundary key={it.key} name="This message">
              <div class={`cv out own${hit === it.key ? " cv-hit" : ""}`} {...stopAttrs(it.key, stops.get(it.key))}>
                <ItemView it={it} previews={previews} />
              </div>
            </Boundary>
          ))}
        </DeliveryCtx.Provider>
      </div>
      {away && (
        <div class="cv-end">
          <div class="cv-jb">
            {/* V0.conversation-jumps: the next card that waits for the owner, the owner's previous and next message, then
                the end with what arrived since he left it. One bar on desktop and phone; the phone's targets are 44 px (phone.css, touch size). */}
            {can.needs > 0 && (
              <button type="button" class="cv-jn" aria-label={`The next card that waits for you (${can.needs})`} title="The next card that waits for you (w)" onClick={() => jump({ kind: "needs", dir: 1 })}>
                Needs you{can.needs > 1 ? ` · ${can.needs}` : ""}
              </button>
            )}
            {can.own && (
              <div class="cv-jp" role="group" aria-label="Your messages">
                <button type="button" class="cv-jo" disabled={!can.prev} aria-label="Your previous message" title="Your previous message ([)" onClick={() => jump({ kind: "own", dir: -1 })}>
                  <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m18 15-6-6-6 6" /></svg>
                </button>
                <button type="button" class="cv-jo" disabled={!can.next} aria-label="Your next message" title="Your next message (])" onClick={() => jump({ kind: "own", dir: 1 })}>
                  <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg>
                </button>
              </div>
            )}
            <button type="button" class={`cv-endb${fresh ? " new" : ""}`} aria-label={fresh ? `Scroll to the newest message, ${fresh} new` : "Scroll to the newest message"} title="Scroll to the newest message" onClick={toEnd}>
              <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 5v14" /><path d="m19 12-7 7-7-7" /></svg>
              {fresh > 0 && <span class="cv-new">{fresh} new</span>}
            </button>
          </div>
        </div>
      )}
    </div>
    </RunCards.Provider>
    </HostContext.Provider>
    </ThreadIdContext.Provider>
  );
}

/** V0.33: the quiet delivery word under a message, with Cancel while it waits and Send again once it is gone. */
function DeliveryLine({ it }: { it: Extract<Item, { kind: "user" }> }) {
  const acts = useContext(DeliveryCtx);
  const [busy, setBusy] = useState(false);
  const word = deliveryLabel(it);
  if (!word) return null;
  const gone = it.delivery === "cancelled" || it.delivery === "lost";
  return (
    <div class={`dl${it.from ? " from" : ""}`}>
      <span>{busy ? "Cancelling…" : word}</span>
      {it.delivery === "queued" && it.id && acts && (
        <button
          class="dlb"
          disabled={busy}
          title="Take this message back before the agent reads it"
          onClick={async () => {
            setBusy(true);
            try {
              await acts.cancel(it.id!);
            } finally {
              setBusy(false);
            }
          }}
        >
          Cancel
        </button>
      )}
      {gone && !it.from && acts && (
        <button class="dlb" onClick={() => void acts.resend(it.attachments.length ? { text: it.text, attachments: it.attachments.map((a) => a.id) } : { text: it.text })}>
          Send again
        </button>
      )}
    </div>
  );
}


/** V0.conversation-jumps: a stop's wrapper names its kind and key, so a jump finds it where it is drawn. */
function stopAttrs(key: string, kind: StopKind | undefined) {
  return kind ? { "data-stop": kind, "data-jk": key } : {};
}

/** V0.41: one paced entry. The wrapper's class carries the rhythm: more room before an outcome, little between steps. */
function EntryView({ en, previews, stop, hit }: { en: Entry; previews: PreviewCard[]; stop?: StopKind; hit?: boolean }) {
  if (en.kind === "fold") return <div class="cv proc"><Fold en={en} previews={previews} /></div>;
  const cls = en.role === "outcome" ? (en.item.kind === "user" ? "out own" : "out") : en.role === "end" ? "end" : "proc";
  return (
    <div class={`cv ${cls}${hit ? " cv-hit" : ""}`} {...stopAttrs(en.key, stop)}>
      <ItemView it={en.item} previews={previews} process={en.role === "process"} />
    </div>
  );
}


/** V0.41: the agent's steps between two outcomes as one quiet line; a tap shows every step in order. */
function Fold({ en, previews }: { en: Extract<Entry, { kind: "fold" }>; previews: PreviewCard[] }) {
  const [open, setOpen] = useState(false);
  const toggle = () => setOpen(!open);
  const time = en.durationMs !== null ? duration(en.durationMs) : "";
  return (
    <div class={`foldw${open ? " open" : ""}`}>
      <div class={`tl fold${en.tone === "recovered" ? " rec" : ""}`} role="button" tabIndex={0} aria-expanded={open} data-keys="self" title={en.text} onClick={toggle} onKeyDown={keyActivate(toggle)}>
        <StateIcon state={TONE_ICON[en.tone]} size={12} />
        <span class="tlt ell">
          {en.text}
          {en.failed > 0 && <b class="tlerr"> · {en.failed} failed</b>}
        </span>
        <span class="d">
          <span class="fst">
            {en.steps} steps{time ? " · " : ""}
          </span>
          {time} {open ? "▾" : "▸"}
        </span>
      </div>
      {open && (
        <div class="foldb">
          {en.items.map((it) => (
            <div class="cv proc" key={it.key}>
              <ItemView it={it} previews={previews} process />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function ItemView({ it, previews, process }: { it: Item; previews: PreviewCard[]; process?: boolean }) {
  const host = useContext(HostContext);
  const api = useApi();
  switch (it.kind) {
    case "runcard":
      return <RunCardAt id={it.action} />;
    case "user": {
      // V0.33: unread messages are dimmed; a cancelled or lost one is struck through.
      const cls = it.pending ? " pend" : it.delivery === "cancelled" || it.delivery === "lost" ? " gone" : "";
      if (it.from === "view")
        // V0.37: a press in the live view, with the View tag.
        return (
          <div class={`youw${cls}`}>
            <div class="you prose vmsg vchip" title="Pressed in the live view">
              <span class="vk">View</span>
              {it.text}
            </div>
            <DeliveryLine it={it} />
          </div>
        );
      if (it.from)
        return (
          <div class={`youw from${cls}`}>
            <FromThreadMessage fromId={it.from} text={it.text}>
              {it.attachments.length > 0 && <SentAttachments items={it.attachments} host={host} />}
            </FromThreadMessage>
            <DeliveryLine it={it} />
          </div>
        );
      return (
        <div class={`youw${cls}`}>
          <div class="you prose">
            {it.text}
            {it.attachments.length > 0 && <SentAttachments items={it.attachments} host={host} />}
            {it.images > 0 && !it.attachments.length && (
              <span class="imgchip">
                {it.images} {it.images === 1 ? "image" : "images"}
              </span>
            )}
          </div>
          <DeliveryLine it={it} />
        </div>
      );
    }
    case "assistant":
      if (it.quiet) return <QuietLine icon="seen" line={it.text.split("\n").find((x) => x.trim()) ?? ""} full={it.text} />;
      return (
        <div class={`msgw${process ? " proc" : ""}`}>
          {/* V0.149 (8): a command the message asks the owner to run is a card (Copy only), never a block that scrolls */}
          {splitProse(it.text).map((part, i) =>
            part.kind === "cmd" ? <ProseCommand key={i} text={part.text} /> : <div key={i} class="cl prose md" onClick={openMarkdownImage} dangerouslySetInnerHTML={{ __html: renderMarkdown(part.text, host, api.attachmentUrl) }} />,
          )}
          <CopyButton text={copySource({ kind: "message", text: it.text })} label="Copy message" cls="msgcp" />
        </div>
      );
    case "tools":
      return <ToolCard calls={it.calls} />;
    case "run":
      return <RunMark it={it} />;
    case "effect":
      return <QuietLine icon={it.ok ? "settled" : "failed"} line={it.line} full={`${it.action}\n\n${it.text}`} fail={!it.ok} />;
    case "note":
      return <QuietLine icon="seen" line={`Fyr: ${it.line}`} full={it.text} />;
    case "undeclared":
      return <QuietLine icon="seen" line={it.line} full={it.text} und />; // V0.164 (N9)
    case "preview":
      return <PreviewCardView card={it.card} all={previews} />;
    case "task":
      return <QuietLine icon={it.started ? "working" : TASK_ICON[it.status] ?? "settled"} line={it.text} fail={it.status === "failed"} />;
    case "answer":
      // V0.ask-cancel: a cancelled card says so, and its questions are one tap away.
      return it.cancelled ? <QuietLine icon="seen" line="Question cancelled" full={it.text} /> : <QuietLine icon="settled" line={`Answered: ${it.text}`} />;
    case "handover":
      return <HandoverDivider it={it} />;
  }
}

/** V0.45, V0.trans-host-handover: "Handed over from Claude to Claude (acme) · 17:20", and cross-host link when moved between hosts. */
function HandoverDivider({ it }: { it: Extract<Item, { kind: "handover" }> }) {
  const profiles = profilesOf({ hostId: useContext(HostContext) }, useContext(ProfilesContext)); // the thread's host's
  const dir = useDirectory();
  const link = it.toHostId && it.toThread
    ? { ref: ref(it.toHostId, it.toThread), label: `Open on ${it.toHost || "target"} →` }
    : it.fromHostId && it.fromThread
      ? { ref: ref(it.fromHostId, it.fromThread), label: `View source on ${it.fromHost || "source"}` }
      : null;
  return (
    <div class="hodiv" role="separator">
      <span class="hot">
        <span>{dividerLine(it, profiles, clockTime(it.atIso))}</span>
        <small title={it.note}>
          {rungDone(it.rung)}
          {it.note ? ` · ${it.note}` : ""}
        </small>
        {link && (
          <button type="button" class="ho-linkbtn" onClick={() => dir.openThread(link.ref)}>
            {link.label}
          </button>
        )}
      </span>
    </div>
  );
}

