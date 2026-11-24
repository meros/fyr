// The mock host's files (V0.message-links-files): what a path in a mock message opens. Loaded with the mock Api only.
import type { FileStat } from "./filelinks.ts";

const ROOT = "/home/owner/src/acme/web";

const LASSO_TEST = `import { describe, expect, it, beforeEach } from "vitest";
import { lasso } from "./lasso";
import { fixtureMap } from "../test/fixtures";

describe("lasso", () => {
  let map: ReturnType<typeof fixtureMap>;

  // The slow part: a whole map for every test.
  beforeEach(() => {
    map = fixtureMap({ tiles: 4096 });
  });

  it("selects the markers inside the polygon", () => {
    const hit = lasso(map, [
      [0, 0],
      [10, 0],
      [10, 10],
      [0, 10],
    ]);
    expect(hit.map((m) => m.id)).toEqual(["a", "b"]);
  });

  it("selects nothing for an open path", () => {
    expect(lasso(map, [[0, 0]])).toEqual([]);
  });
});
`;

const TEXT: Record<string, string> = {
  "src/map/lasso.test.ts": LASSO_TEST,
  "vitest.config.ts": `import { defineConfig } from "vitest/config";\n\nexport default defineConfig({\n  test: { environment: "jsdom", isolate: false },\n});\n`,
};
const IMAGE: Record<string, string> = {
  "public/marker.png":
    "data:image/svg+xml," +
    encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="240" height="240"><rect width="240" height="240" fill="#e8e2d8"/><circle cx="120" cy="100" r="46" fill="#2f6f8f"/><path d="M120 210 84 130h72z" fill="#2f6f8f"/></svg>'),
};
const OTHER: Record<string, number> = { "dist/tiles.bin": 48_213 };

/** The path below the mock thread's folder: as written, or under its absolute or ~ form. */
function rel(path: string): string {
  for (const prefix of [ROOT + "/", "~/src/acme/web/", "./"]) if (path.startsWith(prefix)) return path.slice(prefix.length);
  return path;
}

export function mockFileStat(paths: string[]): FileStat[] {
  const out: FileStat[] = [];
  for (const path of paths) {
    const r = rel(path);
    const abs = `${ROOT}/${r}`;
    if (r in TEXT) out.push({ path, abs, size: TEXT[r].length, kind: "text" });
    else if (r in IMAGE) out.push({ path, abs, size: 1204, kind: "image" });
    else if (r in OTHER) out.push({ path, abs, size: OTHER[r], kind: "other" });
  }
  return out;
}

export function mockFileText(path: string): string {
  return TEXT[rel(path)] ?? "";
}

export function mockFileUrl(path: string): string {
  const r = rel(path);
  return IMAGE[r] ?? "data:text/plain;charset=utf-8," + encodeURIComponent(TEXT[r] ?? "");
}
