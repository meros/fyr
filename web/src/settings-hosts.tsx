// The Hosts settings pages (split from settings-ui.tsx).

import { useHostCommand } from "./settings-automation.tsx";
import type { PageUi, SettingsProps } from "./settings-ui.tsx";
import { HostMark, useHostMarks } from "./host-mark-ui.tsx";
import { HOST_ICON_NAMES, hostGlyph, ownerIcon } from "./host-mark.ts";
import { HOST_GLYPHS } from "./lucide.ts";
import { PALETTE_NAMES } from "./projects.ts";
import { useEffect, useState } from "preact/hooks";
import { type ComponentChildren } from "preact";
import { badLine, hostLine, updating, linkLine, hostState, hostRoute, shortID, updateLine, updateRows, updateWords, type HostInfo } from "./hosts.ts";
import { setupLine } from "./projects.ts";
import { SetBy } from "./project-ui.tsx";
import { type PageRef } from "./settings-nav.ts";
import { Group, Row, SaveField, useSet } from "./settings-rows.tsx";
import { Grip, MoveControls, ReorderButton, useReorder } from "./reorder-ui.tsx";

// ---- Hosts (V0.125, decision 0026 slice 1) ----

/** One host in Settings → Hosts, the same row on the desktop and the phone (the phone's 56 px and larger type come from
 *  the one `.set.phone` rule set, for touch): the state mark, the name, one line (state, route, short id); a changed id
 *  replaces the line and is the only red row. The row opens the host's page, where every action is. */
function HostRow({ h, now, go, still }: { h: HostInfo; now: number; go(r: PageRef): void; /** the phone's Reorder mode: the row does not open */ still?: boolean }) {
  const line = hostLine(h, now);
  const link = linkLine(h.link, now); // decision 0030 Amendment 1: how the front reaches it, and why not
  const upd = updateLine(h); // V0.host-updates: "3 commits behind · last update failed" is in the line
  return (
    <Row
      class={upd.bad ? "host bad" : "host"}
      onClick={still ? undefined : () => go({ page: "hosts", sub: h.name })}
      lead={<HostMark name={h.name} size={24} />}
      title={h.name}
      mono
      desc={h.idWas ? undefined : link ? <>{line}<span class="hlink">{link}</span></> : line}
      error={h.idWas ? line : null}
      data={{ host: h.name, sub: h.name, ...(h.id ? { id: h.id } : {}) }}
      end={
        <>
          {h.idWas && <span class="pst needs-login">Needs you</span>}
          <span class="chev">›</span>
        </>
      }
    />
  );
}

const HOSTS_FOOT = (
  <>
    Your other installs join this list once they are paired; installs of your Tailscale login pair by themselves. A host that is not paired goes in fyrd's config (<span class="mono">hosts</span>).
  </>
);

/** The Hosts page: the hosts in the owner's order (V0.132). No buttons: a host gets on the list
 *  by pairing or by the config (V0.retire-discovery), and the foot says so. */
/** V0.132: the owner orders the hosts with the one reorder control (V0.119 (4)): a grip on the desktop, a Reorder mode
 *  with ↑ and ↓ on the phone. The first is the default host. Each move saves at once (front.hostOrder, kept on every
 *  host) and shows on this device first. */
export function HostsPage({ rows, now, go, ui, run }: { rows: HostInfo[]; now: number; go(r: PageRef): void; ui: PageUi; run(names: string[]): Promise<string | null> }) {
  const env = useSet();
  const ro = useReorder(
    rows.map((h) => h.name),
    async (next) => {
      const e = await run(next);
      if (!e) env.saved();
      return e;
    },
  );
  const shown = ro.sort(rows, (h) => h.name); // a move shows here first, until the hosts event catches up
  const can = rows.length > 1 && !env.readOnly;
  const mode = env.phone && ui.reordering && can;
  return (
    <Group
      label="Hosts"
      foot={
        <>
          {rows.length <= 1 && "This is your only Fyr install. "}
          {rows.length > 1 && "The first host is the default for New project, New thread and the folder picker. This order is kept on your hosts, so every device shows it. "}
          {HOSTS_FOOT}
        </>
      }
    >
      {ro.err && (
        <div class="seterr" role="alert">
          {ro.err}
        </div>
      )}
      {shown.map((h, i) => {
        const r = ro.row(i);
        if (mode)
          return (
            <div key={h.name} class={`hreo mvrow ${r.class}`} ref={r.ref} data-sub={h.name}>
              <HostRow h={h} now={now} go={go} still />
              <MoveControls ro={ro} i={i} name={h.name} />
            </div>
          );
        if (!can || env.phone) return <HostRow key={h.name} h={h} now={now} go={go} />;
        return (
          <div key={h.name} class={`hreo g ${r.class}`} ref={r.ref}>
            <Grip {...ro.grip(i, h.name)} />
            <HostRow h={h} now={now} go={go} />
          </div>
        );
      })}
    </Group>
  );
}

