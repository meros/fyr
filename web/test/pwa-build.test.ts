import { test } from "node:test";
import assert from "node:assert/strict";
import { inflateSync } from "node:zlib";
import { PNG_SIGNATURE, crc32, drawMark, encodePng, markPng } from "../scripts/icons.ts";
import { manifest } from "../scripts/manifest.ts";

function ihdr(png: Buffer) {
  return { len: png.readUInt32BE(8), type: png.toString("ascii", 12, 16), w: png.readUInt32BE(16), h: png.readUInt32BE(20), depth: png[24], colour: png[25] };
}

test("PNG: signature, IHDR with the right size, valid CRCs, decodable IDAT", () => {
  for (const size of [192, 512]) {
    const png = markPng(size);
    assert.deepEqual(png.subarray(0, 8), PNG_SIGNATURE);
    assert.deepEqual(ihdr(png), { len: 13, type: "IHDR", w: size, h: size, depth: 8, colour: 6 });
    // Walk the chunks and check each CRC.
    let off = 8;
    const types: string[] = [];
    const idat: Buffer[] = [];
    while (off < png.length) {
      const len = png.readUInt32BE(off);
      const type = png.toString("ascii", off + 4, off + 8);
      assert.equal(png.readUInt32BE(off + 8 + len), crc32(png.subarray(off + 4, off + 8 + len)), `${type} CRC`);
      if (type === "IDAT") idat.push(png.subarray(off + 8, off + 8 + len));
      types.push(type);
      off += 12 + len;
    }
    assert.deepEqual(types, ["IHDR", "IDAT", "IEND"]);
    assert.equal(inflateSync(Buffer.concat(idat)).length, (size * 4 + 1) * size);
  }
});

test("PNG encoder rejects a wrong buffer size; crc32 matches the PNG reference value", () => {
  assert.throws(() => encodePng(2, 2, new Uint8Array(3)));
  assert.equal(crc32(new TextEncoder().encode("IEND")), 0xae426082);
});

test("the mark: night tile with an amber lamp; maskable is full bleed; badge is white on transparent", () => {
  const n = 64;
  const at = (px: Uint8Array, x: number, y: number) => [...px.subarray((y * n + x) * 4, (y * n + x) * 4 + 4)];
  const any = drawMark(n);
  assert.equal(at(any, 0, 0)[3], 0, "rounded corner is transparent");
  assert.deepEqual(at(any, 4, n / 2), [0x14, 0x21, 0x2d, 255], "night background");
  assert.deepEqual(at(any, Math.round(n * 0.4), Math.round(n * 0.6)), [0xe8, 0x96, 0x0f, 255], "amber lamp");
  assert.equal(at(drawMark(n, { maskable: true }), 0, 0)[3], 255, "maskable fills the square");
  const badge = drawMark(n, { badge: true });
  assert.equal(at(badge, 4, n / 2)[3], 0);
  assert.deepEqual(at(badge, Math.round(n * 0.4), Math.round(n * 0.6)), [255, 255, 255, 255]);
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
