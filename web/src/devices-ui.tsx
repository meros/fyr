// SPEC V0.157, decision 0030 3.4: Settings → Devices, one page on the desktop and the phone. The devices that are
// signed in, with Revoke; the ones that wait, with Allow and Deny; Approve a code; Add a device (a link and a QR
// code, valid once for 10 minutes); and whether the browser way in is required. The width is the only difference.
import { useEffect, useRef, useState } from "preact/hooks";
import type { Api } from "./api.ts";
import { autoNotice, codeShow, deviceLine, devicesNeed, modeLine, splitDevices, typedCode, type AuthState, type DeviceRow } from "./devices.ts";
import { Empty, Group, LoadError, Row, Skeleton, Switch, useSet } from "./settings-rows.tsx";

type DevicesApi = Pick<Api, "authState" | "authAct">;

/** The state of Settings → Devices, read by the page and by the nav's line; it reloads every 5 s while shown. */
export function useAuthState(api: Partial<DevicesApi>, live: boolean): { s: AuthState | null; err: string | null; reload(): void } {
  const [s, setS] = useState<AuthState | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const load = () => {
    if (!api.authState) return;
    api.authState().then(
      (v) => (setS(v), setErr(null)),
      (e) => setErr(String((e as Error).message ?? e)),
    );
  };
  useEffect(() => {
    load();
    if (!live) return;
    const t = setInterval(load, 5000);
    return () => clearInterval(t);
  }, [live, !!api.authState]);
  return { s, err, reload: load };
}

interface Added {
  url: string;
  svg?: string;
  name: string;
}

export function DevicesPage({ api, now, st }: { api: DevicesApi; now: number; st: { s: AuthState | null; err: string | null; reload(): void } }) {
  const env = useSet();
  const { s, err, reload } = st;
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [code, setCode] = useState("");
  const [added, setAdded] = useState<Added | null>(null);
  const [confirm, setConfirm] = useState<string | null>(null);
  const [name, setName] = useState("");
  const input = useRef<HTMLInputElement>(null);
  if (err && !s) return <LoadError text={err} onRetry={reload} />;
  if (!s) return <Skeleton rows={3} label="Devices" />;

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
      {waiting.length > 0 && (
        <Group label="Waiting" title={`Waiting for you (${devicesNeed(s)})`} foot="Allow a device only if it is yours: the code on its screen is the same as here.">
          {waiting.map((d) => (
            <Row
              key={d.id}
              class="dev wait"
              title={d.name}
              desc={<span class="mono">{codeShow(d.code)}</span>}
              end={
                <>
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
      <Group label="Devices" title="Signed in" foot="A device is signed in on this install only. Revoke signs it out here, closes what it has open, and the devices it added go with it.">
        {signedIn.length === 0 && <Empty text="No device is signed in." />}
        {signedIn.map((d) => (
          <DeviceItem key={d.id} d={d} now={now} you={you === d.id} confirming={confirm === d.id} disabled={ro || !you} onAsk={() => setConfirm(d.id)} onCancel={() => setConfirm(null)} onRevoke={() => (setConfirm(null), void act("revoke", { id: d.id }))} />
        ))}
      </Group>
      <Group label="Add a device" title="Add a device" foot="Open the link or scan the code on the other device. It works once, for 10 minutes. A device you add is signed out when you revoke this one.">
        <Row
          stack
          title="Name"
          end={
            <span class="inl">
              <input ref={input} type="text" aria-label="Name of the new device" placeholder="Kitchen tablet" value={name} disabled={ro || !you} onInput={(e) => setName((e.target as HTMLInputElement).value)} />
            </span>
          }
        />
        <Row
          title="New sign-in link"
          end={
            <button type="button" class="btn" disabled={ro || !you} onClick={() => void add()}>
              Add a device
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
      <Group label="Approve" title="Approve a code" foot="A browser that is not signed in shows a code. Type it here, or run fyr device approve on the machine that runs Fyr.">
        <Row
          stack
          title="Code"
          end={
            <span class="codein">
              <span class="inl">
                <input class="mono" type="text" aria-label="Approval code" autocomplete="off" autocapitalize="characters" spellcheck={false} placeholder="ABCD-1234" value={code} disabled={ro || !you} onInput={(e) => setCode(typedCode((e.target as HTMLInputElement).value))} />
              </span>
              <button type="button" class="btn" disabled={ro || !you || code.length !== 8} onClick={() => void approve()}>
                Approve
              </button>
            </span>
          }
        />
      </Group>
      {browser && (
        <Group label="Sign-in" title="Sign-in" foot={modeLine(browser, now)}>
          <Row
            label
            title="Require sign-in"
            desc={browser.mode === "enforce" ? "On" : "Off: watching"}
            end={<Switch on={browser.mode === "enforce"} label="Require sign-in" disabled={ro || !you} onChange={(on) => void act("mode", { way: "browser", mode: on ? "enforce" : "observe" })} />}
          />
        </Group>
      )}
      {!you && <div class="note">Sign in to change anything here: run <span class="mono">fyr open</span> on the machine that runs Fyr.</div>}
    </>
  );
}

function DeviceItem({ d, now, you, confirming, disabled, onAsk, onCancel, onRevoke }: { d: DeviceRow; now: number; you: boolean; confirming: boolean; disabled: boolean; onAsk(): void; onCancel(): void; onRevoke(): void }) {
  return (
    <Row
      class="dev"
      title={
        <>
          {d.name}
          {you && <span class="pill you"> this device</span>}
        </>
      }
      desc={deviceLine(d, now)}
      end={
        confirming ? (
          <>
            <button type="button" class="btn danger" disabled={disabled} onClick={onRevoke}>
              Revoke
            </button>
            <button type="button" class="btn" onClick={onCancel}>
              Cancel
            </button>
          </>
        ) : (
          <button type="button" class="btn" disabled={disabled} onClick={onAsk}>
            {you ? "Sign out" : "Revoke…"}
          </button>
        )
      }
    />
  );
}