/** The phone's Reorder and Done for the Hosts page (touch has no hover for a grip, and a drag fights the scroll). */
export function HostsReorderButton({ ui, n }: { ui: PageUi; n: number }) {
  const env = useSet();
  if (!env.phone || n < 2 || env.readOnly) return null;
  return <ReorderButton on={ui.reordering} set={ui.setReordering} />;
}

/** The status pill at the end of a host's title (desktop) or first on its page (phone: its header has only the title
 *  and Back). */
export function HostPill({ h }: { h: HostInfo }) {
  if (h.idWas) return <span class="pst needs-login">Needs you</span>;
  return <span class="pst">{h.online ? "Online" : "Offline"}</span>;
}

/** An install id in full: four groups, the first (the short form) in ink. */
function IdFull({ id, big }: { id: string; big?: boolean }) {
  return (
    <span class={`idf${big ? " big" : ""}`} aria-label={`id ${id}`}>
      {id.split("-").map((g, i) => (
        <span key={i} class={i ? undefined : "s"}>
          {g}
        </span>
      ))}
    </span>
  );
}

/** Copy writes the id with its dashes; "Copied" for 2 s. Not rendered where the browser has no clipboard API. */
function CopyId({ id }: { id: string }) {
  const [done, setDone] = useState(false);
  useEffect(() => {
    if (!done) return;
    const t = setTimeout(() => setDone(false), 2000);
    return () => clearTimeout(t);
  }, [done]);
  if (typeof navigator === "undefined" || !navigator.clipboard?.writeText) return null;
  return (
    <button type="button" class="btn" aria-label={`Copy the id ${id}`} onClick={() => void navigator.clipboard.writeText(id).then(() => setDone(true), () => {})}>
      {done ? "Copied" : "Copy"}
    </button>
  );
}

/** A row whose text is a label, an id in full and a line under it (the id rows of a host's page). */
function IdRow({ title, id, desc, end, bad }: { title: string; id: string; desc: ComponentChildren; end?: ComponentChildren; bad?: boolean }) {
  return (
    <div class={`sr idr${bad ? " bad" : ""}`}>
      <span class="sr-b">
        <span class="sr-t">{title}</span>
        <IdFull id={id} big />
        <span class="sr-d">{desc}</span>
      </span>
      {end && <span class="sr-e">{end}</span>}
    </div>
  );
}

const sentence = (s: string) => {
  const t = s.trim();
  if (!t) return "";
  return `${t[0].toUpperCase()}${t.slice(1)}${/[.!?]$/.test(t) ? "" : "."}`;
};

/** A host's page: Identity (the full id, Copy, how Fyr trusts it today) and Connection (state, address and route, how
 *  it got on the list). A changed id: the note, both ids in full and Trust <new> (front.hostId); the foot says what
 *  Fyr does meanwhile. The same page on the desktop and the phone. */
const GLYPH_WORDS: Record<string, string> = { laptop: "a laptop (it has a battery)", monitor: "a desktop", server: "a server" };

/** V0.171: the host's mark, set by the owner: an icon (Automatic, one of the glyphs, or an emoji) and a colour
 *  (Automatic or one of the ten hues). Kept by its install id on every host (front.hostMark); the same group on the
 *  desktop and the phone. V0.208: as a project's fields, an agent fills what the owner has not set ("Set by agent"
 *  until the owner changes it), and Set up automatically runs it on that host. */
