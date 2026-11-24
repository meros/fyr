// SPEC V0.157, decision 0030 3.4: Settings → Sign-in (V0.settings-ia: was Devices), one page on the desktop and the
// phone. In this order: the devices that wait, with Allow and Deny; the ones that are signed in, with Sign out; Add a
// device (a link and a QR code, valid once for 10 minutes); Allow by code; whether signing in is required; and last,
// how this device gets in. The width is the only difference.
import { type ComponentChildren } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { Api } from "./api.ts";
import { useAsk } from "./confirm-ui.tsx";
import { autoNotice, codeShow, deviceLine, devicesNeed, modeLine, splitDevices, typedCode, type AuthState, type DeviceRow } from "./devices.ts";
import { requireSignInAsk } from "./settings-asks.ts";
import { ConfirmButton, Empty, Group, LoadError, Row, Skeleton, Switch, useSet } from "./settings-rows.tsx";

type DevicesApi = Pick<Api, "authState" | "authAct">;

/** The state of Settings → Sign-in, read by the page and by the nav's line; it reloads every 5 s while shown. */
export function useAuthState(api: Partial<DevicesApi>, live: boolean): { s: AuthState | null; err: string | null; reload(): void } {
  const [s, setS] = useState<AuthState | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const load = () => {
    if (!api.authState) return;
    void api.authState().then((r) => (r.ok ? (setS(r.data), setErr(null)) : setErr(r.error)));
  };
  /* eslint-disable react-hooks/exhaustive-deps -- polls while live and reloads when the auth API appears (a boolean); load is a new function each render */
  useEffect(() => {
    load();
    if (!live) return;
    const t = setInterval(load, 5000);
    return () => clearInterval(t);
  }, [live, !!api.authState]);
  /* eslint-enable react-hooks/exhaustive-deps */
  return { s, err, reload: load };
}

interface Added {
  url: string;
  svg?: string;
  name: string;
}

