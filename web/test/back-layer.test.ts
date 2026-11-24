// V0.dialogs-review (4): the phone's Back layers (back-layer.ts): a layer is one history entry, the history follows
// the stack of open layers whoever moved it, and a navigation waits until it does.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createLayers, layerDepth, TRAVERSE_MS } from "../src/back-layer.ts";
import { navFromHistory, seedHistory, type Nav } from "../src/nav.ts";

/** A history whose traversals land later, as a browser's do: go() queues, `land` delivers the popstate. Timers are
 *  the test's to run: `after(0)` is the turn after an event, `after(TRAVERSE_MS)` the give-up timers. */
function fake() {
  const entries: unknown[] = [{ screen: { name: "new" }, sheet: null }];
  let at = 0;
  const queued: number[] = [];
  const timers: { fn: () => void; ms: number }[] = [];
  const h = {
    get state() {
      return entries[at];
    },
    pushState(s: unknown) {
      entries.splice(at + 1);
      entries.push(s);
      at = entries.length - 1;
    },
    go(n: number) {
      queued.push(n);
    },
  };
  const ls = createLayers(h, (fn, ms) => void timers.push({ fn, ms }));
  const after = (ms: number) => {
    for (const t of timers.filter((x) => x.ms === ms)) {
      timers.splice(timers.indexOf(t), 1);
      t.fn();
    }
  };
  const move = (d: number) => {
    at = Math.min(entries.length - 1, Math.max(0, at + d));
    ls.popped(entries[at]);
  };
  return {
    h,
    ls,
    after,
    /** the oldest queued traversal lands; false when none was asked for */
    land(): boolean {
      const d = queued.shift();
      if (d === undefined) return false;
      move(d);
      return true;
    },
    /** every queued traversal lands, each followed by the turn after its event; returns how many did */
    async run(): Promise<number> {
      let n = 0;
      for (;;) {
        await tick();
        const d = queued.shift();
        if (d === undefined) return n;
        n++;
        move(d);
        after(0);
      }
    },
    /** the owner's Back and Forward, with the turn after the event */
    back: () => (move(-1), after(0)),
    forward: () => (move(1), after(0)),
    /** the oldest pending timer of `ms` fires, alone */
    oldest(ms: number) {
      const t = timers.find((x) => x.ms === ms);
      if (t) (timers.splice(timers.indexOf(t), 1), t.fn());
    },
    at: () => at,
    asked: () => queued.length,
  };
}
const tick = () => new Promise<void>((r) => setTimeout(r, 0));

test("V0.dialogs-review (4): Back closes the top-most layer, and the screen under it stays", () => {
  const f = fake();
  const closed: string[] = [];
  f.ls.open(() => closed.push("list"));
  f.ls.open(() => closed.push("folder picker"));
  assert.equal(f.at(), 2, "one entry per layer");
  assert.deepEqual((f.h.state as { screen: unknown }).screen, { name: "new" }, "a layer's entry is the screen's own state");
  assert.equal(layerDepth(f.h.state), 2, "and says how many layers lie over the screen there");
  f.back();
  assert.deepEqual(closed, ["folder picker"]);
  f.back();
  assert.deepEqual(closed, ["folder picker", "list"]);
  assert.equal(f.at(), 0, "still on the screen");
  assert.equal(f.ls.depth(), 0);
  f.back(); // nothing left: the navigation's own Back
  assert.equal(closed.length, 2);
  assert.equal(f.asked(), 0, "and the stack asked for no traversal of its own");
});

test("V0.dialogs-review (4): a layer closed on the page takes its entry out, and that traversal is not a Back", async () => {
  const f = fake();
  const closed: string[] = [];
  const list = f.ls.open(() => closed.push("list"));
  const step = f.ls.open(() => closed.push("step"));
  // A project was made: the step and the list close in one turn, in either order.
  f.ls.close(list);
  f.ls.close(step);
  assert.equal(await f.run(), 1, "one traversal for both entries");
  assert.equal(f.at(), 0);
  assert.deepEqual(closed, [], "no layer was told to close again");
});

