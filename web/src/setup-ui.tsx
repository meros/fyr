// Setup Wizard UI (decision 0037, #434): both the "Set up · X of 6" inbox card
// and Settings → Setup, shared between desktop and phone.

import { useEffect, useState } from "preact/hooks";
import type { Api } from "./api.ts";
import { useApi } from "./api-context.ts";
import type { HostId } from "./hostid.ts";
import type { HostInfo } from "./hosts.ts";
import type { PageRef } from "./settings-nav.ts";
import { Empty, Group } from "./settings-rows.tsx";
import { setupCardLabel, setupNeedsCard, stepPill, type GHAuthState, type SetupRepo, type SetupReport, type SetupStep } from "./setup.ts";

/** Reads the first online addressable host's setup report (for the inbox card). */
export function SetupInboxCard({ hosts, projectCount, phone, onOpen }: { hosts: HostInfo[]; projectCount: number; phone?: boolean; onOpen(): void }) {
  const api = useApi();
  const [report, setReport] = useState<SetupReport | null>(null);
  const hostId = hosts.find((h) => h.online && h.id)?.id;

  useEffect(() => {
    if (!hostId || !api.setup) return;
    let alive = true;
    void api
      .setup(hostId)
      .then((r) => {
        if (alive) setReport(r.data);
      })
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, [api, hostId, projectCount]);

  if (!setupNeedsCard(report)) return null;
  const next = report!.steps.find((s) => !s.optional && s.status !== "ok");
  return (
    <button type="button" class={`setup-card${phone ? " phone" : ""}`} onClick={onOpen}>
      <span class="setup-card-h">
        <strong>{setupCardLabel(report!)}</strong>
        <span class="setup-card-go">Continue →</span>
      </span>
      {next && <span class="setup-card-sub">Next: {next.title} — {next.reason}</span>}
    </button>
  );
}

interface GHRepoItem {
  nameWithOwner: string;
  description?: string;
}

export function SetupPage({
  api,
  host,
  hostName,
  go,
  onOpenThread,
}: {
  api: Pick<Api, "command"> & Partial<Pick<Api, "setup" | "authAct">>;
  host: HostId | undefined;
  /** V0.host-tabs: when several hosts are shown, the active tab's host name so Open Providers lands on it */
  hostName?: string;
  go(r: PageRef): void;
  onOpenThread?(id: string): void;
}) {
  const [report, setReport] = useState<SetupReport | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [ghAuth, setGhAuth] = useState<GHAuthState | null>(null);
  const [ghRepos, setGhRepos] = useState<GHRepoItem[] | null>(null);
  const [qr, setQr] = useState<{ url: string; svg: string } | null>(null);

  const load = async () => {
    if (!host || !api.setup) return;
    try {
      const r = await api.setup(host);
      setReport(r.data);
      setGhAuth(r.data.ghAuth ?? null);
      setErr(null);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  };

  useEffect(() => {
    setReport(null);
    setErr(null);
    setNote(null);
    setGhAuth(null);
    setGhRepos(null);
    setQr(null);
    void load();
  }, [host]);

  // Poll while GitHub device-code login is running.
  useEffect(() => {
    if (!ghAuth?.running) return;
    const t = setInterval(() => void load(), 2500);
    return () => clearInterval(t);
  }, [ghAuth?.running, host]);

  const runFix = async (action: string, extra?: Record<string, unknown>) => {
    if (!host) return;
    setBusy(action);
    setNote(null);
    try {
      const r = await api.command(host, "host.setupFix", undefined, { action, ...(extra ?? {}) });
      if (!r.ok) {
        setNote(r.error ?? "That step failed");
        return null;
      }
      if (action === "gh.login" && r.result && typeof r.result === "object") {
        setGhAuth(r.result as unknown as GHAuthState);
      } else if (action === "gh.loginCancel") {
        setGhAuth(null);
      } else if (action === "gh.repos" && r.result && typeof r.result === "object") {
        setGhRepos(((r.result as { repos?: GHRepoItem[] }).repos ?? []));
      }
      await load();
      return r.result;
    } finally {
      setBusy(null);
    }
  };

  const startFirstThread = async (cwd: string) => {
    if (!host) return;
    setBusy("thread:" + cwd);
    setNote(null);
    try {
      const r = await api.command(host, "thread.create", undefined, {
        cwd,
        title: "Explore this repository",
        text: "Give me a concise overview of this repository: what it does, how to build and test it, and any notable recent changes.",
      });
      if (!r.ok) {
        setNote(r.error ?? "Could not start a thread");
        return;
      }
      await load();
      if (r.threadId && onOpenThread) onOpenThread(r.threadId);
    } finally {
      setBusy(null);
    }
  };

  const cloneAndStart = async (repo: string) => {
    const res = await runFix("project.clone", { repo });
    const path = (res as { path?: string } | null)?.path;
    if (path) await startFirstThread(path);
  };

  const showPhoneQR = async () => {
    if (!api.authAct) return;
    setBusy("qr");
    setNote(null);
    try {
      const r = await api.authAct("add", {});
      if (!r.ok) {
        setNote(r.error ?? "Could not create a pairing link");
        return;
      }
      setQr({ url: String(r.url ?? ""), svg: String(r.svg ?? "") });
    } finally {
      setBusy(null);
    }
  };

  if (!host || !api.setup) {
    return (
      <Group>
        <Empty text="Setup checks are not available on this host." />
      </Group>
    );
  }
  if (err && !report) {
    return (
      <Group>
        <div class="setup-err">
          <span>{err}</span>
          <button type="button" class="btn sm" onClick={() => void load()}>
            Retry
          </button>
        </div>
      </Group>
    );
  }
  if (!report) {
    return (
      <Group>
        <Empty text="Checking this host…" />
      </Group>
    );
  }

  return (
    <div class="setup-wiz">
      <div class="setup-head">
        <span class="setup-prog">
          {report.done} of {report.total} ready
        </span>
        <button type="button" class="btn sm" disabled={!!busy} onClick={() => void load()}>
          Re-check
        </button>
      </div>
      {note && <div class="note">{note}</div>}
      {report.steps.map((s) => (
        <StepCard
          key={s.id}
          step={s}
          busy={busy}
          ghAuth={s.id === "github" ? ghAuth : null}
          ghRepos={s.id === "project" ? ghRepos : null}
          qr={s.id === "phone" ? qr : null}
          canQR={!!api.authAct}
          onFix={runFix}
          onGo={(r) => go(r.page === "providers" && hostName ? { ...r, host: hostName } : r)}
          onStartThread={startFirstThread}
          onCloneAndStart={cloneAndStart}
          onShowQR={showPhoneQR}
        />
      ))}
    </div>
  );
}

function StepCard({
  step,
  busy,
  ghAuth,
  ghRepos,
  qr,
  canQR,
  onFix,
  onGo,
  onStartThread,
  onCloneAndStart,
  onShowQR,
}: {
  step: SetupStep;
  busy: string | null;
  ghAuth: GHAuthState | null;
  ghRepos: GHRepoItem[] | null;
  qr: { url: string; svg: string } | null;
  canQR: boolean;
  onFix(action: string, extra?: Record<string, unknown>): Promise<unknown>;
  onGo(r: PageRef): void;
  onStartThread(cwd: string): Promise<void>;
  onCloneAndStart(repo: string): Promise<void>;
  onShowQR(): Promise<void>;
}) {
  const pill = stepPill(step);
  const detail = step.detail ?? {};
  const repos = (Array.isArray(detail.repos) ? detail.repos : []) as SetupRepo[];
  const canClone = detail.canClone === true;
  const needsLoginId = typeof detail.needsLoginId === "string" ? detail.needsLoginId : "";

  return (
    <div class={`setup-step ${pill.cls}`}>
      <div class="setup-step-h">
        <strong>{step.title}</strong>
        <span class={`setup-pill ${pill.cls}`}>{pill.text}</span>
      </div>
      <p class="setup-why">{step.reason}</p>
      <div class="setup-acts">
        {(step.fixes ?? []).map((f) => (
          <button key={f.id} type="button" class="btn sm pri" disabled={!!busy} onClick={() => void onFix(f.id)}>
            {busy === f.id ? "Working…" : f.label}
          </button>
        ))}
        {step.id === "claude" && step.status !== "ok" && (
          <button
            type="button"
            class="btn sm"
            onClick={() => onGo(needsLoginId ? { page: "providers", sub: needsLoginId } : { page: "providers" })}
          >
            Open Providers
          </button>
        )}
        {step.id === "project" && step.status !== "ok" && canClone && !ghRepos && (
          <button type="button" class="btn sm" disabled={!!busy} onClick={() => void onFix("gh.repos")}>
            {busy === "gh.repos" ? "Loading…" : "Clone from GitHub"}
          </button>
        )}
        {step.id === "phone" && step.status === "ok" && canQR && (
          <button type="button" class="btn sm" disabled={!!busy} onClick={() => void onShowQR()}>
            {busy === "qr" ? "Creating…" : "Show phone QR"}
          </button>
        )}
        {step.id === "peers" && (
          <button type="button" class="btn sm" onClick={() => onGo({ page: "hosts" })}>
            Open Hosts &amp; Pairing
          </button>
        )}
      </div>

      {ghAuth && ghAuth.running && (
        <div class="setup-box">
          <div>
            Enter this one-time code on GitHub:{" "}
            <strong class="mono">{ghAuth.code || "starting…"}</strong>
          </div>
          <div class="setup-acts">
            {ghAuth.url && (
              <a class="btn sm pri" href={ghAuth.url} target="_blank" rel="noreferrer">
                Open github.com/login/device
              </a>
            )}
            <button type="button" class="btn sm" onClick={() => void onFix("gh.loginCancel")}>
              Cancel
            </button>
          </div>
        </div>
      )}
      {ghAuth && !ghAuth.running && ghAuth.error && <p class="setup-why">{ghAuth.error}</p>}

      {step.id === "project" && step.status !== "ok" && repos.length > 0 && (
        <div class="setup-list">
          {repos.slice(0, 8).map((r) => (
            <div key={r.path} class="setup-repo">
              <div class="setup-repo-info">
                <strong>{r.name}</strong>
                <span class="mono">{r.path}</span>
              </div>
              <button type="button" class="btn sm pri" disabled={!!busy} onClick={() => void onStartThread(r.path)}>
                {busy === "thread:" + r.path ? "Starting…" : "Start first thread"}
              </button>
            </div>
          ))}
        </div>
      )}

      {ghRepos && ghRepos.length > 0 && (
        <div class="setup-list">
          {ghRepos.slice(0, 8).map((r) => (
            <div key={r.nameWithOwner} class="setup-repo">
              <div class="setup-repo-info">
                <strong>{r.nameWithOwner}</strong>
                {r.description && <span>{r.description}</span>}
              </div>
              <button type="button" class="btn sm" disabled={!!busy} onClick={() => void onCloneAndStart(r.nameWithOwner)}>
                {busy === "project.clone" ? "Cloning…" : "Clone & start"}
              </button>
            </div>
          ))}
        </div>
      )}

      {qr && (
        <div class="setup-box setup-qr">
          {qr.svg && <div class="setup-qr-svg" dangerouslySetInnerHTML={{ __html: qr.svg }} />}
          <span class="mono setup-qr-url">{qr.url}</span>
        </div>
      )}
    </div>
  );
}
