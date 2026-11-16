// V0.110: every app icon is the rendered lighthouse, committed in assets/icons and copied by the build to ICON_DIR.
import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { inflateSync } from "node:zlib";
import { ICON_DIR, ICON_FILES, iconUrl } from "../src/icon-paths.ts";
import { manifest } from "../scripts/manifest.ts";

const web = new URL("..", import.meta.url).pathname;

/** Decode an 8-bit RGB or RGBA PNG without interlace (what design/logo/icons.sh writes). */
function decodePng(png: Buffer) {
  assert.deepEqual([...png.subarray(0, 8)], [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a], "PNG signature");
  let off = 8, w = 0, h = 0, ch = 0;
  const idat: Buffer[] = [];
  while (off < png.length) {
    const len = png.readUInt32BE(off);
    const type = png.toString("ascii", off + 4, off + 8);
    const data = png.subarray(off + 8, off + 8 + len);
    if (type === "IHDR") {
      w = data.readUInt32BE(0);
      h = data.readUInt32BE(4);
      assert.equal(data[8], 8, "8-bit");
      assert.ok(data[9] === 2 || data[9] === 6, `RGB or RGBA, not colour type ${data[9]}`);
      assert.equal(data[12], 0, "not interlaced");
      ch = data[9] === 6 ? 4 : 3;
    }
    if (type === "IDAT") idat.push(data);
    off += 12 + len;
  }
  const raw = inflateSync(Buffer.concat(idat));
  const stride = w * ch;
  const px = Buffer.alloc(h * stride);
  for (let y = 0; y < h; y++) {
    const f = raw[y * (stride + 1)];
    for (let x = 0; x < stride; x++) {
      const v = raw[y * (stride + 1) + 1 + x];
      const a = x >= ch ? px[y * stride + x - ch] : 0;
      const b = y > 0 ? px[(y - 1) * stride + x] : 0;
      const c = x >= ch && y > 0 ? px[(y - 1) * stride + x - ch] : 0;
      const p = a + b - c, pa = Math.abs(p - a), pb = Math.abs(p - b), pc = Math.abs(p - c);
      const pred = [0, a, b, (a + b) >> 1, pa <= pb && pa <= pc ? a : pb <= pc ? b : c][f];
      px[y * stride + x] = (v + pred) & 0xff;
    }
  }
  const at = (x: number, y: number) => [...px.subarray((y * w + x) * ch, (y * w + x) * ch + ch)];
  return { w, h, ch, at };
}

const SIZES: Record<keyof typeof ICON_FILES, number> = { any192: 192, any512: 512, maskable512: 512, apple180: 180, favicon32: 32, badge96: 96 };

test("the build writes every icon under ICON_DIR at its size; index.html, the manifest and sw.js point at them", (t) => {
  const out = mkdtempSync(join(tmpdir(), "fyr-icons-"));
  t.after(() => rmSync(out, { recursive: true, force: true }));
  execFileSync(process.execPath, [join(web, "scripts/build.mjs"), "--out", out], { stdio: "pipe" });
  for (const [key, size] of Object.entries(SIZES) as [keyof typeof ICON_FILES, number][]) {
    const png = decodePng(readFileSync(join(out, iconUrl(key).slice(1))));
    assert.deepEqual([png.w, png.h], [size, size], `${key} is ${size} px`);
  }
  const html = readFileSync(join(out, "index.html"), "utf8");
  assert.match(html, new RegExp(`rel="icon"[^>]*sizes="32x32" href="${iconUrl("favicon32")}"`));
  assert.match(html, new RegExp(`rel="apple-touch-icon"[^>]*href="${iconUrl("apple180")}"`));
  const m = JSON.parse(readFileSync(join(out, "manifest.webmanifest"), "utf8"));
  for (const i of m.icons) assert.ok(i.src.startsWith(`${ICON_DIR}/`), i.src);
  const sw = readFileSync(join(out, "sw.js"), "utf8");
  for (const n of Object.values(ICON_FILES)) assert.ok(sw.includes(`"${ICON_DIR}/${n}"`), `sw.js precaches ${n}`);
  assert.ok(!html.includes('"/icons/icon-192.png"'), "the old icon URL is gone");
});

test("the icons: any is transparent around the tower; maskable and Apple fill the square with night; badge is white", () => {
  const icon = (k: keyof typeof ICON_FILES) => decodePng(readFileSync(join(web, "assets/icons", ICON_FILES[k])));
  const any = icon("any512");
  assert.equal(any.ch, 4);
  assert.equal(any.at(4, 500)[3], 0, "corner is transparent");
  const tower = any.at(256, 300);
  assert.ok(tower[3] === 255 && tower[0] > 150, `opaque light tower ${tower}`);
  for (const k of ["maskable512", "apple180"] as const) {
    const p = icon(k);
    assert.deepEqual(p.at(0, 0).slice(0, 3), [0x14, 0x21, 0x2d], `${k} corner is --night`);
    // The safe zone is the centred circle of radius 40 %: outside it there is only the tile and the fading beams.
    const r = p.w * 0.4, c = p.w / 2;
    const lower = p.at(Math.round(c), Math.round(c + r + (p.w / 2 - r) / 2));
    assert.deepEqual(lower.slice(0, 3), [0x14, 0x21, 0x2d], `${k} has nothing below the safe zone`);
  }
  const badge = icon("badge96");
  assert.equal(badge.at(2, 94)[3], 0, "badge corner is transparent");
  const b = badge.at(48, 60);
  assert.deepEqual(b, [255, 255, 255, 255], `badge tower is solid white ${b}`);
});

test("manifest has the fields installability needs", () => {
  const m = manifest();
  assert.equal(m.name, "Fyr");
  assert.equal(m.short_name, "Fyr");
  assert.equal(m.start_url, "/");
  assert.equal(m.scope, "/");
  assert.equal(m.display, "standalone");
  assert.match(m.background_color, /^#[0-9a-f]{6}$/);
  assert.match(m.theme_color, /^#[0-9a-f]{6}$/);
  const sizes = m.icons.map((i) => `${i.sizes}/${i.purpose}`);
  assert.ok(sizes.includes("192x192/any"));
  assert.ok(sizes.includes("512x512/any"));
  assert.ok(sizes.includes("512x512/maskable"));
});
