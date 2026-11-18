// V0.ai-search: which threads each host is asked about, and how the answers become one list.
import { test } from "node:test";
import assert from "node:assert/strict";
import { AI_SEARCH_THREADS, askPerHost, mergeHits } from "../src/ai-search.ts";
import { matchesQuery } from "../src/store.ts";
import { thread } from "./helpers.ts";

const T = (id: string, hostId: string | undefined, extra = {}) => thread(`${hostId}/${id}`, "seen", "seen", { hostId, rid: id, ...extra });

test("each host is asked about its own threads by their own ids, in list order", () => {
  const m = askPerHost([T("a", "H1"), T("b", "H2"), T("c", "H1")]);
  assert.deepEqual([...m.entries()], [["H1", ["a", "c"]], ["H2", ["b"]]]);
});

test("an offline host and a thread with no host are not asked", () => {
  assert.equal(askPerHost([T("a", "H1", { hostOffline: "2026-11-18T10:00:00Z" }), T("b", undefined)]).size, 0);
});

test("a host is asked about a bounded number of threads", () => {
  const many = Array.from({ length: AI_SEARCH_THREADS + 5 }, (_, i) => T(`t${i}`, "H1"));
  assert.equal(askPerHost(many).get("H1")!.length, AI_SEARCH_THREADS);
});

test("answers merge best first across hosts, as refs, without repeats", () => {
  const out = mergeHits([
    { host: "H1", hits: [{ id: "a", why: "x" }, { id: "b" }, { id: "a" }] },
    { host: "H2", hits: [{ id: "c" }] },
  ]);
  assert.deepEqual(out.map((h) => h.id), ["H1/a", "H2/c", "H1/b"]);
  assert.equal(out[0].why, "x");
  assert.equal(out[2].why, "");
});

test("typed search: every word counts, in any order", () => {
  const t = thread("a", "seen", "seen", { title: "Fix the login page", project: "web" });
  assert.equal(matchesQuery(t, "page  login"), true);
  assert.equal(matchesQuery(t, "web fix"), true);
  assert.equal(matchesQuery(t, "login oauth"), false);
});