test("V0.dialogs-review (4): a navigation waits for a layer's entry on its way out, and never lands on top of a layer", async () => {
  const f = fake();
  const order: string[] = [];
  const confirm = f.ls.open(() => order.push("cancelled"));
  f.ls.close(confirm); // the owner pressed the confirmation's button
  f.ls.settled(() => order.push(`navigate at ${f.at()}`)); // what it confirmed navigates at once
  assert.equal(order.length, 0, "not before the entry is out");
  await f.run();
  assert.deepEqual(order, ["navigate at 0"]);

  // Start with the project list open: the list closes and its entry goes first, then the thread replaces the
  // screen's entry. The layer is told: it must not stay on the screen without an entry (review round 2, 10).
  f.ls.open(() => order.push("list closed"));
  assert.equal(f.at(), 1);
  f.ls.settled(() => order.push(`replace at ${f.at()}`));
  assert.deepEqual(order, ["navigate at 0", "list closed"], "closed at once, the navigation not yet");
  await f.run();
  assert.deepEqual(order, ["navigate at 0", "list closed", "replace at 0"]);
  assert.equal(f.ls.depth(), 0);
  // With nothing open a navigation runs at once.
  f.ls.settled(() => order.push("now"));
  assert.equal(order.at(-1), "now");
});

test("V0.dialogs-review (4): a waiting navigation runs after the popstate event, not inside it", async () => {
  const f = fake();
  const order: string[] = [];
  const l = f.ls.open(() => {});
  f.ls.close(l);
  f.ls.settled(() => order.push("navigate"));
  await tick();
  // The traversal lands. Whatever other popstate listener runs for the same event (the navigation's own, which sets
  // the screen of the entry) runs before the waiting navigation, in either listener order.
  assert.equal(f.land(), true);
  order.push("the other popstate listeners");
  f.after(0);
  assert.deepEqual(order, ["the other popstate listeners", "navigate"]);
});

test("V0.dialogs-review (4): a layer that opens while another's entry leaves waits for it; the header's × steps over all", async () => {
  const f = fake();
  const closed: string[] = [];
  const list = f.ls.open(() => closed.push("list"));
  f.ls.close(list);
  f.ls.open(() => closed.push("step")); // the list became the New project step in the same turn
  assert.equal(f.at(), 1, "no entry pushed under a traversal that is on its way");
  await f.run();
  assert.equal(f.at(), 1, "the list's entry left, the step's came");
  assert.equal(layerDepth(f.h.state), 1);
  assert.equal(f.ls.depth(), 1);
  assert.deepEqual(closed, []);
  // The header's ×: one traversal over the step's entry and the screen's.
  let ran = "";
  f.h.pushState({ screen: { name: "new" }, sheet: null, backLayer: 1 }); // (the fake's entry 0 stands for the inbox here)
  f.ls.leave();
  f.ls.settled(() => (ran = `at ${f.at()}`));
  assert.equal(f.ls.depth(), 0);
  assert.deepEqual(closed, ["step"], "the layer is told, so it never stays up without an entry");
  assert.equal(ran, "", "a navigation waits for the leave too");
  assert.equal(await f.run(), 1);
  assert.equal(f.at(), 0);
  assert.equal(ran, "at 0");
});

test("V0.dialogs-review (4): a traversal that never lands does not hold navigation for ever", async () => {
  const f = fake();
  const l = f.ls.open(() => {});
  f.ls.close(l);
  let ran = false;
  f.ls.settled(() => (ran = true));
  await tick();
  assert.equal(ran, false);
  f.after(TRAVERSE_MS); // the timeout
  assert.equal(ran, true);
});

