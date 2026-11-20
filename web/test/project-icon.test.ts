import { test } from "node:test";
import assert from "node:assert/strict";
import { stampProject } from "../src/hostfeed.ts";
import { iconFileError, linkProjects, projectIconUrl, setByAgent, setupLine } from "../src/projects.ts";
import type { Project } from "../src/types.ts";
import { hid } from "./helpers.ts";

test("V0.116: a project with an image has an icon URL with its version, sized for the place", () => {
  assert.equal(projectIconUrl({ id: "p_fyr", image: "abc", hostId: hid("H1") }, 16), "/api/h/H1/projects/p_fyr/icon?size=64&v=abc");
  assert.equal(projectIconUrl({ id: "p_fyr", image: "abc", hostId: hid("H1") }, 44), "/api/h/H1/projects/p_fyr/icon?size=128&v=abc");
  assert.equal(projectIconUrl({ id: "p_fyr", image: "abc" }, 44), null, "a project with no host cannot be read from one");
  assert.equal(projectIconUrl({ id: "p_fyr" }, 16), null, "no image: the glyph");
  assert.equal(projectIconUrl({ image: "abc" }, 16), null, "a project the client just made has no id yet");
  assert.equal(projectIconUrl(undefined, 16), null);
});

test("V0.project-icons-on-demand: the URL names the project by its host's own id, as the host knows it", () => {
  const record = (id: string, hostId: string, image?: string) => stampProject({ id, name: "Fyr", color: "", icon: { kind: "monogram", value: "" }, folder: "", link: "l_1", ...(image ? { image } : {}) } as Project, { id: hid(hostId), name: hostId, online: true });
  assert.equal(projectIconUrl(record("p_fyr", "H1", "abc"), 16), "/api/h/H1/projects/p_fyr/icon?size=64&v=abc");
  const hosts = [{ id: hid("H1"), name: "H1", online: true }, { id: hid("H2"), name: "H2", online: true }];
  const [linked] = linkProjects([record("p_a", "H1"), record("p_b", "H2", "abc")], hosts);
  assert.equal(projectIconUrl(linked, 16), "/api/h/H2/projects/p_b/icon?size=64&v=abc", "a linked project's image is read from the member that has it");
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
