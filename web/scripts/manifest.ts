// The web app manifest (SPEC 0.5 V0.15). Colours come from the design tokens in src/base.css.
import { iconUrl } from "../src/icon-paths.ts";

export const THEME = { light: "#f6f7f8", dark: "#0f1419" } as const; // --surface, light and dark

export const ICONS = [
  { src: iconUrl("any192"), sizes: "192x192", type: "image/png", purpose: "any" },
  { src: iconUrl("any512"), sizes: "512x512", type: "image/png", purpose: "any" },
  { src: iconUrl("maskable512"), sizes: "512x512", type: "image/png", purpose: "maskable" },
];

export function manifest() {
  return {
    id: "/",
    name: "Fyr",
    short_name: "Fyr",
    description: "The inbox for your coding agents.",
    start_url: "/",
    scope: "/",
    display: "standalone",
    orientation: "any",
    background_color: THEME.light,
    theme_color: THEME.light,
    icons: ICONS,
  };
}
