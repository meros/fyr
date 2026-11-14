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
 * The Fyr mark: a lighthouse. A light tower with a gallery and a pointed cap
 * stands on a dark headland; its amber lamp throws two beams to the sides.
 * The tile is night with a soft vertical gradient. The badge is the same
 * silhouette in white on transparent; the maskable icon scales the mark into
 * the safe zone and fills the square.
 */
export function drawMark(size: number, opts: MarkOptions = {}): Uint8Array {
  const px = new Uint8Array(size * size * 4);
  const k = opts.maskable ? 0.8 : 1;
  // Map unit coordinates of the drawing into the (possibly shrunk) mark box.
  const toMark = (x: number, y: number): [number, number] => [(x - 0.5) / k + 0.5, (y - 0.5) / k + 0.5];
  const radius = opts.maskable || opts.badge ? 0 : 0.225;
  const top = hex("#22374b");
  const bottom = hex("#0d151d");
  const tower = hex("#eef1f4");
  const shade = hex("#c3ccd5");
  const lampCore = hex("#ffbf3d");
  const amber = hex(AMBER);
  const land = hex("#1c2c3b");
  const S = 4;

  const inTile = (x: number, y: number) => {
    if (opts.badge) return false;
    if (radius === 0) return true;
    const dx = Math.max(radius - x, 0, x - (1 - radius));
    const dy = Math.max(radius - y, 0, y - (1 - radius));
    return dx * dx + dy * dy <= radius * radius;
  };
  const LX = 0.5, LY = 0.35; // the lamp
  const inTower = (x: number, y: number) => {
    if (y < 0.425 || y > 0.8) return false;
    const t = (y - 0.425) / (0.8 - 0.425);
    return Math.abs(x - 0.5) <= 0.052 + 0.048 * t;
  };
  const inGallery = (x: number, y: number) => y >= 0.4 && y <= 0.43 && Math.abs(x - 0.5) <= 0.085;
  const inLantern = (x: number, y: number) => y >= 0.305 && y <= 0.4 && Math.abs(x - 0.5) <= 0.045;
  const inCap = (x: number, y: number) => y >= 0.25 && y <= 0.31 && Math.abs(x - 0.5) <= 0.06 * ((y - 0.25) / 0.06);
  const inLamp = (x: number, y: number) => Math.hypot(x - LX, y - LY) <= 0.036;
  const inBand = (_x: number, y: number) => y >= 0.575 && y <= 0.625;
  const inLand = (x: number, y: number) => ((x - 0.5) / 0.62) ** 2 + ((y - 1.02) / 0.24) ** 2 <= 1;
  /** beam alpha: two wedges from the lamp, left and right, fading out */
  const beam = (x: number, y: number) => {
    const dx = x - LX, dy = y - LY, d = Math.hypot(dx, dy);
    if (d < 0.04 || d > 0.46) return 0;
    const a = Math.abs(Math.atan2(dy, Math.abs(dx)));
    const half = 0.17;
    if (a > half) return 0;
    const edge = Math.min(1, (half - a) / 0.05);
    return 0.78 * edge * Math.pow(1 - (d - 0.04) / 0.42, 1.3);
  };
  const glow = (x: number, y: number) => {
    const d = Math.hypot(x - LX, y - LY);
    return d > 0.13 ? 0 : 0.45 * (1 - d / 0.13) ** 2;
  };

  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      let r = 0, g = 0, b = 0, a = 0;
      for (let sy = 0; sy < S; sy++) {
        for (let sx = 0; sx < S; sx++) {
          const ux = (x + (sx + 0.5) / S) / size;
          const uy = (y + (sy + 0.5) / S) / size;
          const [mx, my] = toMark(ux, uy);
          // Paint back to front into one sample.
          let c: RGB = [0, 0, 0];
          let al = 0;
          const over = (col: RGB, alpha: number) => {
            if (alpha <= 0) return;
            const out = alpha + al * (1 - alpha);
            for (let i = 0; i < 3; i++) c[i] = (col[i] * alpha + c[i] * al * (1 - alpha)) / out;
            al = out;
          };
          if (opts.badge) {
            const solid = inTower(mx, my) || inGallery(mx, my) || inLantern(mx, my) || inCap(mx, my);
            over([255, 255, 255], Math.max(solid ? 1 : 0, Math.min(1, beam(mx, my) * 1.4)));
          } else if (inTile(ux, uy)) {
            const t = Math.min(1, Math.max(0, uy));
            over([0, 1, 2].map((i) => top[i] + (bottom[i] - top[i]) * t) as RGB, 1);
            over(amber, beam(mx, my));
            over(amber, glow(mx, my));
            if (inLand(mx, my)) over(land, 1);
            if (inTower(mx, my)) over(inBand(mx, my) ? shade : tower, 1);
            if (inGallery(mx, my) || inLantern(mx, my) || inCap(mx, my)) over(tower, 1);
            if (inLamp(mx, my)) over(lampCore, 1);
          }
          r += c[0] * al; g += c[1] * al; b += c[2] * al; a += al;
        }
      }
      const i = (y * size + x) * 4;
      if (a <= 0) continue;
      px[i] = Math.round(r / a);
      px[i + 1] = Math.round(g / a);
      px[i + 2] = Math.round(b / a);
      px[i + 3] = Math.round((a / (S * S)) * 255);
    }
  }
  return px;
}

export function markPng(size: number, opts: MarkOptions = {}): Buffer {
  return encodePng(size, size, drawMark(size, opts));
}