test("V0.dialogs-review (4): the give-up timer of one traversal does not end the next one", async () => {
  const f = fake();
  const closed: string[] = [];
  f.ls.open(() => closed.push("list"));
  const step = f.ls.open(() => closed.push("step"));
  const picker = f.ls.open(() => closed.push("folder picker"));
  f.ls.close(picker); // traversal 1
  await tick();
  assert.equal(f.land(), true);
  f.after(0);
  f.ls.close(step); // traversal 2, just before traversal 1's timer is due
  await tick();
  let ran = false;
  f.ls.settled(() => (ran = true)); // closes the list too; waits for traversal 2
  assert.deepEqual(closed, ["list"]);
  f.oldest(TRAVERSE_MS); // traversal 1's timer, while traversal 2 is on its way
  assert.equal(ran, false, "traversal 2 is still on its way: nothing runs under it");
  assert.equal(f.land(), true); // traversal 2 lands: not the owner's Back
  f.after(0);
  assert.deepEqual(closed, ["list"], "no layer was closed as if the owner went Back");
  await f.run(); // the list's entry
  assert.equal(ran, true);
  assert.equal(f.at(), 0);
});

test("V0.dialogs-review (4): the history follows the stack whoever moved it: Forward onto a gone layer's entry steps back", async () => {
  const f = fake();
  const closed: string[] = [];
  f.ls.open(() => closed.push("list"));
  f.back(); // the owner's Back closed the list; its entry is still ahead
  assert.deepEqual(closed, ["list"]);
  f.forward(); // onto the entry of a layer that is gone
  assert.equal(f.at(), 1);
  assert.equal(await f.run(), 1, "the stack steps back off it");
  assert.equal(f.at(), 0);
  assert.deepEqual(closed, ["list"], "and closes nothing");
});

test("V0.dialogs-review (4): a traversal that is not about a layer closes none", () => {
  const f = fake();
  const closed: string[] = [];
  f.ls.open(() => closed.push("picker"));
  // Something else has an entry over the layer's (the full-screen view copies the state it is pushed on) and leaves.
  f.h.pushState({ ...(f.h.state as object), layer: true });
  assert.equal(layerDepth(f.h.state), 1);
  f.back();
  assert.deepEqual(closed, [], "the layer stays: the entry landed on is its own");
  assert.equal(f.ls.depth(), 1);
  f.back();
  assert.deepEqual(closed, ["picker"]);
});

test("V0.dialogs-review (4): two layers closed one after the other, the second while the first entry is on its way", async () => {
  const f = fake();
  const a = f.ls.open(() => {});
  const b = f.ls.open(() => {});
  f.ls.close(b);
  await tick();
  f.ls.close(a); // traversal 1 has not landed
  assert.equal(f.asked(), 1);
  assert.equal(await f.run(), 2, "one after the other, never two at once");
  assert.equal(f.at(), 0);
});

test("V0.dialogs-review (4): a page loaded on a layer's entry keeps the entries under it and pushes none", () => {
  const calls: string[] = [];
  const h = { replaceState: (s: unknown) => calls.push(`replace ${(s as Nav).screen.name}`), pushState: (s: unknown) => calls.push(`push ${(s as Nav).screen.name}`) };
  const at = seedHistory(h, "?open=t1", { screen: { name: "thread", id: "t1" }, sheet: null, backLayer: 1 });
  assert.deepEqual(calls, ["replace thread"], "the layer's mark goes; the thread's own entries are under it already");
  assert.equal(at.screen.name, "thread");
  // An entry without the mark is seeded as before.
  calls.length = 0;
  seedHistory(h, "?open=t1", { screen: { name: "thread", id: "t1" }, sheet: null });
  assert.deepEqual(calls, ["replace inbox", "push thread"]);
  // The mark is the entry's, not the screen's: it never reaches the navigation state.
  assert.deepEqual(navFromHistory({ screen: { name: "new" }, sheet: null, backLayer: 2 }, ""), { screen: { name: "new" }, sheet: null });
});
