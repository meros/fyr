// A tiny PNG encoder (RGBA, filter 0, zlib from node) and the Fyr mark, drawn without dependencies.
// Used by scripts/build.mjs; pure apart from node:zlib, so it is unit-tested.

import { deflateSync } from "node:zlib";

const CRC_TABLE = (() => {
  const t = new Uint32Array(256);
  for (let n = 0; n < 256; n++) {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    t[n] = c >>> 0;
  }
  return t;
})();

export function crc32(buf: Uint8Array): number {
  let c = 0xffffffff;
  for (let i = 0; i < buf.length; i++) c = CRC_TABLE[(c ^ buf[i]) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

function chunk(type: string, data: Uint8Array): Buffer {
  const out = Buffer.alloc(12 + data.length);
  out.writeUInt32BE(data.length, 0);
  out.write(type, 4, "ascii");
  Buffer.from(data).copy(out, 8);
  out.writeUInt32BE(crc32(out.subarray(4, 8 + data.length)), 8 + data.length);
  return out;
}

export const PNG_SIGNATURE = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);

/** Encode RGBA pixels (width * height * 4 bytes) as a PNG. */
export function encodePng(width: number, height: number, rgba: Uint8Array): Buffer {
  if (rgba.length !== width * height * 4) throw new Error("pixel buffer size does not match");
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(width, 0);
  ihdr.writeUInt32BE(height, 4);
  ihdr[8] = 8; // bit depth
  ihdr[9] = 6; // colour type RGBA
  ihdr[10] = 0;
  ihdr[11] = 0;
  ihdr[12] = 0;
  const raw = Buffer.alloc((width * 4 + 1) * height);
  for (let y = 0; y < height; y++) {
    raw[y * (width * 4 + 1)] = 0; // filter: none
    Buffer.from(rgba.buffer, rgba.byteOffset + y * width * 4, width * 4).copy(raw, y * (width * 4 + 1) + 1);
  }
  return Buffer.concat([PNG_SIGNATURE, chunk("IHDR", ihdr), chunk("IDAT", deflateSync(raw, { level: 9 })), chunk("IEND", new Uint8Array(0))]);
}

type RGB = [number, number, number];
const hex = (h: string): RGB => [parseInt(h.slice(1, 3), 16), parseInt(h.slice(3, 5), 16), parseInt(h.slice(5, 7), 16)];

export const NIGHT = "#14212d";
export const AMBER = "#e8960f";

export interface MarkOptions {
  /** full-bleed background for maskable icons (the mark stays inside the 80 % safe zone) */
  maskable?: boolean;
  /** white mark on transparent, for the Android notification badge */
  badge?: boolean;
}

/**
 * The Fyr mark: a lighthouse-amber disc (the lamp) with a short beam to the upper right,
 * on a night square with rounded corners (or full bleed when maskable).
 */
export function drawMark(size: number, opts: MarkOptions = {}): Uint8Array {
  const px = new Uint8Array(size * size * 4);
  const bg = hex(NIGHT);
  const fg: RGB = opts.badge ? [255, 255, 255] : hex(AMBER);
  // Geometry in unit coordinates (0..1). The maskable icon shrinks the mark into the safe zone.
  const k = opts.maskable ? 0.78 : 1;
  const cx = 0.5 - 0.1 * k;
  const cy = 0.5 + 0.1 * k;
  const r = 0.17 * k;
  const beamLen = 0.27 * k;
  const ang = -Math.PI / 4; // towards the upper right
  const half = 0.22; // half-angle of the beam in radians
  const radius = opts.maskable || opts.badge ? 0 : 0.22; // corner radius of the tile
  const S = 4; // supersampling per axis

  const inTile = (x: number, y: number) => {
    if (opts.badge) return false;
    if (radius === 0) return true;
    const dx = Math.max(radius - x, 0, x - (1 - radius));
    const dy = Math.max(radius - y, 0, y - (1 - radius));
    return dx * dx + dy * dy <= radius * radius;
  };
  /** 0 outside, 1 on the disc, 0..1 along the beam (fades out) */
  const markAlpha = (x: number, y: number): number => {
    const dx = x - cx;
    const dy = y - cy;
    const d = Math.hypot(dx, dy);
    if (d <= r) return 1;
    if (d > r + beamLen) return 0;
    let a = Math.atan2(dy, dx) - ang;
    a = Math.atan2(Math.sin(a), Math.cos(a));
    if (Math.abs(a) > half) return 0;
    const t = (d - r) / beamLen;
    return 0.8 * (1 - 0.75 * t);
  };

  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      let tile = 0;
      let mark = 0;
      for (let sy = 0; sy < S; sy++) {
        for (let sx = 0; sx < S; sx++) {
          const ux = (x + (sx + 0.5) / S) / size;
          const uy = (y + (sy + 0.5) / S) / size;
          if (inTile(ux, uy)) tile++;
          mark += markAlpha(ux, uy);
        }
      }
      const n = S * S;
      const ta = tile / n;
      const ma = mark / n;
      // Composite the mark over the tile, the tile over transparent.
      const outA = ma + ta * (1 - ma);
      const i = (y * size + x) * 4;
      if (outA <= 0) continue;
      for (let c = 0; c < 3; c++) px[i + c] = Math.round((fg[c] * ma + bg[c] * ta * (1 - ma)) / outA);
      px[i + 3] = Math.round(outA * 255);
    }
  }
  return px;
}

export function markPng(size: number, opts: MarkOptions = {}): Buffer {
  return encodePng(size, size, drawMark(size, opts));
}
