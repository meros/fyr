// SPEC 0.5 V0.42: the host folder picker's pure helpers.
import { test } from "node:test";
import assert from "node:assert/strict";
import { breadcrumbs, complete, completions, filterSections, folderNameError, isPathQuery, splitPath } from "../src/folders.ts";
import { tildePath } from "../src/paths.ts";

const H = "/home/owner";
const d = (name: string, dir = H) => ({ name, path: `${dir}/${name}` });

test("a path browses, a word searches", () => {
  assert.equal(isPathQuery("~/src"), true);
  assert.equal(isPathQuery(" /home/owner"), true);
  assert.equal(isPathQuery("fyr"), false);
  assert.equal(tildePath("/home/owner/src/fyr"), "~/src/fyr");
  assert.equal(tildePath("/srv/x", H), "/srv/x");
  assert.equal(tildePath("/home/owner", H), "~");
});

test("a typed path splits into the folder and the start of the child", () => {
  assert.deepEqual(splitPath("~"), { dir: "~", prefix: "" });
  assert.deepEqual(splitPath("~/"), { dir: "~", prefix: "" });
  assert.deepEqual(splitPath("~/src/fy"), { dir: "~/src", prefix: "fy" });
  assert.deepEqual(splitPath("~/src/"), { dir: "~/src", prefix: "" });
  assert.deepEqual(splitPath("/ho"), { dir: "/", prefix: "ho" });
});

test("completion: prefix match on the children, hidden only for a dot, one match adds /", () => {
  const kids = [d("src"), d("Sites"), d("secrets"), d(".config"), d("git")];
  assert.deepEqual(completions(kids, "s").map((x) => x.name), ["src", "Sites", "secrets"]);
  assert.deepEqual(completions(kids, "").map((x) => x.name), ["src", "Sites", "secrets", "git"]);
  assert.deepEqual(completions(kids, ".c").map((x) => x.name), [".config"]);
  assert.equal(complete("~/g", kids), "~/git/");
  assert.equal(complete("~/se", kids), "~/secrets/");
  assert.equal(complete("~/s", kids), "~/s", "src, Sites, secrets share only s");
  assert.equal(complete("~/x", kids), "~/x");
  assert.equal(complete("~/sr", [d("src"), d("srv")]), "~/sr");
  assert.equal(complete("~/a", [d("app-one"), d("app-two")]), "~/app-");
});

test("breadcrumbs start at ~", () => {
  assert.deepEqual(breadcrumbs(H, H), [{ label: "~", path: H }]);
  assert.deepEqual(breadcrumbs(`${H}/src/fyr`, H), [
    { label: "~", path: H },
    { label: "src", path: `${H}/src` },
    { label: "fyr", path: `${H}/src/fyr` },
  ]);
  assert.deepEqual(breadcrumbs(`${H}x/y`, H), [{ label: `${H}x/y`, path: `${H}x/y` }]);
});

test("search across sections: every word in the name or path; a folder shows once; empty sections go", () => {
  const sections = [
    { key: "recent", title: "Recent", dirs: [d("fyr", `${H}/git`), d("notes")] },
    { key: "repos", title: "Repos found", dirs: [d("fyr", `${H}/git`), d("web", `${H}/src/acme`)] },
  ];
  assert.deepEqual(filterSections(sections, "").map((s) => s.dirs.map((x) => x.name)), [["fyr", "notes"], ["web"]]);
  assert.deepEqual(filterSections(sections, "acme").map((s) => s.key), ["repos"]);
  assert.deepEqual(filterSections(sections, "git fyr", H).map((s) => s.dirs.length), [1]);
  assert.deepEqual(filterSections(sections, "zzz"), []);
});

test("a new folder's name: not empty, no /, not taken", () => {
  assert.match(folderNameError(" ", [])!, /Name/);
  assert.match(folderNameError("a/b", [])!, /\//);
  assert.match(folderNameError("..", [])!, /not a folder name/);
  assert.match(folderNameError("src", ["src"])!, /already/);
  assert.equal(folderNameError("nytt", ["src"]), null);
});
