// SPEC 0.5 V0.mock-contract (#457): the mock (src/mock.ts) is a second fyrd for the layout test and sets each
// command's attention by hand. The Go test TestCommandEffectsFixture runs the owner's commands on a real engine and
// writes test/fixtures/command-effects.json: per command, the thread as the host sent it, its attention before, what
// the command's own upsert showed and, for a command that starts work (an answer, a Run), every attention its upserts
// showed until that work ended. This test seeds the mock with that same thread and asserts the mock sends the same.
// TestProviderLoginFixture writes test/fixtures/provider-login.json, which the last test holds the mock's
// provider.login to.
import { afterEach, beforeEach, mock, test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createMockServer } from "../src/mock.ts";
import { createMockProviders } from "../src/mock-providers.ts";
import type { Project, ThreadDTO } from "../src/types.ts";

type Att = { state: string; section: string; group?: string; word?: string };
type Effect = { name: string; action: string; args?: Record<string, unknown>; thread: ThreadDTO; projects: Project[]; before: Att; after: Att; path?: Att[] };

const fixture = (name: string) => JSON.parse(readFileSync(new URL(`./fixtures/${name}`, import.meta.url), "utf8"));
const effects: Effect[] = fixture("command-effects.json").effects;

/** What a row shows of the attention: the fields the fixture records. */
const att = (a: Att): Att => ({ state: a.state, section: a.section, ...(a.group ? { group: a.group } : {}), ...(a.word ? { word: a.word } : {}) });

beforeEach(() => mock.timers.enable({ apis: ["setTimeout", "Date"], now: Date.parse("2026-01-01T00:00:00Z") }));
afterEach(() => mock.timers.reset());

test("the fixture covers the owner's commands", () => {
  const actions = new Set(effects.map((e) => e.action));
  for (const a of ["thread.settle", "thread.unsettle", "thread.snooze", "thread.unsnooze", "thread.archive", "thread.unarchive", "thread.markSeen", "thread.markUnseen", "thread.pin", "thread.setProject", "project.archive", "thread.answer", "action.run"])
    assert.ok(actions.has(a), a);
});

for (const e of effects) {
  test(`${e.name}: the mock sends the host's attention`, () => {
    const srv = createMockServer("1", Date.now());
    srv.seed([structuredClone(e.thread)], structuredClone(e.projects));
    const now = () => att(srv.state().threads.find((t) => t.id === e.thread.id)!.attention);
    assert.deepEqual(now(), e.before, "before");
    // What the mock pushes for the thread, a repeat left out, as the Go test reads the host's stream.
    const sent: Att[] = [];
    srv.subscribe({
      thread: (t) => {
        const a = att(t.attention);
        if (t.id === e.thread.id && JSON.stringify(sent.at(-1)) !== JSON.stringify(a)) sent.push(a);
      },
      event: () => {},
    });
    const r = srv.command(`c-${e.name}`, e.action, e.action.startsWith("project.") ? undefined : e.thread.id, structuredClone(e.args ?? {}));
    assert.ok(r.ok, `${e.action}: ${r.error}`);
    for (let i = 0; i < 60; i++) mock.timers.tick(1000); // the mock's pushes, its agent's turn and its script run on timers
    assert.deepEqual(sent[0], e.after, "the command's own upsert");
    if (e.path) assert.deepEqual(sent, e.path, "every attention until the work it started ended");
    else assert.deepEqual(now(), e.after, "a mark stays");
  });
}
test("provider.login: the host's states in order, the link only once it waits", () => {
  const host = fixture("provider-login.json") as { before: unknown; after: { kind: string; state: string; url: boolean }; link: { kind: string; state: string; url: boolean } };
  const p = createMockProviders("", Date.now);
  const login = () => {
    const l = p.list().find((x) => x.id === "claude")!.login;
    return l ? { kind: l.kind, state: l.state, url: !!l.url } : null;
  };
  assert.deepEqual(login(), host.before, "before");
  assert.ok(p.command("provider.login", { id: "claude" }).ok);
  assert.deepEqual(login(), host.after, "right after the reply");
  mock.timers.tick(5_000);
  assert.deepEqual(login(), host.link, "once the CLI printed its link");
});
