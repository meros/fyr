// The mock host's Changes tab (V0.25): a branch's files and their patches. Loaded with the mock Api only.
import type { ChangesDTO, FilePatchDTO } from "./changes.ts";
import { LONG_PROJECT } from "./mock-util.ts";

const LONG_CHANGE_PATH = `infrastructure/${LONG_PROJECT}/modules/opensearch/cluster/variables-for-the-search-cluster-and-its-dashboards.tf`;

/** V0.65: the mock's Viewed marks, `${threadId}:${path}` → blob. t_done starts with its test file viewed. */
export const mockViewed = new Map<string, string>();

/** A stable fake blob id for a mock path. */
function mockBlob(path: string): string {
  let h = 0;
  for (const ch of path) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return h.toString(16).padStart(8, "0").repeat(5);
}

/** The branch diff of a mock thread; t_deploy is outside git. t_done also has a look 3 h ago (V0.65). */
export function mockChanges(threadId: string): ChangesDTO {
  if (threadId === "t_deploy") return { git: false };
  // V0.73 (2): a checkout on its base with nothing changed.
  if (threadId === "t_ended") return { git: true, root: "/home/owner/src/fyr", branch: "main", base: "origin/main", mergeBase: "262ec20a1b", commits: 0, head: "262ec20a1b2c3d4e5f60718293a4b5c6d7e8f901", truncated: false, files: [] };
  if (threadId === "t_done" && !mockViewed.has("t_done:init")) {
    mockViewed.set("t_done:init", "");
    mockViewed.set("t_done:web/test/map.test.ts", mockBlob("web/test/map.test.ts"));
    mockViewed.set("t_done:web/src/map/lasso.ts", mockBlob("web/src/map/lasso.ts"));
  }
  const files = [
    { path: "docs/old-notes.md", status: "deleted" as const, additions: 0, deletions: 12, binary: false },
    { path: LONG_CHANGE_PATH, status: "modified" as const, additions: 4, deletions: 2, binary: false, blob: mockBlob(LONG_CHANGE_PATH) },
    { path: "web/public/marker.png", status: "added" as const, additions: 0, deletions: 0, binary: true, blob: mockBlob("web/public/marker.png") },
    { path: "web/src/map/lasso.ts", oldPath: "web/src/map/select.ts", status: "renamed" as const, additions: 6, deletions: 2, binary: false, blob: mockBlob("web/src/map/lasso.ts") },
    { path: "web/src/map/render.ts", status: "modified" as const, additions: 42, deletions: 18, binary: false, blob: mockBlob("web/src/map/render.ts") },
    { path: "web/test/map.test.ts", status: "added" as const, additions: 64, deletions: 0, binary: false, blob: mockBlob("web/test/map.test.ts") },
  ].map((f) => ({ ...f, viewed: mockViewed.get(`${threadId}:${f.path}`) === (f.blob ?? "") && mockViewed.has(`${threadId}:${f.path}`) }));
  const c: ChangesDTO = {
    git: true,
    root: "/home/owner/src/acme/web",
    branch: "fix/map-lasso-mobile",
    base: "origin/main",
    mergeBase: "3f2a9c1d0e",
    commits: 3,
    head: "7be1c0d4a9f2e3b5c6d7e8f90a1b2c3d4e5f6a7b",
    truncated: false,
    files,
  };
  if (threadId === "t_done") {
    const pick = (p: string, add: number, del: number) => ({ ...files.find((f) => f.path === p)!, additions: add, deletions: del });
    c.since = {
      head: "51d0aa3c9e8b7f6a5d4c3b2a1908f7e6d5c4b3a2",
      at: new Date(Date.now() - 3 * 3600_000).toISOString(),
      mode: "ancestor",
      commits: 2,
      files: [pick(LONG_CHANGE_PATH, 2, 1), pick("web/src/map/render.ts", 3, 1)],
    };
  }
  return c;
}

/** One mock file's unified diff. */
export function mockFilePatch(path: string): FilePatchDTO {
  const head = (extra = "") => `diff --git a/${path} b/${path}\n${extra}--- a/${path}\n+++ b/${path}\n`;
  if (path.endsWith(".png")) return { path, patch: `diff --git a/${path} b/${path}\nnew file mode 100644\nBinary files /dev/null and b/${path} differ\n`, binary: true, truncated: false };
  if (path === LONG_CHANGE_PATH) {
    const patch =
      head("index 1a2b3c4..5d6e7f8 100644\n") +
      "@@ -12,7 +12,9 @@ variable \"cluster\" {\n" +
      '   type = object({\n     name = string\n-    size = number\n+    size          = number\n+    dedicated_master_count_for_the_cluster_when_it_runs_in_three_availability_zones = optional(number, 3)\n' +
      `+    description = "${"A very long description line without breaks that must scroll sideways inside the diff and never widen the page. ".repeat(3)}"\n` +
      "     zones = list(string)\n-    tier  = string\n+    tier  = optional(string, \"hot\")\n+    warm  = optional(bool, false)\n   })\n }\n";
    return { path, patch, binary: false, truncated: false };
  }
  if (path === "docs/old-notes.md") {
    const lines = Array.from({ length: 12 }, (_, i) => `-Note ${i + 1}: the old map notes.`).join("\n");
    return { path, patch: `diff --git a/${path} b/${path}\ndeleted file mode 100644\n--- a/${path}\n+++ /dev/null\n@@ -1,12 +0,0 @@\n${lines}\n`, binary: false, truncated: false };
  }
  if (path === "web/test/map.test.ts") {
    const lines = Array.from({ length: 20 }, (_, i) => `+test("map case ${i + 1}", () => expect(render(${i})).toMatchSnapshot());`).join("\n");
    return { path, patch: `diff --git a/${path} b/${path}\nnew file mode 100644\n--- /dev/null\n+++ b/${path}\n@@ -0,0 +1,20 @@\n${lines}\n`, binary: false, truncated: false };
  }
  const patch =
    head("index 0f1e2d3..4c5b6a7 100644\n") +
    "@@ -40,9 +40,12 @@ export function renderMap(map: MapState, opts: RenderOptions) {\n" +
    "   const layer = map.layers.find((l) => l.id === opts.layer);\n" +
    "   if (!layer) return;\n" +
    "-  for (const f of layer.features) drawFeature(ctx, f);\n" +
    "+  const visible = layer.features.filter((f) => inView(map.view, f.bbox));\n" +
    "+  for (const f of visible) drawFeature(ctx, f);\n" +
    "+  stats.drawn = visible.length;\n" +
    "   ctx.restore();\n" +
    " }\n" +
    "@@ -120,6 +123,6 @@ function drawFeature(ctx: CanvasRenderingContext2D, f: Feature) {\n" +
    "   ctx.beginPath();\n" +
    "-  ctx.lineWidth = 2;\n" +
    "+  ctx.lineWidth = f.selected ? 3 : 2;\n" +
    "   ctx.stroke();\n" +
    "\\ No newline at end of file\n";
  return { path, patch, binary: false, truncated: false };
}
