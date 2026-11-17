import { test } from "node:test";
import assert from "node:assert/strict";
import { iconFileError, projectIconUrl, setByAgent, setupLine } from "../src/projects.ts";

test("V0.116: a project with an image has an icon URL with its version, sized for the place", () => {
  assert.equal(projectIconUrl({ id: "p_fyr", image: "abc", hostId: "H1" }, 16), "/api/h/H1/projects/p_fyr/icon?size=64&v=abc");
  assert.equal(projectIconUrl({ id: "p_fyr", image: "abc", hostId: "H1" }, 44), "/api/h/H1/projects/p_fyr/icon?size=128&v=abc");
  assert.equal(projectIconUrl({ id: "p_fyr", image: "abc" }, 44), null, "a project with no host cannot be read from one");
  assert.equal(projectIconUrl({ id: "p_fyr" }, 16), null, "no image: the glyph");
  assert.equal(projectIconUrl({ image: "abc" }, 16), null, "a project the client just made has no id yet");
  assert.equal(projectIconUrl(undefined, 16), null);
});

test("V0.116: only the agent's values are marked Set by agent", () => {
  const p = { by: { name: "agent", color: "owner", image: "" } };
  assert.equal(setByAgent(p, "name"), true);
  assert.equal(setByAgent(p, "color"), false);
  assert.equal(setByAgent(p, "image"), false);
  assert.equal(setByAgent({}, "name"), false);
});

test("V0.116: the setup line says what the run did or why it could not start", () => {
  assert.equal(setupLine({}), null);
  assert.equal(setupLine({ setup: { at: "x", thread: "t_1" } })?.error, false);
  const e = setupLine({ setup: { at: "x", error: "no Haiku; Settings → System agents" } });
  assert.equal(e?.error, true);
  assert.match(e!.text, /could not start: no Haiku/);
});

test("V0.116: the upload takes PNG, JPEG and GIF up to 2 MB", () => {
  assert.equal(iconFileError({ type: "image/png", size: 1000 }), null);
  assert.equal(iconFileError({ type: "image/jpeg", size: 2 * 1024 * 1024 }), null);
  assert.match(iconFileError({ type: "image/svg+xml", size: 10 })!, /PNG, JPEG or GIF/);
  assert.match(iconFileError({ type: "image/webp", size: 10 })!, /PNG/);
  assert.match(iconFileError({ type: "image/png", size: 2 * 1024 * 1024 + 1 })!, /2 MB/);
});
