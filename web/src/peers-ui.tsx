// SPEC V0.159, decision 0030 3.6: the installs panel of Settings → Hosts, one panel on the desktop and the phone. The
// installs this one is paired with and how each came in; the tailnet nodes that are not, with why; Pair (open an invite
// here: text, a QR code, a link; 10 minutes, once) and Add (type one from another install). #259: an install found on
// the LAN shows its id, address and who found it, with Pair (Add, bound to its id and address) and Ignore.
import { useEffect, useRef, useState } from "preact/hooks";
import type { Api } from "./api.ts";
import { candidateWarn, candidateWords, configNote, inviteFromUrl, inviteId, memberLink, needsInvite, others, pairAsks, typedInvite, viaWords, type InviteInfo, type PeerState } from "./peers.ts";
import { Group, Row, useSet } from "./settings-rows.tsx";

type PeersApi = Pick<Api, "peerState" | "peerAct">;

/** The roster as the page reads it; reloads every 5 s while shown. */
export function usePeerState(api: Partial<PeersApi>, live: boolean): { s: PeerState | null; err: string | null; reload(): void } {
  const [s, setS] = useState<PeerState | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const load = () => {
    if (!api.peerState) return;
    void api.peerState().then((r) => (r.ok ? (setS(r.data), setErr(null)) : setErr(r.error)));
  };
  /* eslint-disable react-hooks/exhaustive-deps -- polls while live and reloads when the peer API appears (a boolean); load is a new function each render */
  useEffect(() => {
    load();
    if (!live) return;
    const t = setInterval(load, 5000);
    return () => clearInterval(t);
  }, [live, !!api.peerState]);
  /* eslint-enable react-hooks/exhaustive-deps */
  return { s, err, reload: load };
}

