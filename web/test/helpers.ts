import { decodeThread } from "../src/decode.ts";
import type { HostId } from "../src/hostid.ts";
import type { Snapshot } from "../src/hostfeed.ts";
import type { HostInfo } from "../src/hosts.ts";
import { viewOf, type Store, type View } from "../src/store.ts";
import type { AttentionState, NeedsCode, Profile, Project, Section, StateDTO, Thread, ThreadDTO } from "../src/types.ts";

/** A test's install id: a string is not a HostId (src/hostid.ts), so a test names its hosts through this. */
export const hid = (s: string) => s as HostId;

/** The one host of a test that does not care which: its threads and projects are stamped with it. */
export const TH = hid("TEST-H1");

export function thread(id: string, state: AttentionState, section: Section, extra: Partial<ThreadDTO> = {}): Thread {
  // #41: the client works on decoded threads; the fixture goes through the same decoder, into its host's state.
  return decodeThread(
    {
      id,
      title: id,
      cwd: "/x",
      project: "p",
      profile: "claude",
      model: "m",
      createdAt: "2026-11-14T08:00:00Z",
      updatedAt: "2026-11-14T08:00:00Z",
      running: false,
      attention: { state, section, reason: "" },
      ...extra,
    },
    extra.hostId ?? TH,
  );
}

/** A thread in Your move with the group code its host sends (V0.row-group-code; every host sends one, V0.old-routes-end). */
export function needsThread(id: string, state: AttentionState, group: NeedsCode, extra: Partial<ThreadDTO> = {}): Thread {
  return thread(id, state, "needs", { ...extra, attention: { state, section: "needs", reason: "", group, ...extra.attention } });
}

/** A snapshot (slice 2b: one state per host) from records stamped with their host (`hostId`, TH when they name none).
 *  `hosts` is the front's list; without one it is TH alone, named "h1". `per` adds to a host's own state. */
export function snapshot(
  o: { now?: string; threads?: ThreadDTO[]; projects?: Project[]; profiles?: Profile[]; hosts?: HostInfo[]; front?: { id: string; name: string }; projectOrder?: string[]; per?: Record<string, Partial<StateDTO>> } = {},
): Snapshot {
  const now = o.now ?? "2026-11-14T09:00:00Z";
  const hosts = o.hosts ?? [{ name: "h1", online: true, id: TH }];
  const states: Record<string, StateDTO> = {};
  for (const h of hosts) {
    if (!h.id) continue;
    const mine = <T extends { hostId?: HostId }>(xs: T[] | undefined) => (xs ?? []).filter((x) => (x.hostId ?? TH) === h.id).map((x) => ({ ...x, hostId: h.id }));
    states[h.id] = { now, user: "owner", threads: mine(o.threads), projects: mine(o.projects), profiles: o.profiles ?? [], actions: [], ...o.per?.[h.id] };
  }
  return { now, user: "owner", front: o.front ?? { id: hosts[0]?.id ?? "", name: hosts[0]?.name ?? "" }, hosts, ...(o.projectOrder ? { projectOrder: o.projectOrder } : {}), states };
}

/** The store as the screens read it. */
export const view = (s: Store): View => viewOf(s);
