import { test } from "node:test";
import assert from "node:assert/strict";
import { createSendDedupe, createSubmitGuard } from "../src/send-guard.ts";

// V0.quota-auto-continue: synchronous submit guard prevents rapid double/triple
// sends in the same tick (before a re-render disables the button) and always
// releases on completion or error.
test("submit guard blocks re-entrant sends until the in-flight send settles", async () => {
  const states: boolean[] = [];
  const guard = createSubmitGuard((b) => states.push(b));
  let calls = 0;
  let resolveSend!: (ok: boolean) => void;

  const send = () =>
    guard.run(async () => {
      calls++;
      return await new Promise<boolean>((r) => {
        resolveSend = r;
      });
    });

  // Three rapid triggers in the same millisecond (e.g. rapid taps / Enter repeat).
  const p1 = send();
  const p2 = send();
  const p3 = send();

  assert.equal(calls, 1, "only the first submit starts");
  assert.equal(guard.busy(), true);
  assert.deepEqual(states, [true]);
  assert.equal(await p2, false);
  assert.equal(await p3, false);

  resolveSend(true);
  assert.equal(await p1, true);
  assert.equal(guard.busy(), false);
  assert.deepEqual(states, [true, false]);

  // After settling, a new send is allowed.
  const ok = await guard.run(async () => {
    calls++;
    return true;
  });
  assert.equal(ok, true);
  assert.equal(calls, 2);
});

test("submit guard releases after a rejected promise", async () => {
  const guard = createSubmitGuard();
  await assert.rejects(
    guard.run(async () => {
      throw new Error("boom");
    }),
    /boom/,
  );
  assert.equal(guard.busy(), false);
  assert.equal(await guard.run(async () => true), true);
});

// V0.quota-auto-continue: useThreadCommands.send deduplicates identical
// concurrent in-flight sends on the same thread without blocking distinct
// messages or later repeats.
test("send dedupe shares one in-flight request for identical concurrent sends", async () => {
  const dedupe = createSendDedupe<boolean>();
  let calls = 0;
  let finish!: (ok: boolean) => void;

  const fire = (id: string, text: string) =>
    dedupe(id, { text }, () => {
      calls++;
      return new Promise<boolean>((r) => {
        finish = r;
      });
    });

  const a = fire("t_1", "Status?");
  const b = fire("t_1", "Status?");
  const c = fire("t_1", "Status?");
  assert.equal(calls, 1, "identical concurrent sends collapse into one");
  assert.equal(a, b);
  assert.equal(b, c);

  // Different text or different thread is NOT collapsed.
  let otherCalls = 0;
  const d = dedupe("t_1", { text: "Different" }, async () => {
    otherCalls++;
    return true;
  });
  const e = dedupe("t_2", { text: "Status?" }, async () => {
    otherCalls++;
    return true;
  });
  assert.equal(await d, true);
  assert.equal(await e, true);
  assert.equal(otherCalls, 2);

  finish(true);
  assert.equal(await a, true);

  // After the first request settles, sending "Status?" again later proceeds normally.
  const again = await dedupe("t_1", { text: "Status?" }, async () => {
    calls++;
    return true;
  });
  assert.equal(again, true);
  assert.equal(calls, 2);
});