export function DevicesPage({ api, now, st, bottom }: { api: DevicesApi; now: number; st: { s: AuthState | null; err: string | null; reload(): void }; /** the page's last group (how this device gets in), which shows whatever the list's read does */ bottom?: ComponentChildren }) {
  const env = useSet();
  const ask = useAsk();
  const { s, err, reload } = st;
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [code, setCode] = useState("");
  const [added, setAdded] = useState<Added | null>(null);
  const [name, setName] = useState("");
  const input = useRef<HTMLInputElement>(null);
  if (err && !s)
    return (
      <>
        <LoadError text={err} onRetry={reload} />
        {bottom}
      </>
    );
  if (!s)
    return (
      <>
        <Skeleton rows={3} label="Devices" />
        {bottom}
      </>
    );

  const act = async (action: Parameters<DevicesApi["authAct"]>[0], body: Record<string, unknown>): Promise<Record<string, unknown> | null> => {
    setBusy(true);
    setMsg(null);
    const r = await api.authAct(action, body);
    setBusy(false);
    if (!r.ok) {
      setMsg(String(r.error ?? "That did not work"));
      return null;
    }
    env.saved();
    reload();
    return r as Record<string, unknown>;
  };
  const { signedIn, waiting } = splitDevices(s);
  const browser = s.modes.find((m) => m.way === "browser");
  const notice = autoNotice(s.audit, now);
  const ro = env.readOnly || busy;
  const you = s.you;

  const approve = async () => {
    if (await act("approve", { code })) setCode("");
  };
  const add = async () => {
    const r = await act("add", { name: name.trim() || "a new device" });
    if (r) setAdded({ url: String(r.url), svg: r.svg as string | undefined, name: name.trim() || "a new device" });
  };

  const turnOn = async (on: boolean) => {
    // Turning it on while requests without a session still arrive would refuse them: that asks first (settings-asks.ts).
    const q = on && browser ? requireSignInAsk(browser) : null;
    if (q && !(await ask(q))) return;
    void act("mode", { way: "browser", mode: on ? "enforce" : "observe" });
  };

  return (
    <>
      {notice && (
        <div class="note" role="status" data-testid="auth-notice">
          {notice}
        </div>
      )}
      {msg && (
        <div class="seterr" role="alert">
          {msg}
        </div>
      )}
      {/* What waits for the owner is first on the page. Each waiting device is counted (settings-nav.ts
          settingsCounts), so its row carries the amber chip. */}
      {waiting.length > 0 && (
        <Group label="Waiting" title={`Waiting for you (${devicesNeed(s)})`} foot="Allow a device only if it is yours: the code on its screen is the same as here.">
          {waiting.map((d) => (
            <Row
              key={d.id}
              class="dev wait"
              acts
              title={d.name}
              desc={<span class="mono">{codeShow(d.code)}</span>}
              end={
                <>
                  <span class="pst needs-login">Waiting</span>
                  <button type="button" class="btn" disabled={ro || !you} onClick={() => void act("approve", { id: d.id })}>
                    Allow
                  </button>
                  <button type="button" class="btn" disabled={ro || !you} onClick={() => void act("deny", { id: d.id })}>
                    Deny
                  </button>
                </>
              }
            />
          ))}
        </Group>
      )}
      <Group label="Devices" title="Signed in" foot="A device is signed in on this host only. Sign out ends its session here and closes what it has open; the devices it added go with it.">
        {signedIn.length === 0 && <Empty text="No device is signed in." />}
        {signedIn.map((d) => (
          <DeviceItem key={d.id} d={d} now={now} you={you === d.id} disabled={ro || !you} onSignOut={() => void act("revoke", { id: d.id })} />
        ))}
      </Group>
      <Group label="Add a device" title="Add a device" foot="Open the link or scan the code on the other device. It works once, for 10 minutes. A device you add is signed out when this one is.">
        <Row
          stack
          title="Name"
          end={
            <span class="inl">
              <input ref={input} type="text" aria-label="Name of the new device" placeholder="Kitchen tablet" value={name} disabled={ro || !you} onInput={(e) => setName((e.target as HTMLInputElement).value)} />
            </span>
          }
        />
        {/* V0.settings-ia: the group is "Add a device"; the row names what the press makes, and the button the press. */}
        <Row
          title="Sign-in link"
          desc="For the device named above."
          end={
            <button type="button" class="btn" disabled={ro || !you} onClick={() => void add()}>
              Create link
            </button>
          }
        />
        {added && (
          <Row col class="addout" title={`Link for ${added.name}`}>
            {added.svg && <img class="qrimg" alt="QR code of the sign-in link" width="192" height="192" src={`data:image/svg+xml;utf8,${encodeURIComponent(added.svg)}`} />}
            <span class="inl linkout">
              <input class="mono" readOnly aria-label="Sign-in link" value={added.url} onFocus={(e) => (e.target as HTMLInputElement).select()} />
            </span>
          </Row>
        )}
      </Group>
      {/* One word for the act: a waiting device is allowed by its row's Allow, or by typing its code here. */}
      <Group label="Allow by code" title="Allow by code" foot="A device that is not signed in shows a code. Type it here, or run fyr device approve on the host.">
        <Row
          stack
          title="Code"
          end={
            <span class="codein">
              <span class="inl">
                <input class="mono" type="text" aria-label="Code of the device to allow" autocomplete="off" autocapitalize="characters" spellcheck={false} placeholder="ABCD-1234" value={code} disabled={ro || !you} onInput={(e) => setCode(typedCode((e.target as HTMLInputElement).value))} />
              </span>
              <button type="button" class="btn" disabled={ro || !you || code.length !== 8} onClick={() => void approve()}>
                Allow
              </button>
            </span>
          }
        />
      </Group>
      {browser && (
        <Group label="Require sign-in" title="Require sign-in" foot={modeLine(browser, now)}>
          <Row label title="A device must sign in" desc={browser.mode === "enforce" ? "On" : "Off: watching"} end={<Switch on={browser.mode === "enforce"} label="Require sign-in" disabled={ro || !you} onChange={(on) => void turnOn(on)} />} />
        </Group>
      )}
      {bottom}
      {!you && <div class="note">Sign in to change anything here: run <span class="mono">fyr open</span> on the host.</div>}
    </>
  );
}

/** A signed-in device. Signing one out cannot be taken back from its row (it must sign in again), so the button asks
 *  in place (settings-rows.tsx ConfirmButton). One word on every row, this device's and another's: "Sign out…".
 *  "this device" is a small pill beside the name. */
function DeviceItem({ d, now, you, disabled, onSignOut }: { d: DeviceRow; now: number; you: boolean; disabled: boolean; onSignOut(): void }) {
  return (
    <Row
      class="dev"
      title={
        <>
          {d.name}
          {you && <span class="pst thisdev">this device</span>}
        </>
      }
      desc={deviceLine(d, now)}
      end={<ConfirmButton label="Sign out" disabled={disabled} onConfirm={onSignOut} />}
    />
  );
}
