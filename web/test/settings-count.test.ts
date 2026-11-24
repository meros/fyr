// V0.settings-ia: the count at the door (the desktop's gear, the phone's More row) is the count inside (the nav's
// badges). The door reads for itself (readDoor); Settings' pages hold what they read of every host (settings-load.ts)
// and count it with countsFrom. Both are run here over the mock's combinations, as data: one host and several; a
// release held back, a provider without its key, a providers read that fails, no providers, and no connection.
import { test } from "node:test";
import assert from "node:assert/strict";
import { countsFrom, readDoor, type CountApi, type HostRead } from "../src/settings-count.ts";
import { devicesNeed, type AuthState } from "../src/devices.ts";
import { settingsHosts } from "../src/host-settings.ts";
import type { HostInfo } from "../src/hosts.ts";
import type { SetupReport } from "../src/setup.ts";

type Mode = "def" | "held" | "nokey" | "fail" | "none" | "offline";
const MODES: Mode[] = ["def", "held", "nokey", "fail", "none", "offline"];

const bad = { commit: "d41c9a7e2b3f", why: "was not healthy" };
const DESK = "F7QK-2M9X-AB34-CD56";
const HOST_B = "M3RS-QX7P-5LKA-2WNE";
const LAB = "B7LA-4QZE-9KDF-3HXW";

/** The front's host list, as the mock sends it. One host: a row with an id and no update summary. Several: desk (its
 *  row carries a summary), host-b (offline, holds a release back), lab (a changed id, no summary), old-nuc (no id). */
function hostList(several: boolean, mode: Mode): HostInfo[] {
  const rows = several
    ? [
        { name: "desk", online: true, id: DESK, update: mode === "held" ? { behind: 2, bad } : { behind: 0 } },
        { name: "host-b", online: false, id: HOST_B, update: { behind: 3, bad } },
        { name: "lab", online: true, id: LAB, idWas: "AX2C-7RTM-8PLQ-6VYD" },
        { name: "old-nuc", online: true },
      ]
    : [{ name: "desk", online: true, id: DESK }];
  return rows as unknown as HostInfo[];
}

/** A host that answers as the mock's do, and a log of what was asked of it. */
function fakeApi(mode: Mode) {
  const asked: string[] = [];
  const answer = <T,>(what: string, id: string, data: T) => {
    asked.push(`${what} ${id}`);
    return Promise.resolve(mode === "offline" || (mode === "fail" && what === "providers") ? { ok: false as const, error: "no answer" } : { ok: true as const, data });
  };
  const providers = (id: string) => {
    if (mode === "none") return [];
    if (id === LAB) return [{ status: "ready" }];
    if (id === HOST_B) return [{ status: "needs-login" }, { status: "needs-key" }]; // what the front last read of it
    return [{ status: "ready" }, { status: "needs-login" }, { status: mode === "nokey" ? "needs-key" : "ready" }];
  };
  const auth = { devices: [{ id: "d1", name: "tablet", pending: true, code: "K7QM" }, { id: "d2", name: "phone" }], modes: [], audit: [] } as unknown as AuthState;
  const api = {
    providers: (id: string) => answer("providers", id, providers(id)),
    systemAgents: (id: string) => answer("systemAgents", id, { default: {}, providers: [], jobs: id === LAB ? [{ id: "title", name: "Thread titles", choice: {}, error: "no Haiku model" }] : [{ id: "title", name: "Thread titles", choice: {} }] }),
    updates: (id: string) => answer("updates", id, id === DESK && mode === "held" ? { live: "c705ef5", updates: [], bad } : { live: "c705ef5", updates: [] }),
    authState: () => answer("authState", "", auth),
  } as unknown as CountApi;
  return { api, asked, auth };
}

/** The nav's count: Settings reads every host that has an id (an offline one answers with what the front last read)
 *  and counts what its pages hold. */
async function navCounts(hosts: HostInfo[], mode: Mode, setup: Record<string, SetupReport>) {
  const { api, auth } = fakeApi(mode);
  const reads = await Promise.all(
    settingsHosts(hosts).map(async (host): Promise<HostRead> => {
      if (!host.id) return { host, providers: null, blocked: 0, view: null };
      const [l, sa, up] = await Promise.all([api.providers(host.id), api.systemAgents!(host.id), api.updates!(host.id)]);
      return { host, providers: l.ok ? l.data : null, blocked: sa.ok ? sa.data.jobs.filter((j) => j.error).length : 0, view: up.ok ? up.data : null };
    }),
  );
  const a = await api.authState!();
  return countsFrom(hosts, reads, setup, a.ok ? devicesNeed(auth) : 0, mode === "offline");
}

test("V0.settings-ia: the door's count is the nav's, before Settings was ever opened", async () => {
  // door total per combination, so a rule that zeroed both sides would not pass
  const want: Record<string, number> = {
    "one def": 2, "one held": 3, "one nokey": 3, "one fail": 1, "one none": 1, "one offline": 0,
    "several def": 4, "several held": 5, "several nokey": 5, "several fail": 3, "several none": 3, "several offline": 0,
  };
  for (const several of [false, true])
    for (const mode of MODES) {
      const name = `${several ? "several" : "one"} ${mode}`;
      const hosts = hostList(several, mode);
      const door = await readDoor(fakeApi(mode).api, hosts, {}, mode === "offline");
      assert.deepEqual(door, await navCounts(hosts, mode, {}), name);
      assert.equal(door.total, want[name], `${name}: ${JSON.stringify(door.pages)}`);
      assert.equal(door.total, door.pages.providers + door.pages.hosts + door.pages.setup + door.pages.devices, name);
    }
});

test("V0.settings-ia: one host whose row carries no update summary: the door reads what it holds", async () => {
  const hosts = hostList(false, "held");
  const { api, asked } = fakeApi("held");
  const door = await readDoor(api, hosts, {}, false);
  assert.deepEqual(door.pages, { providers: 1, hosts: 1, setup: 0, devices: 1 }, "the held-back release counts at the door");
  assert.ok(asked.includes(`updates ${DESK}`));
});

test("V0.settings-ia: what the door does not read", async () => {
  const { api, asked } = fakeApi("held");
  await readDoor(api, hostList(true, "held"), {}, false);
  assert.deepEqual(asked.filter((a) => a.startsWith("updates")), [`updates ${LAB}`], "a row with an update summary is not asked again; lab's has none");
  assert.equal(asked.some((a) => a.endsWith(HOST_B)), false, "an offline host is not read");
  assert.equal(asked.some((a) => a.startsWith("setup")), false, "a host's setup checks are never run from the door");
  const off = fakeApi("offline");
  await readDoor(off.api, hostList(true, "def"), {}, true);
  assert.deepEqual(off.asked, [], "with no connection nothing is read");
});

test("V0.settings-ia: a required setup step counts at the door once Settings has read it, and equally inside", async () => {
  const half = { os: "", host: "", done: 4, total: 6, ready: false, steps: [{ id: "gh", title: "GitHub", status: "missing" as const, reason: "" }] } as SetupReport;
  const hosts = hostList(true, "def");
  const door = await readDoor(fakeApi("def").api, hosts, { [DESK]: half, [HOST_B]: half }, false);
  assert.equal(door.pages.setup, 1, "desk's step; host-b is offline");
  assert.deepEqual(door, await navCounts(hosts, "def", { [DESK]: half, [HOST_B]: half }));
});