export function PeersPanel({ api, st }: { api: PeersApi; st: { s: PeerState | null; err: string | null; reload(): void } }) {
  const env = useSet();
  const { s, reload } = st;
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);
  const [invite, setInvite] = useState<InviteInfo | null>(null);
  const [code, setCode] = useState(() => (typeof location === "undefined" ? "" : inviteFromUrl(location.search)));
  const [addr, setAddr] = useState("");
  const [confirm, setConfirm] = useState<string | null>(null);
  /** #259: the found install Pair was pressed on: Add takes only an invite with its id */
  const [target, setTarget] = useState<{ id: string; name: string; addr: string } | null>(null);
  const [showIgnored, setShowIgnored] = useState(false);
  const codeRef = useRef<HTMLInputElement>(null);
  const inviteUntil = invite?.until;
  useEffect(() => {
    if (!inviteUntil) return;
    const t = setTimeout(() => setInvite(null), Math.max(0, Date.parse(inviteUntil) - Date.now()));
    return () => clearTimeout(t);
  }, [inviteUntil]);
  if (!s) return null;
  const signed = s.signedIn !== false;
  const ro = env.readOnly || busy || !signed;

  const pair = async () => {
    setBusy(true);
    setMsg(null);
    setOk(null);
    const r = await api.peerAct("invite", {});
    setBusy(false);
    if (!r.ok) return setMsg(String(r.error ?? "That did not work"));
    env.saved();
    setInvite({ code: String(r.code), until: String(r.until), link: String(r.link), svg: r.svg as string | undefined });
  };
  const add = async () => {
    setBusy(true);
    setMsg(null);
    setOk(null);
    const r = await api.peerAct("join", { code, addr: addr.trim() });
    setBusy(false);
    if (!r.ok) return setMsg(String(r.error ?? "That did not work"));
    env.saved();
    setCode("");
    setAddr("");
    setTarget(null);
    setOk(`Paired with ${r.name}.`);
    reload();
  };

  const remove = async (id: string) => {
    setConfirm(null);
    setBusy(true);
    setMsg(null);
    setOk(null);
    const r = await api.peerAct("remove", { id });
    setBusy(false);
    if (!r.ok) return setMsg(String(r.error ?? "That did not work"));
    env.saved();
    reload();
  };
  const ignore = async (id: string, on: boolean) => {
    setBusy(true);
    setMsg(null);
    setOk(null);
    const r = await api.peerAct("ignore", { id, on });
    setBusy(false);
    if (!r.ok) return setMsg(String(r.error ?? "That did not work"));
    env.saved();
    reload();
  };
  // Pair on a found install: the code comes from that install, so Add is bound to its id and address.
  const pairWith = (c: { id: string; name: string; addr: string }) => {
    setConfirm(null);
    setTarget(c);
    setAddr(c.addr);
    if (code && inviteId(code) !== c.id) setCode("");
    codeRef.current?.scrollIntoView?.({ block: "center" });
    codeRef.current?.focus();
  };
  const wrongCode = !!target && code.startsWith("FYR1-") && inviteId(code) !== target.id;
  const ignored = s.ignored ?? [];
  const rest = others(s);
  const cfg = configNote(s);
  return (
    <div class="peers" data-testid="peers">
      {cfg && (
        <div class="note bad cfgchanged" role="status">
          <b>{cfg.title}.</b> {cfg.how}
        </div>
      )}
      {msg && (
        <div class="seterr" role="alert">
          {msg}
        </div>
      )}
      {ok && (
        <div class="note" role="status">
          {ok}
        </div>
      )}
      <Group label="Paired installs" title="Paired installs" foot="Installs that know each other by key. A paired install reads every host, and drives it unless that host switched it off (fyr host drive). Remove ends an install for ever: its link closes, and what it signed counts for nothing.">
        {s.members.map((m) => (
          <Row
            key={m.id}
            class="peerrow"
            mono
            ell
            title={m.name}
            desc={[`${viaWords(m)} · ${m.id.slice(0, 4)}`, memberLink(m, Date.now())].filter(Boolean).join(" · ")}
            end={
              m.self ? undefined : confirm === m.id ? (
                <>
                  <button type="button" class="btn danger" disabled={ro} onClick={() => void remove(m.id)}>
                    Remove
                  </button>
                  <button type="button" class="btn" onClick={() => setConfirm(null)}>
                    Cancel
                  </button>
                </>
              ) : (
                <button type="button" class="btn" disabled={ro} onClick={() => setConfirm(m.id)}>
                  Remove…
                </button>
              )
            }
          />
        ))}
        {rest.length === 0 && <Row class="peerrow" title="No other install is paired." desc="Installs of one tailnet user pair by themselves; others pair with an invite." />}
      </Group>
      {(s.candidates.length > 0 || ignored.length > 0) && (
        <Group label="Not paired" title="Not paired" foot="Tailnet nodes and installs found on this LAN that are not in the roster. Being found gives an install nothing: only pairing does, and Pair asks for that install's code. An install you do not know is probably someone else's: ignore it.">
          {s.candidates.map((c) => {
            if (c.kind !== "lan" || !c.id)
              return <Row key={c.name} class={`peercand${c.online ? "" : " off"}`} mono ell title={c.name} desc={candidateWords(c)} end={needsInvite(c) ? <span class="pill">needs an invite</span> : undefined} />;
            const id = c.id;
            const warn = candidateWarn(c);
            const t = { id, name: c.name, addr: c.addr ?? "" };
            return (
              // stack: on the phone the two buttons go under the text (two 44 px buttons do not fit beside it).
              <Row
                key={id}
                class={`peercand lan${c.spoof ? " bad" : ""}`}
                stack
                mono
                ell
                title={c.name}
                desc={
                  <>
                    {candidateWords(c)}
                    {warn && <span class="cwarn">{warn}</span>}
                  </>
                }
                end={
                  confirm === `pair:${id}` ? (
                    <>
                      <button type="button" class="btn" disabled={ro} onClick={() => pairWith(t)}>
                        Pair anyway
                      </button>
                      <button type="button" class="btn" onClick={() => setConfirm(null)}>
                        Cancel
                      </button>
                    </>
                  ) : (
                    <>
                      <button type="button" class="btn" disabled={ro} onClick={() => void ignore(id, true)}>
                        Ignore
                      </button>
                      <button type="button" class="btn" disabled={ro} onClick={() => (pairAsks(c) ? setConfirm(`pair:${id}`) : pairWith(t))}>
                        Pair…
                      </button>
                    </>
                  )
                }
              />
            );
          })}
          {ignored.length > 0 && (
            <Row
              class="peerign"
              title={
                <button type="button" class="lnk" aria-expanded={showIgnored} onClick={() => setShowIgnored(!showIgnored)}>
                  {showIgnored ? "Hide ignored" : `Show ignored (${ignored.length})`}
                </button>
              }
            />
          )}
          {showIgnored &&
            ignored.map((g) => (
              <Row
                key={`ign:${g.id}`}
                class="peercand ign"
                mono
                ell
                title={g.id}
                desc={[`Ignored ${new Date(g.at).toLocaleDateString(undefined, { day: "numeric", month: "short" })}`, g.addr ? `heard at ${g.addr}` : "not heard now"].join(" · ")}
                end={
                  <button type="button" class="btn" disabled={ro} onClick={() => void ignore(g.id, false)}>
                    Stop ignoring
                  </button>
                }
              />
            ))}
        </Group>
      )}
      <Group label="Pair" title="Pair" foot="Open an invite here and type it on the other install (fyr pair CODE). It works once, for 10 minutes, and only for the install it is typed on.">
        <Row
          title="New invite"
          end={
            <button type="button" class="btn" disabled={ro} onClick={() => void pair()}>
              Pair
            </button>
          }
        />
        {invite && (
          <Row col class="addout" title="Invite">
            {invite.svg && <img class="qrimg" alt="QR code of the invite" width="192" height="192" src={`data:image/svg+xml;utf8,${encodeURIComponent(invite.svg)}`} />}
            <span class="inl linkout">
              <input class="mono" readOnly aria-label="Invite" value={invite.code} onFocus={(e) => (e.target as HTMLInputElement).select()} />
            </span>
          </Row>
        )}
      </Group>
      <Group label="Add" title="Add" foot="Type the invite another install made. Give its address (host:port) when this one cannot find it by itself.">
        {target && (
          <Row
            class="pairfor"
            mono
            title={`Pairing with ${target.name}`}
            desc={`Open an invite on that install (Settings → Hosts → Pair, or fyr pair there) and type it here. Only an invite from ${target.id} is taken.`}
            end={
              <button type="button" class="btn" onClick={() => (setTarget(null), setAddr(""))}>
                Cancel
              </button>
            }
          />
        )}
        <Row
          stack
          title="Invite"
          error={wrongCode ? `That invite is from ${inviteId(code)}, not ${target?.id}.` : null}
          end={
            <span class="inl">
              <input ref={codeRef} class="mono" type="text" aria-label="Invite from another install" autocomplete="off" spellcheck={false} placeholder="fyr1-…" value={code} disabled={ro} onInput={(e) => setCode(typedInvite((e.target as HTMLInputElement).value))} />
            </span>
          }
        />
        <Row
          stack
          title="Address"
          end={
            <span class="inl">
              <input class="mono" type="text" aria-label="Address of the other install" autocomplete="off" spellcheck={false} placeholder="host:port (optional)" value={addr} disabled={ro} onInput={(e) => setAddr((e.target as HTMLInputElement).value)} />
              <button type="button" class="btn" disabled={ro || !code.startsWith("FYR1-") || wrongCode} onClick={() => void add()}>
                Add
              </button>
            </span>
          }
        />
      </Group>
      {!signed && <div class="note">Sign in to pair installs: run <span class="mono">fyr open</span> on the machine that runs Fyr.</div>}
    </div>
  );
}
