// The URLs of the app icons (favicon, PWA, Apple touch icon, push). The service worker serves /icons/ cache-first
// as immutable (sw.ts), so a new icon needs a new folder: rename ICON_DIR when design/logo/icons.sh makes new ones.
// V0.110: "lighthouse", the rendered lighthouse; the hand-drawn mark before it lived in /icons/.
export const ICON_DIR = "/icons/lighthouse";

export const ICON_FILES = {
  any192: "icon-192.png",
  any512: "icon-512.png",
  maskable512: "maskable-512.png",
  apple180: "apple-touch-icon.png",
  favicon32: "favicon-32.png",
  badge96: "badge-96.png",
} as const;

export const iconUrl = (name: keyof typeof ICON_FILES) => `${ICON_DIR}/${ICON_FILES[name]}`;
