// SPEC V0.159, decision 0030 3.6: the pairing panel of Settings → Hosts, one panel on the desktop and the phone, under
// the list of hosts. V0.settings-ia: in the order of use. Add a host: the two ways, each named for what its press does
// (Create invite: text, a QR code, a link; 10 minutes, once; Join: type an invite another host made). Found, not
// paired: the tailnet nodes and LAN hosts that are not paired, with why, folded with their count (#259: one found on
// the LAN shows its id, address and who found it, with Join… bound to its id and address, and Ignore). Pairing: the
// hosts this one is paired with and how each came in, folded; Remove asks in the confirmation dialog.
import { useEffect, useRef, useState } from "preact/hooks";
import type { Api } from "./api.ts";
import { candidateWarn, candidateWords, configNote, inviteFromUrl, inviteId, memberLink, needsInvite, others, pairAsks, typedInvite, viaWords, type InviteInfo, type PeerState } from "./peers.ts";
import { Group, LoadError, Row, Skeleton, useSet } from "./settings-rows.tsx";
import { useAsk } from "./confirm-ui.tsx";
import { removeHostAsk } from "./settings-asks.ts";

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
  const ask = useAsk();
  const { s, err: readErr, reload } = st;
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [ok, setOk] = useState<string | null>(null);
  const [invite, setInvite] = useState<InviteInfo | null>(null);
  const [code, setCode] = useState(() => (typeof location === "undefined" ? "" : inviteFromUrl(location.search)));
  const [addr, setAddr] = useState("");
  const [confirm, setConfirm] = useState<string | null>(null);
  /** #259: the found host Join… was pressed on: Join takes only an invite with its id */
  const [target, setTarget] = useState<{ id: string; name: string; addr: string } | null>(null);
  const [showIgnored, setShowIgnored] = useState(false);
  const codeRef = useRef<HTMLInputElement>(null);
  const inviteUntil = invite?.until;
  useEffect(() => {
    if (!inviteUntil) return;
    const t = setTimeout(() => setInvite(null), Math.max(0, Date.parse(inviteUntil) - Date.now()));
    return () => clearTimeout(t);
  }, [inviteUntil]);
  // V0.settings-ia: while the roster loads, and when its read fails, the page says so.
  if (!s && readErr)
    return (
      <div class="peers" data-testid="peers">
        <LoadError text={`Could not read the paired hosts: ${readErr}.`} onRetry={reload} />
      </div>
    );
  if (!s)
    return (
      <div class="peers" data-testid="peers">
        <Skeleton rows={2} label="Loading the paired hosts" />
      </div>
    );
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
  // Join… on a found host: the invite comes from that host, so Join is bound to its id and address.
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
  const found = s.candidates.length;
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
      {/* One group for the two ways to add a host. */}
      <Group class="pinvite" label="Add a host" title="Add a host" foot="Either way pairs the two hosts: make an invite here and type it on the other one (fyr pair CODE there), or type here the invite the other one made. An invite works once, for 10 minutes." how="Hosts know each other by key. A paired host reads every host, and drives it unless that host switched it off (fyr host drive). Hosts of one Tailscale login pair by themselves; a host that is not paired goes in the config file (hosts).">
        <Row
          title="Invite from here"
          desc="For the other host to type."
          end={
            <button type="button" class="btn" disabled={ro} onClick={() => void pair()}>
              Create invite
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
        {target && (
          <Row
            class="pairfor"
            mono
            title={`Pairing with ${target.name}`}
            desc={`Create an invite on that host (Settings → Hosts → Add a host, or fyr pair there) and type it here. Only an invite from ${target.id} is taken.`}
            end={
              <button type="button" class="btn" onClick={() => (setTarget(null), setAddr(""))}>
                Cancel
              </button>
            }
          />
        )}
        <Row
          stack
          class="pjoin"
          title="Join with an invite"
          desc="The invite another host made."
          error={wrongCode ? `That invite is from ${inviteId(code)}, not ${target?.id}.` : null}
          end={
            <span class="inl">
              <input ref={codeRef} class="mono" type="text" aria-label="Invite from another host" autocomplete="off" spellcheck={false} placeholder="fyr1-…" value={code} disabled={ro} onInput={(e) => setCode(typedInvite((e.target as HTMLInputElement).value))} />
            </span>
          }
        />
        <Row
          stack
          title="Address of the other host"
          desc="Only when this host cannot find the other one by itself."
          end={
            <span class="inl">
              <input class="mono" type="text" aria-label="Address of the other host" autocomplete="off" spellcheck={false} placeholder="host:port (optional)" value={addr} disabled={ro} onInput={(e) => setAddr((e.target as HTMLInputElement).value)} />
              <button type="button" class="btn" disabled={ro || !code.startsWith("FYR1-") || wrongCode} onClick={() => void add()}>
                Join
              </button>
            </span>
          }
        />
      </Group>
      {(s.candidates.length > 0 || ignored.length > 0) && (
        <Group label="Found, not paired" title="Found, not paired" fold={{ id: "hosts:found", open: false, summary: found ? `${found} found${ignored.length ? ` · ${ignored.length} ignored` : ""}` : `${ignored.length} ignored` }} foot="Tailnet nodes and hosts found on this LAN. Being found gives a host nothing: only pairing does, and Join… asks for that host's invite. A host you do not know is probably someone else's: ignore it.">
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
                        Join anyway
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
                        Join…
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
      {/* Status: who is paired and how. Folded with the count; the hosts themselves are the list above. */}
      <Group
        label="Pairing"
        title="Pairing"
        fold={{ id: "hosts:pairing", open: false, summary: rest.length ? `Paired with ${rest.length} ${rest.length === 1 ? "host" : "hosts"}` : "Not paired with another host" }}
        foot="The hosts that know this one by key. A paired host is in the list above once it is reachable. Remove ends a pairing for ever: its link closes, and what that host signed counts for nothing."
      >
        {s.members.map((m) => (
          <Row
            key={m.id}
            class="peerrow"
            mono
            ell
            title={m.name}
            desc={[`${viaWords(m)} · ${m.id.slice(0, 4)}`, memberLink(m, Date.now())].filter(Boolean).join(" · ")}
            end={
              m.self ? undefined : (
                // for ever, and it reaches another machine: the confirmation dialog, which names the host (settings-asks.ts)
                <button type="button" class="btn" disabled={ro} onClick={async () => (await ask(removeHostAsk(m.name))) && void remove(m.id)}>
                  Remove…
                </button>
              )
            }
          />
        ))}
        {rest.length === 0 && <Row class="peerrow" title="No other host is paired." desc="Hosts of one tailnet user pair by themselves; others pair with an invite." />}
      </Group>
      {!signed && <div class="note">Sign in to pair hosts: run <span class="mono">fyr open</span> on the host.</div>}
    </div>
  );
}