function HostMarkGroup({ h, api }: { h: HostInfo; api: SettingsProps["api"] }) {
  const env = useSet();
  const marks = useHostMarks();
  const [err, setErr] = useState<string | null>(null);
  const [setupErr, setSetupErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  if (!h.id)
    return (
      <Group title="Mark">
        <Row title={<HostMark name={h.name} size={32} />} desc="Automatic. This host runs a Fyr older than V0.124 and has no install id, so its mark cannot be set until its next update." />
      </Group>
    );
  const own = marks[h.id] ?? {};
  const icon = ownerIcon(own.icon);
  const auto = hostGlyph(h.machine);
  const set = async (patch: { icon?: string; color?: string }) => {
    const next = { icon: own.icon ?? "", color: own.color ?? "", ...patch };
    setBusy(true);
    const r = await api.frontCommand("front.hostMark", { id: h.id, ...next });
    setBusy(false);
    if (r.ok) {
      setErr(null);
      env.saved();
    } else setErr(r.error || "the mark could not be saved");
  };
  const off = env.readOnly || busy;
  const iconBy = !!own.icon && own.by?.icon === "agent";
  const colorBy = !!own.color && own.by?.color === "agent";
  const setter = (own.icon && !iconBy) || (own.color && !colorBy) ? "you" : "an agent";
  const setup0 = setupLine({ setup: own.setup });
  const setup = async () => {
    setBusy(true);
    const r = await api.command(h.id!, "host.setupRun");
    setBusy(false);
    setSetupErr(r.ok ? null : (r.error ?? "That did not work"));
  };
  return (
    <Group title="Mark" foot="Kept on every host by its id, so every device shows it.">
      <Row
        class="hmprev"
        title={<HostMark name={h.name} size={32} />}
        desc={!own.icon && !own.color ? `Automatic: ${GLYPH_WORDS[auto]}, a colour from its id.` : `Set by ${setter}.${!own.icon ? ` The icon is automatic: ${GLYPH_WORDS[auto]}.` : ""}${!own.color ? " The colour is automatic." : ""}`}
        error={err}
      />
      <div class="sr col hmicon">
        <span class="sr-t">Icon{iconBy && <SetBy of="host" />}</span>
        <div class="igrid" role="radiogroup" aria-label="Icon">
          <button type="button" class={`ig auto${!icon ? " on" : ""}`} role="radio" aria-checked={!icon} title="Automatic" aria-label="Automatic" disabled={off} onClick={() => icon && void set({ icon: "" })}>
            Auto
          </button>
          {HOST_ICON_NAMES.map((nm) => {
            const on = !!icon && "glyph" in icon && icon.glyph === nm;
            return (
              <button type="button" class={`ig${on ? " on" : ""}`} role="radio" aria-checked={on} title={nm} aria-label={nm} disabled={off} onClick={() => !on && void set({ icon: nm })}>
                <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" dangerouslySetInnerHTML={{ __html: HOST_GLYPHS[nm] }} />
              </button>
            );
          })}
        </div>
        <span class="pemoji">
          <SaveField value={icon && "emoji" in icon ? icon.emoji : ""} label="Emoji" placeholder="Or an emoji" onError={setErr} check={(v) => (v.trim() && !ownerIcon(v.trim()) ? "One emoji" : null)} onSave={async (v) => {
              // Empty gives the automatic icon back; otherwise the first emoji typed.
              const i = ownerIcon(v.trim());
              await set({ icon: i && "emoji" in i ? i.emoji : "" });
              return null;
            }} />
        </span>
      </div>
      <Row
        title={<>Colour{colorBy && <SetBy of="host" />}</>}
        stack
        end={
          <div class="swatches" role="radiogroup" aria-label="Colour">
            <button type="button" class={`sw auto${!own.color ? " on" : ""}`} role="radio" aria-checked={!own.color} aria-label="Automatic" title="Automatic" disabled={off} onClick={() => own.color && void set({ color: "" })}>
              A
            </button>
            {PALETTE_NAMES.map((c) => (
              <button type="button" class={`sw${c === own.color ? " on" : ""}`} role="radio" aria-checked={c === own.color} aria-label={c} title={c} style={{ "--pc": `var(--p-${c})` }} disabled={off} onClick={() => c !== own.color && void set({ color: c })} />
            ))}
          </div>
        }
      />
      <Row
        title="Set up automatically"
        class="psetup"
        desc={!setupErr && !setup0?.error ? <>An agent on this host looks at the machine and fills in what you have not set.{!h.online ? " Needs the host online." : setup0 ? ` ${setup0.text}` : ""}</> : undefined}
        error={setupErr ?? (setup0?.error ? setup0.text : null)}
        end={
          <button type="button" class="btn" disabled={off || !h.online} onClick={() => void setup()}>
            {own.setup ? "Run again" : "Run setup"}
          </button>
        }
      />
    </Group>
  );
}

export function HostPage({ h, api, now }: { h: HostInfo; api: SettingsProps["api"]; now: number }) {
  const env = useSet();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  // Decision 0031 2.6: the host the front runs in is the one its link is the pipe to, a fact about the link.
  const isSelf = h.link?.via === "pipe";
  const route = hostRoute(h);
  const trust = async () => {
    setBusy(true);
    const r = await api.frontCommand("front.hostId", { host: h.id });
    setBusy(false);
    if (r.ok) {
      setErr(null);
      env.saved(); // the hosts stream event makes this page the ordinary one
    } else setErr(r.error || "the id could not be saved");
  };
  const state = hostState({ ...h, deploy: undefined }, now);
  const TRUST_ROW = !isSelf && <Row title="Trust" desc="Through your tailnet: Fyr follows it because it is a node of your Tailscale login, not because of its key." />;
  const identity =
    h.idWas && h.id ? (
      <Group title="Identity" foot={`Fyr still follows ${h.name} meanwhile: today it trusts a host through your tailnet login, not its key. If you do not know this install, take that machine off your tailnet.`}>
        <IdRow title="It was" id={h.idWas} desc={`The id Fyr first saw for ${h.name}.`} />
        <IdRow title="It is now" id={h.id} bad desc={`To check, open Fyr on ${h.name}: its Hosts page shows its own id.`} end={<CopyId id={h.id} />} />
        <Row
          class="trust"
          title={`Trust ${shortID(h.id)} as ${h.name}`}
          desc={err ? undefined : "Fyr then remembers the new id and stops warning."}
          error={err ? `Not trusted: Fyr answered “${err}”. Nothing changed.` : null}
          end={
            <button type="button" class="btn" disabled={env.readOnly || busy} onClick={() => void trust()}>
              Trust {shortID(h.id)}
            </button>
          }
        />
      </Group>
    ) : (
      <Group title="Identity">
        {h.id ? (
          <IdRow
            title="Id"
            id={h.id}
            desc={isSelf ? `The id of the Fyr you are using. Your other installs show this id for ${h.name}.` : "Fyr remembers this id and tells you when another install answers under this name."}
            end={<CopyId id={h.id} />}
          />
        ) : (
          <Row title="Id" desc="None. This host runs a Fyr older than V0.124, which has no install key. It shows an id after its next update." />
        )}
        {TRUST_ROW}
      </Group>
    );
  const under = !h.online ? (
    <>
      {h.error && <span class="err">{sentence(h.error)} </span>}
      Its threads show as last read and cannot be changed.
    </>
  ) : h.deploy && updateWords(h) ? (
    `Updating Fyr: ${updateWords(h)}`
  ) : undefined;
  return (
    <>
      {h.idWas && h.id && (
        <div class="note bad" role="alert">
          <span>
            <b>Another install answers as {h.name}.</b> That is normal after you reinstalled or replaced {h.name}. It is wrong if you did not.
          </span>
        </div>
      )}
      {identity}
      {/* V0.171: after Identity, so a changed id and its Trust stay first. */}
      <HostMarkGroup h={h} api={api} />
      {h.update && <HostUpdatesGroup h={h} api={api} now={now} />}
      <Group title="Connection">
        <Row class="hstate" title={state[0].toUpperCase() + state.slice(1)} desc={under} />
        {h.url && (
          <Row
            title="Address"
            desc={
              <>
                <span class="mono">{h.url}</span>
                {route && ` · ${route}`}
              </>
            }
          />
        )}
        {!isSelf &&
          (h.found ? (
            <Row title="Paired" desc="Fyr follows it over its peer link, as one of your installs. It leaves this list when it is removed from them." />
          ) : (
            <Row
              title="Set in this front's config"
              desc={
                <>
                  It stays on this list until it is taken out of <span class="mono">hosts</span> there.
                </>
              }
            />
          ))}
      </Group>
    </>
  );
}

/** V0.host-updates (#154): what the host runs, the newest release and how far behind, its last update, and a release it
 *  holds back with Update now · retry, which clears the mark. One component for the desktop and the phone. */
function HostUpdatesGroup({ h, api, now }: { h: HostInfo; api: SettingsProps["api"]; now: number }) {
  const env = useSet();
  const run = useHostCommand(h.id, api);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const u = h.update!;
  const press = async () => {
    setBusy(true);
    setErr(await run("host.updateNow", {}));
    setBusy(false);
  };
  const label = u.paused ? "Back to release" : u.bad ? "Update now · retry" : "Update now";
  const offer = h.online && (u.bad || u.paused || u.behind !== 0);
  return (
    <Group title="Updates" label="Host updates" foot={u.bad ? badLine(u.bad, now) : undefined}>
      {updateRows(u, now).map((r) => (
        <Row key={r.title} title={r.title} desc={r.desc} class={r.bad ? "bad" : undefined} />
      ))}
      {offer && (
        <Row
          title={u.bad ? `Try ${u.bad.commit.slice(0, 7)} again` : u.paused ? "Go back to the release" : "Install the newest release now"}
          error={err}
          end={
            <button type="button" class="btn" disabled={env.readOnly || busy || updating(h)} onClick={() => void press()}>
              {label}
            </button>
          }
        />
      )}
    </Group>
  );
}

/** V0.58: every project with its icon, folder and thread count; a row opens the project's page. Search from nine
 *  projects. V0.121: in the owner's one order, which this page sets with the one reorder control (a grip on the
 *  desktop, a Reorder mode with ↑ and ↓ on the phone); quiet projects are listed and keep their place. V0.125: the hosts have their own page. */
