#!/usr/bin/env python3
"""Generates the Fyr design system files under ../system/."""
import json, os, datetime

ROOT = os.path.join(os.path.dirname(__file__), "..", "system")

def w(path, text):
    p = os.path.join(ROOT, path)
    os.makedirs(os.path.dirname(p), exist_ok=True)
    with open(p, "w") as f:
        f.write(text)

# ---------------------------------------------------------------- tokens
C = [
 ("surface",        "#f6f7f8", "#0f1419", "Page and inbox background."),
 ("surface-raised", "#ffffff", "#161d24", "Cards, the thread pane, the Decision card."),
 ("surface-sunken", "#edeff2", "#0b0f13", "Section headers, code blocks, the composer well."),
 ("surface-select", "#e6ecf3", "#1c2733", "The selected row. Never a state colour."),
 ("line",           "#dde1e6", "#26303a", "Hairlines between rows and around cards."),
 ("line-strong",    "#b4bcc6", "#3d4957", "Input borders and control outlines (3:1 on surface)."),
 ("ink",            "#111820", "#e8ecf0", "Primary text on every surface."),
 ("ink-muted",      "#56616d", "#9aa6b2", "Metadata, row summaries, timestamps. 4.5:1 on surface and surface-raised in both themes."),
 ("ink-faint",      "#7d8894", "#6f7b87", "Icons and dividers only, never body text (3:1 on surface)."),
 ("night",          "#14212d", "#24323f", "Primary buttons and the brand slab. Text on it uses on-night."),
 ("on-night",       "#f6f7f8", "#f6f7f8", "Text and icons on night."),
 ("beam",           "#e8960f", "#f5a524", "THE accent: the lamp. Fills only things that need the user (asking, the attention count). Never decorative."),
 ("beam-mark",      "#b86e00", "#f5a524", "The asking icon fill. 4:1 on surface-raised in light, 9:1 in dark."),
 ("beam-ink",       "#8f5300", "#f5b54a", "Beam as text or icon on surface (asking reason, attention counts). 5:1 light, 10:1 dark."),
 ("on-beam",        "#1a1204", "#1a1204", "Text on a beam fill."),
 ("beam-wash",      "#fdf1dc", "#2a2112", "Background of the Decision card header and a focused asking row."),
 ("signal",         "#d93a2b", "#ff6b5b", "Failed state fill (small marks only)."),
 ("signal-ink",     "#b42318", "#ff8a7d", "Failed state text and icon on surface. 6:1 light, 8:1 dark."),
 ("harbour",        "#2f6fb0", "#6fa8e8", "Review state, links and the focus ring. 4.9:1 light, 7.4:1 dark on surface."),
 ("harbour-wash",   "#e7f0fa", "#15253a", "Review highlight, link hover ground."),
 ("calm",           "#2e7d5b", "#5cc49a", "Settled state and passing checks. Used with a check icon, never by hue alone."),
]
tokens = {
  "name": "Fyr", "version": 1,
  "color": {
    "themes": [{"id": "light", "name": "Light"}, {"id": "dark", "name": "Dark"}],
    "tokens": [{"name": n, "value": {"light": l, "dark": d}, "usage": u} for n, l, d, u in C],
  },
  "type": {
    "fonts": [],
    "families": {
      "sans": "\"IBM Plex Sans\", ui-sans-serif, system-ui, -apple-system, \"Segoe UI\", sans-serif",
      "mono": "\"IBM Plex Mono\", ui-monospace, \"SF Mono\", Menlo, Consolas, monospace",
    },
    "groups": [
      {"name": "Interface", "family": "sans", "styles": [
        {"name": "title", "fontSize": "20px", "lineHeight": "28px", "fontWeight": 600, "letterSpacing": "-0.01em", "sample": "Fix map lasso on mobile", "usage": "Thread and pane titles. One per view."},
        {"name": "body", "fontSize": "15px", "lineHeight": "24px", "fontWeight": 400, "sample": "The agent merged the fix and CI is green.", "usage": "Messages, Briefs, Decision questions."},
        {"name": "row", "fontSize": "13px", "lineHeight": "18px", "fontWeight": 500, "sample": "Babysit PR #4182", "usage": "Row titles, buttons, menu items."},
        {"name": "meta", "fontSize": "12px", "lineHeight": "16px", "fontWeight": 400, "sample": "waiting for build slot · 4 min", "usage": "Row summaries, reasons, timestamps, section counts."},
        {"name": "label", "fontSize": "12px", "lineHeight": "16px", "fontWeight": 600, "letterSpacing": "0.04em", "sample": "NEEDS YOU", "usage": "Section headers only, uppercase."},
      ]},
      {"name": "Code", "family": "mono", "styles": [
        {"name": "code", "fontSize": "12px", "lineHeight": "18px", "fontWeight": 400, "sample": "gh pr merge 4182 --squash", "usage": "Commands, paths, hashes, PR numbers, host names."},
      ]},
    ],
  },
  "spacing": {"tokens": [
    {"name": "space-1", "value": "4px", "usage": "Icon to label."},
    {"name": "space-2", "value": "8px", "usage": "Inside chips and badges; between row meta items."},
    {"name": "space-3", "value": "12px", "usage": "Row vertical padding; gap between buttons."},
    {"name": "space-4", "value": "16px", "usage": "Row horizontal padding, card padding, page gutter."},
    {"name": "space-6", "value": "24px", "usage": "Between card sections."},
    {"name": "space-8", "value": "32px", "usage": "Between page regions."},
  ]},
  "radius": {"tokens": [
    {"name": "radius-sm", "value": "3px", "usage": "Badges, chips, kbd keys."},
    {"name": "radius-md", "value": "6px", "usage": "Buttons, inputs, menu items."},
    {"name": "radius-lg", "value": "10px", "usage": "Cards, the palette, toasts."},
  ]},
  "shadow": {"tokens": [
    {"name": "shadow-overlay", "value": {"light": "0 12px 32px rgba(17,24,32,0.14), 0 1px 3px rgba(17,24,32,0.10)", "dark": "0 12px 32px rgba(0,0,0,0.55), 0 1px 3px rgba(0,0,0,0.40)"}, "usage": "Palette, menus and toasts only. Rows and cards have no shadow."},
  ]},
}
w("tokens.json", json.dumps(tokens, indent=2))

# tokens.css as compiled
def css_colors(theme):
    return "\n".join(f"  --{n}: {l if theme=='light' else d};" for n, l, d, _ in C)
sh = tokens["shadow"]["tokens"][0]["value"]
lines = ["/* Fyr — generated from tokens.json */",
         ":root, [data-theme=\"light\"] {", css_colors("light"), f"  --shadow-overlay: {sh['light']};", "}",
         "[data-theme=\"dark\"] {", css_colors("dark"), f"  --shadow-overlay: {sh['dark']};", "}",
         ":root {"]
for fam in ("spacing", "radius"):
    for t in tokens[fam]["tokens"]:
        lines.append(f"  --{t['name']}: {t['value']};")
for k, v in tokens["type"]["families"].items():
    lines.append(f"  --font-{k}: {v};")
lines.append("}")
for g in tokens["type"]["groups"]:
    for s in g["styles"]:
        ls = f" letter-spacing: {s['letterSpacing']};" if "letterSpacing" in s else ""
        lines.append(f".{s['name']} {{ font-family: var(--font-{g['family']}); font-size: {s['fontSize']}; line-height: {s['lineHeight']}; font-weight: {s['fontWeight']};{ls} }}")
w("tokens.css", "\n".join(lines) + "\n")

# ---------------------------------------------------------------- bundle.css
bundle_css = r"""@import url("https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500&family=IBM+Plex+Sans:wght@400;500;600&display=swap");
body { margin: 0; background: var(--surface); color: var(--ink); font-family: var(--font-sans); font-size: 13px; line-height: 18px; -webkit-font-smoothing: antialiased; }
.fy-pad { padding: var(--space-4); display: flex; flex-direction: column; gap: var(--space-3); }
.fy-hstack { display: flex; align-items: center; gap: var(--space-2); flex-wrap: wrap; }
:focus-visible { outline: 2px solid var(--harbour); outline-offset: 2px; }
@media (prefers-reduced-motion: reduce) { * { transition: none !important; animation: none !important; } }

/* Button */
.fy-btn { font: 500 13px/18px var(--font-sans); display: inline-flex; align-items: center; gap: var(--space-2); height: 30px; padding: 0 var(--space-3); border-radius: var(--radius-md); border: 1px solid var(--line-strong); background: var(--surface-raised); color: var(--ink); cursor: pointer; }
.fy-btn:hover { background: var(--surface-sunken); }
.fy-btn-primary { background: var(--night); border-color: var(--night); color: var(--on-night); }
.fy-btn-primary:hover { background: var(--night); filter: brightness(1.15); }
.fy-btn-ghost { border-color: transparent; background: transparent; color: var(--ink-muted); }
.fy-btn-ghost:hover { color: var(--ink); background: var(--surface-sunken); }
.fy-btn-danger { color: var(--signal-ink); }
.fy-btn[disabled] { opacity: .5; cursor: not-allowed; }
.fy-btn .fy-kbd { margin-left: var(--space-1); }
.fy-btn-primary .fy-kbd { background: transparent; color: var(--on-night); border-color: rgba(246,247,248,.35); }

/* Kbd */
.fy-kbd { font: 500 11px/16px var(--font-mono); min-width: 16px; padding: 0 4px; text-align: center; border: 1px solid var(--line-strong); border-bottom-width: 2px; border-radius: var(--radius-sm); color: var(--ink-muted); background: var(--surface-raised); display: inline-block; }

/* Status */
.fy-status { display: inline-flex; align-items: center; gap: var(--space-1); font: 500 12px/16px var(--font-sans); color: var(--ink-muted); }
.fy-icon { width: 14px; height: 14px; flex: none; display: block; }
.fy-s-asking { color: var(--beam-ink); }
.fy-s-failed { color: var(--signal-ink); }
.fy-s-blocked { color: var(--harbour); }
.fy-s-done { color: var(--ink); }
.fy-s-settled { color: var(--calm); }
.fy-s-working, .fy-s-waiting, .fy-s-seen, .fy-s-snoozed { color: var(--ink-muted); }
.fy-s-asking .fy-icon { color: var(--beam-mark); }

/* PR badge */
.fy-pr { display: inline-flex; align-items: center; gap: 4px; height: 20px; padding: 0 6px; border-radius: var(--radius-sm); border: 1px solid var(--line); font: 500 11px/16px var(--font-mono); color: var(--ink-muted); background: var(--surface-raised); white-space: nowrap; }
.fy-pr b { font-weight: 500; color: var(--ink); }
.fy-pr-failed { border-color: var(--signal-ink); color: var(--signal-ink); }
.fy-pr-passing { color: var(--calm); }
.fy-pr-merged { background: var(--surface-sunken); }
.fy-pr-merged b, .fy-pr-closed b { color: var(--ink-muted); }

/* Host chip */
.fy-host { display: inline-flex; align-items: center; gap: 5px; font: 400 11px/16px var(--font-mono); color: var(--ink-muted); white-space: nowrap; }
.fy-host i { width: 6px; height: 6px; border-radius: 50%; background: var(--calm); display: block; }
.fy-host-off i { background: transparent; border: 1px solid var(--ink-faint); width: 4px; height: 4px; }
.fy-host-off { color: var(--ink-faint); }

/* Section header */
.fy-section { display: flex; align-items: center; gap: var(--space-2); height: 30px; padding: 0 var(--space-4); background: var(--surface-sunken); border-block: 1px solid var(--line); color: var(--ink-muted); cursor: pointer; user-select: none; }
.fy-section .label { color: var(--ink-muted); text-transform: uppercase; }
.fy-section .fy-count { font: 500 12px/16px var(--font-mono); color: var(--ink-muted); }
.fy-section-attention .fy-count { color: var(--on-beam); background: var(--beam); border-radius: var(--radius-sm); padding: 0 5px; }
.fy-section .fy-hint { margin-left: auto; font: 400 12px/16px var(--font-sans); color: var(--ink-faint); }
.fy-chev { width: 10px; height: 10px; color: var(--ink-faint); transition: transform .15s; }
.fy-section[aria-expanded="false"] .fy-chev { transform: rotate(-90deg); }

/* Thread row */
.fy-row { display: grid; grid-template-columns: 14px minmax(0,1fr) auto; column-gap: var(--space-3); row-gap: 2px; align-items: start; padding: var(--space-3) var(--space-4); border-bottom: 1px solid var(--line); background: var(--surface-raised); cursor: pointer; }
.fy-row:hover { background: var(--surface-sunken); }
.fy-row[aria-selected="true"] { background: var(--surface-select); }
.fy-row > .fy-icon { margin-top: 2px; grid-row: span 2; }
.fy-row-title { font: 500 13px/18px var(--font-sans); color: var(--ink); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.fy-row-done .fy-row-title { font-weight: 600; }
.fy-row-side { display: flex; align-items: center; gap: var(--space-2); justify-self: end; }
.fy-row-time { font: 400 12px/16px var(--font-sans); color: var(--ink-muted); font-variant-numeric: tabular-nums; }
.fy-row-meta { grid-column: 2 / 4; font: 400 12px/16px var(--font-sans); color: var(--ink-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.fy-row-meta .fy-reason { font-weight: 500; }
.fy-row-dim .fy-row-title { color: var(--ink-muted); font-weight: 500; }
.fy-reason.fy-s-asking { color: var(--beam-ink); }

/* Decision card */
.fy-decision { background: var(--surface-raised); border: 1px solid var(--line); border-radius: var(--radius-lg); overflow: hidden; max-width: 640px; }
.fy-decision-head { display: flex; align-items: center; gap: var(--space-2); padding: var(--space-3) var(--space-4); background: var(--beam-wash); border-bottom: 1px solid var(--line); font: 400 12px/16px var(--font-sans); color: var(--ink-muted); flex-wrap: wrap; }
.fy-decision-head strong { color: var(--beam-ink); font-weight: 600; }
.fy-decision-body { padding: var(--space-4); display: flex; flex-direction: column; gap: var(--space-6); }
.fy-q { display: flex; flex-direction: column; gap: var(--space-2); }
.fy-q-text { font: 500 15px/24px var(--font-sans); color: var(--ink); }
.fy-opt { display: grid; grid-template-columns: 20px minmax(0,1fr); gap: var(--space-3); align-items: start; padding: var(--space-2) var(--space-3); border: 1px solid var(--line); border-radius: var(--radius-md); cursor: pointer; background: var(--surface-raised); text-align: left; font: inherit; color: inherit; }
.fy-opt:hover { border-color: var(--line-strong); }
.fy-opt[aria-checked="true"] { border-color: var(--harbour); background: var(--harbour-wash); }
.fy-opt .fy-kbd { margin-top: 1px; }
.fy-opt-label { font: 500 13px/18px var(--font-sans); color: var(--ink); }
.fy-opt-desc { font: 400 12px/16px var(--font-sans); color: var(--ink-muted); }
.fy-field { font: 400 13px/18px var(--font-sans); color: var(--ink); background: var(--surface); border: 1px solid var(--line-strong); border-radius: var(--radius-md); padding: 7px var(--space-3); width: 100%; box-sizing: border-box; }
.fy-decision-foot { display: flex; align-items: center; gap: var(--space-3); padding: var(--space-3) var(--space-4); border-top: 1px solid var(--line); flex-wrap: wrap; }
.fy-decision-foot .fy-default { font: 400 12px/16px var(--font-sans); color: var(--ink-muted); margin-right: auto; }

/* Toast */
.fy-toast { display: inline-flex; align-items: center; gap: var(--space-3); padding: var(--space-2) var(--space-2) var(--space-2) var(--space-4); background: var(--night); color: var(--on-night); border-radius: var(--radius-lg); box-shadow: var(--shadow-overlay); font: 500 13px/18px var(--font-sans); position: relative; overflow: hidden; }
.fy-toast span { color: var(--on-night); opacity: .7; font-weight: 400; }
.fy-toast .fy-btn { height: 26px; background: transparent; border-color: rgba(246,247,248,.3); color: var(--on-night); }
.fy-toast-timer { position: absolute; left: 0; bottom: 0; height: 2px; background: var(--on-night); opacity: .4; }

/* Palette */
.fy-palette { width: 100%; max-width: 560px; background: var(--surface-raised); border: 1px solid var(--line); border-radius: var(--radius-lg); box-shadow: var(--shadow-overlay); overflow: hidden; }
.fy-palette input { width: 100%; box-sizing: border-box; border: 0; border-bottom: 1px solid var(--line); padding: var(--space-3) var(--space-4); font: 400 15px/24px var(--font-sans); background: transparent; color: var(--ink); outline: none; }
.fy-palette-group { padding: var(--space-2) var(--space-4) 2px; font: 600 11px/16px var(--font-sans); letter-spacing: .04em; text-transform: uppercase; color: var(--ink-faint); }
.fy-palette-item { display: flex; align-items: center; gap: var(--space-3); padding: 7px var(--space-4); font: 500 13px/18px var(--font-sans); color: var(--ink); cursor: pointer; }
.fy-palette-item[aria-selected="true"] { background: var(--surface-select); }
.fy-palette-item .fy-sub { color: var(--ink-muted); font-weight: 400; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
.fy-palette-item .fy-kbd, .fy-palette-item .fy-host { margin-left: auto; }
"""
w("components/bundle.css", bundle_css)

# ---------------------------------------------------------------- bundle.js
bundle_js = r"""/* @ds-bundle: {"format":4,"namespace":"Fyr","components":[{"name":"Button"},{"name":"Kbd"},{"name":"StatusBadge"},{"name":"PRBadge"},{"name":"HostChip"},{"name":"SectionHeader"},{"name":"ThreadRow"},{"name":"DecisionCard"},{"name":"Toast"},{"name":"Palette"}]} */
(function () {
  var React = window.React, h = React.createElement;
  function cx() { return Array.prototype.filter.call(arguments, Boolean).join(" "); }

  var LABELS = { asking: "Asking", failed: "Failed", blocked: "Blocked", done: "Done", working: "Working", waiting: "Waiting", seen: "Seen", snoozed: "Snoozed", settled: "Settled" };
  function svg(children) { return h("svg", { className: "fy-icon", viewBox: "0 0 16 16", "aria-hidden": "true" }, children); }
  var ICONS = {
    asking: function () { return svg([h("circle", { key: 1, cx: 8, cy: 8, r: 6, fill: "currentColor" })]); },
    failed: function () { return svg([h("path", { key: 1, d: "M8 1.8 14.2 8 8 14.2 1.8 8Z", fill: "currentColor" })]); },
    blocked: function () { return svg([h("circle", { key: 1, cx: 8, cy: 8, r: 5.25, fill: "none", stroke: "currentColor", strokeWidth: 1.6 }), h("circle", { key: 2, cx: 8, cy: 8, r: 2, fill: "currentColor" })]); },
    done: function () { return svg([h("circle", { key: 1, cx: 8, cy: 8, r: 3.5, fill: "currentColor" })]); },
    working: function () { return svg([h("circle", { key: 1, cx: 8, cy: 8, r: 5.25, fill: "none", stroke: "currentColor", strokeOpacity: 0.35, strokeWidth: 1.6 }), h("path", { key: 2, d: "M8 2.75a5.25 5.25 0 0 1 5.25 5.25", fill: "none", stroke: "currentColor", strokeWidth: 1.6, strokeLinecap: "round" })]); },
    waiting: function () { return svg([h("circle", { key: 1, cx: 8, cy: 8, r: 5.25, fill: "none", stroke: "currentColor", strokeWidth: 1.6, strokeDasharray: "2.2 2.2" })]); },
    seen: function () { return svg([h("circle", { key: 1, cx: 8, cy: 8, r: 4.5, fill: "none", stroke: "currentColor", strokeWidth: 1.4 })]); },
    snoozed: function () { return svg([h("path", { key: 1, d: "M10.5 2.6a5.6 5.6 0 1 0 3 8.9A4.6 4.6 0 0 1 10.5 2.6Z", fill: "currentColor" })]); },
    settled: function () { return svg([h("path", { key: 1, d: "M3.2 8.4 6.5 11.6 12.8 4.6", fill: "none", stroke: "currentColor", strokeWidth: 1.8, strokeLinecap: "round", strokeLinejoin: "round" })]); }
  };
  function StatusIcon(state) { return (ICONS[state] || ICONS.seen)(); }

  function Kbd(p) { return h("kbd", { className: "fy-kbd" }, p.children); }

  function Button(p) {
    var v = p.variant || "default";
    return h("button", { type: "button", className: cx("fy-btn", v !== "default" && "fy-btn-" + v, p.className), disabled: p.disabled, title: p.disabled && p.reason ? p.reason : undefined, onClick: p.onClick },
      p.children, p.kbd ? h(Kbd, null, p.kbd) : null);
  }

  function StatusBadge(p) {
    var s = p.state || "seen";
    return h("span", { className: cx("fy-status", "fy-s-" + s) }, StatusIcon(s), h("span", null, p.label || LABELS[s]));
  }

  var PR_TEXT = { draft: "draft", open: "open", pending: "checks running", failed: "checks failed", passing: "passing", merged: "merged", closed: "closed" };
  function PRBadge(p) {
    var s = p.state || "open";
    return h("span", { className: cx("fy-pr", "fy-pr-" + s), title: "PR #" + p.number + " " + PR_TEXT[s] },
      h("svg", { className: "fy-icon", viewBox: "0 0 16 16", "aria-hidden": "true", style: { width: 12, height: 12 } },
        h("circle", { cx: 4.5, cy: 3.5, r: 1.6, fill: "none", stroke: "currentColor", strokeWidth: 1.3 }),
        h("circle", { cx: 4.5, cy: 12.5, r: 1.6, fill: "none", stroke: "currentColor", strokeWidth: 1.3 }),
        h("circle", { cx: 11.5, cy: 12.5, r: 1.6, fill: "none", stroke: "currentColor", strokeWidth: 1.3 }),
        h("path", { d: "M4.5 5.1v5.8M11.5 10.9V6.5a2 2 0 0 0-2-2H7.5", fill: "none", stroke: "currentColor", strokeWidth: 1.3 })),
      h("b", null, "#" + p.number), PR_TEXT[s]);
  }

  function HostChip(p) {
    return h("span", { className: cx("fy-host", p.offline && "fy-host-off"), title: p.offline ? p.name + " is offline" : p.name }, h("i"), p.name, p.offline ? " · offline" : null);
  }

  function SectionHeader(p) {
    var open = p.open !== false;
    return h("div", { className: cx("fy-section", "fy-section-" + (p.kind || "plain")), role: "button", tabIndex: 0, "aria-expanded": String(open), onClick: p.onToggle },
      h("svg", { className: "fy-chev", viewBox: "0 0 10 10", "aria-hidden": "true" }, h("path", { d: "M2 3.5 5 6.5 8 3.5", fill: "none", stroke: "currentColor", strokeWidth: 1.5, strokeLinecap: "round" })),
      h("span", { className: "label" }, p.title),
      p.count != null ? h("span", { className: "fy-count" }, p.count) : null,
      p.hint ? h("span", { className: "fy-hint" }, p.hint) : null);
  }

  function ThreadRow(p) {
    var s = p.state || "seen";
    var dim = s === "working" || s === "waiting" || s === "seen" || s === "snoozed" || s === "settled";
    return h("div", { className: cx("fy-row", s === "done" && "fy-row-done", dim && "fy-row-dim"), role: "option", "aria-selected": p.selected ? "true" : "false", tabIndex: 0, onClick: p.onClick },
      h("span", { className: "fy-s-" + s, title: LABELS[s] }, StatusIcon(s)),
      h("div", { className: "fy-row-title" }, p.title),
      h("div", { className: "fy-row-side" },
        p.pr ? h(PRBadge, p.pr) : null,
        p.host ? h(HostChip, { name: p.host, offline: p.offline }) : null,
        p.time ? h("span", { className: "fy-row-time" }, p.time) : null),
      h("div", { className: "fy-row-meta" },
        p.reason ? h("span", { className: cx("fy-reason", "fy-s-" + s) }, p.reason) : null,
        p.reason && p.summary ? " · " : null,
        p.summary || null));
  }

  function DecisionCard(p) {
    var qs = p.questions || [];
    var st = React.useState({}), picked = st[0], setPicked = st[1];
    function toggle(qi, oi, multi) {
      var cur = picked[qi] || [];
      var next = multi ? (cur.indexOf(oi) >= 0 ? cur.filter(function (x) { return x !== oi; }) : cur.concat([oi])) : [oi];
      var o = Object.assign({}, picked); o[qi] = next; setPicked(o);
    }
    var answered = qs.every(function (_, i) { return (picked[i] || []).length > 0; });
    return h("section", { className: "fy-decision", "aria-label": "Decision" },
      h("header", { className: "fy-decision-head" },
        h(StatusBadge, { state: "asking", label: p.kind || "Question" }),
        h("span", null, "·"), h("span", null, p.from),
        p.unblocks ? h("span", null, "· unblocks ", h("strong", null, p.unblocks)) : null,
        p.price ? h("span", null, "· about " + p.price) : null),
      h("div", { className: "fy-decision-body" },
        qs.map(function (q, qi) {
          return h("div", { className: "fy-q", key: qi },
            h("div", { className: "fy-q-text" }, q.text),
            (q.options || []).map(function (o, oi) {
              var on = (picked[qi] || []).indexOf(oi) >= 0;
              return h("button", { key: oi, type: "button", className: "fy-opt", role: q.multi ? "checkbox" : "radio", "aria-checked": on ? "true" : "false", onClick: function () { toggle(qi, oi, q.multi); } },
                h(Kbd, null, String(oi + 1)),
                h("span", null, h("div", { className: "fy-opt-label" }, o.label), o.desc ? h("div", { className: "fy-opt-desc" }, o.desc) : null));
            }));
        }),
        p.freeText !== false ? h("input", { className: "fy-field", placeholder: "Or write your own answer", "aria-label": "Your own answer" }) : null),
      h("footer", { className: "fy-decision-foot" },
        h("span", { className: "fy-default" }, p.defaultText || "No default. Expires in 24 h."),
        h(Button, { variant: "ghost", kbd: "L" }, "Later"),
        h(Button, { variant: "primary", kbd: "⏎", disabled: !answered, reason: "Pick an answer to every question" }, "Answer")));
  }

  function Toast(p) {
    return h("div", { className: "fy-toast", role: "status" },
      h("div", null, p.title, p.detail ? h("span", null, " · " + p.detail) : null),
      p.action ? h(Button, { kbd: "Z" }, p.action) : null,
      h("div", { className: "fy-toast-timer", style: { width: (p.remaining == null ? 60 : p.remaining) + "%" } }));
  }

  function Palette(p) {
    var groups = p.groups || [];
    return h("div", { className: "fy-palette", role: "dialog", "aria-label": "Command palette" },
      h("input", { defaultValue: p.query || "", placeholder: "Go to a thread or run an action", "aria-label": "Search" }),
      h("div", { role: "listbox" }, groups.map(function (g, gi) {
        return h(React.Fragment, { key: gi },
          h("div", { className: "fy-palette-group" }, g.title),
          g.items.map(function (it, ii) {
            return h("div", { key: ii, className: "fy-palette-item", role: "option", "aria-selected": it.selected ? "true" : "false" },
              it.state ? h("span", { className: "fy-s-" + it.state }, StatusIcon(it.state)) : null,
              h("span", null, it.label), it.sub ? h("span", { className: "fy-sub" }, it.sub) : null,
              it.host ? h(HostChip, { name: it.host }) : it.kbd ? h(Kbd, null, it.kbd) : null);
          }));
      })));
  }

  window.Fyr = Object.assign(window.Fyr || {}, { Button: Button, Kbd: Kbd, StatusBadge: StatusBadge, PRBadge: PRBadge, HostChip: HostChip, SectionHeader: SectionHeader, ThreadRow: ThreadRow, DecisionCard: DecisionCard, Toast: Toast, Palette: Palette, LABELS: LABELS });
})();
"""
assert "</script" not in bundle_js and "<!--" not in bundle_js
w("components/bundle.js", bundle_js)

w("components/index.d.ts", """// Fyr components. Props are documentation; the bundle is plain JS on window.Fyr.
export type State = "asking" | "failed" | "blocked" | "done" | "working" | "waiting" | "seen" | "snoozed" | "settled";
export interface ButtonProps { variant?: "default" | "primary" | "ghost" | "danger"; kbd?: string; disabled?: boolean; reason?: string; onClick?: () => void; children: any }
export interface KbdProps { children: string }
export interface StatusBadgeProps { state: State; label?: string }
export interface PRBadgeProps { number: number; state: "draft" | "open" | "pending" | "failed" | "passing" | "merged" | "closed" }
export interface HostChipProps { name: string; offline?: boolean }
export interface SectionHeaderProps { title: string; count?: number; kind?: "attention" | "plain"; open?: boolean; hint?: string; onToggle?: () => void }
export interface ThreadRowProps { title: string; state: State; reason?: string; summary?: string; pr?: PRBadgeProps; host?: string; offline?: boolean; time?: string; selected?: boolean; onClick?: () => void }
export interface DecisionCardProps { kind?: string; from: string; unblocks?: string; price?: string; questions: { text: string; multi?: boolean; options: { label: string; desc?: string }[] }[]; freeText?: boolean; defaultText?: string }
export interface ToastProps { title: string; detail?: string; action?: string; remaining?: number }
export interface PaletteProps { query?: string; groups: { title: string; items: { label: string; sub?: string; state?: State; host?: string; kbd?: string; selected?: boolean }[] }[] }
""")

# ---------------------------------------------------------------- previews + READMEs
def preview(name, group, height, script, subtitle=None):
    sub = f' subtitle="{subtitle}"' if subtitle else ""
    return f"""<!-- @dsCard group="{group}" height={height}{sub} -->
<!doctype html>
<html>
<head><meta charset="utf-8"><title>{name} — preview</title></head>
<body>
<div id="root" class="fy-pad"></div>
<script>
  var F = window.Fyr, h = React.createElement;
  ReactDOM.createRoot(document.getElementById('root')).render({script});
</script>
</body>
</html>
"""

comps = {
 "Button": ("Actions", 72,
   "h('div', {className:'fy-hstack'}, h(F.Button,{variant:'primary',kbd:'\\u23ce'},'Answer'), h(F.Button,{kbd:'E'},'Settle'), h(F.Button,{variant:'ghost',kbd:'S'},'Snooze'), h(F.Button,{variant:'danger'},'Archive'), h(F.Button,{disabled:true,reason:'A run is in progress'},'Settle'))",
   "One action, one label, one shortcut. Use `primary` (night fill) once per view for the action that moves work forward; `default` for the rest; `ghost` in dense rows; `danger` only for destructive actions.",
   ["Show the shortcut with `kbd` on every action that has one; the label and key are the same on every surface.",
    "A disabled button always carries the `reason` the host gave (shown as its tooltip). Never disable silently.",
    "Never colour a button with `beam`: beam marks what needs the user, not what the user can do."]),
 "Kbd": ("Actions", 56,
   "h('div', {className:'fy-hstack'}, h(F.Kbd,null,'J'), h(F.Kbd,null,'K'), h(F.Kbd,null,'\\u2318K'), h(F.Kbd,null,'#'), h(F.Kbd,null,'1'))",
   "A key cap for shortcuts in buttons, menus, the palette and Decision options.",
   ["One key or chord per cap. Use the platform's symbols (⌘ on macOS, Ctrl elsewhere)."]),
 "StatusBadge": ("Status", 96,
   "h('div', {className:'fy-hstack', style:{gap:'16px'}}, ['asking','failed','blocked','done','working','waiting','seen','snoozed','settled'].map(function(s){return h(F.StatusBadge,{key:s,state:s});}))",
   "The one status vocabulary: an icon plus a word, for each of the nine states Attention computes.",
   ["Each state has exactly one icon and one label everywhere: row, thread header, notification, palette.",
    "Shape carries the state, colour only reinforces it: asking is a filled disc in `beam-mark`, failed a diamond in `signal-ink`, blocked a ring with a centre in `harbour`, done a small dot in `ink`.",
    "Working, waiting, seen and snoozed are deliberately grey (`ink-muted`): work in progress must not compete with what needs the user.",
    "Never invent a state in a module. Modules add Signals that map onto these nine."]),
 "PRBadge": ("Status", 72,
   "h('div', {className:'fy-hstack'}, h(F.PRBadge,{number:4182,state:'draft'}), h(F.PRBadge,{number:4182,state:'pending'}), h(F.PRBadge,{number:4182,state:'failed'}), h(F.PRBadge,{number:4182,state:'passing'}), h(F.PRBadge,{number:4182,state:'merged'}))",
   "The state of the pull request a thread serves, shown on every row that has one.",
   ["Show at most two PRs on a row; more collapse to `+n`.",
    "Failed checks get the `signal-ink` outline; everything else stays quiet."]),
 "HostChip": ("Status", 56,
   "h('div', {className:'fy-hstack', style:{gap:'16px'}}, h(F.HostChip,{name:'desk'}), h(F.HostChip,{name:'laptop'}), h(F.HostChip,{name:'build-01',offline:true}))",
   "Which machine a thread resides on, in mono. Offline hosts say so in words.",
   ["Hide the chip when the user has only one host."]),
 "SectionHeader": ("Inbox", 140,
   "h('div', null, h(F.SectionHeader,{title:'Your move',count:4,kind:'attention'}), h(F.SectionHeader,{title:'Moving without you',count:7,open:false,hint:'7 runs \\u00b7 2 waiting for a build slot'}), h(F.SectionHeader,{title:'Snoozed',count:3,open:false}))",
   "The fixed inbox sections: Your move, Finished unread, Finished seen, Moving without you, Waiting to run, Snoozed, Settled, in that order.",
   ["Only the Your move count uses a `beam` fill. Every other count is plain text.",
    "Moving without you is collapsed by default and says what is happening in its hint."]),
 "ThreadRow": ("Inbox", 300,
   "h('div', {role:'listbox', style:{border:'1px solid var(--line)', borderRadius:'10px', overflow:'hidden'}}, h(F.ThreadRow,{state:'asking',title:'Fix map lasso on mobile',reason:'2 questions',summary:'Which gesture should close the sheet?',pr:{number:4182,state:'open'},host:'desk',time:'2 min'}), h(F.ThreadRow,{state:'failed',title:'Speed up e2e suite',reason:'API error \\u00b7 retry',summary:'Run stopped after 14 min',host:'build-01',time:'9 min'}), h(F.ThreadRow,{state:'blocked',title:'Listing cards v2',reason:'CI failed',pr:{number:4177,state:'failed'},host:'desk',time:'21 min',selected:true}), h(F.ThreadRow,{state:'done',title:'Triage sprint issues',summary:'Filed 6 issues, closed 2 duplicates',time:'1 h'}), h(F.ThreadRow,{state:'working',title:'Babysit PR #4170',summary:'Rebasing on main',pr:{number:4170,state:'pending'},host:'laptop',time:'now'}))",
   "One thread in the inbox: state icon, title, the reason it is where it is, PR, host and time.",
   ["The reason is the first thing in the meta line and explains the row's position (\"2 questions\", \"checks failed on head\").",
    "Rows in Working, Seen, Snoozed and Settled are dimmed (title in `ink-muted`); rows that need the user keep full `ink`.",
    "Rows never move under the pointer or keyboard focus; the list reorders when the pointer leaves.",
    "Subagents never get rows; they live inside their parent's thread view."]),
 "DecisionCard": ("Decisions", 520,
   "h(F.DecisionCard,{from:'Fix map lasso on mobile',unblocks:'2 threads',price:'20 s',defaultText:'Default in 2 h: keep the current gesture',questions:[{text:'Which gesture should close the filter sheet?',options:[{label:'Swipe down',desc:'Matches the iOS sheet the app already uses.'},{label:'Tap outside',desc:'Simpler; conflicts with the lasso on small screens.'},{label:'Both',desc:'Most forgiving; two code paths to test.'}]},{text:'Which screens should get the new sheet?',multi:true,options:[{label:'Map'},{label:'List'},{label:'Saved searches'}]}]})",
   "Answers what an agent asks. Shows every question of one ask at once, with keys 1–4 per option and one Answer.",
   ["The header always says who asks, what the answer unblocks and about how long it takes (the Decision's price).",
    "Show the default and when it will be taken, or say that there is none.",
    "`beam-wash` marks the header; the body stays neutral so the options read clearly.",
    "Approvals use the same card: the body shows the exact command, working directory and host that the approval covers."]),
 "Toast": ("Feedback", 72,
   "h(F.Toast,{title:'Settled',detail:'Triage sprint issues',action:'Undo',remaining:62})",
   "Confirms a lifecycle action and offers Undo for 5 seconds.",
   ["Settle, snooze and archive show a toast instead of asking for confirmation.",
    "The `beam` bar shows the time left to undo; it is the only moving element."]),
 "Palette": ("Navigation", 300,
   "h(F.Palette,{query:'lasso',groups:[{title:'Threads',items:[{label:'Fix map lasso on mobile',sub:'2 questions',state:'asking',host:'desk',selected:true},{label:'Lasso polygon perf',sub:'settled yesterday',state:'settled',host:'laptop'}]},{title:'Actions',items:[{label:'Snooze until tomorrow 06:00',kbd:'S'},{label:'New thread with this as context',kbd:'N'}]}]})",
   "⌘K: go to any thread on any host, or run any Action on the selected thread.",
   ["The palette lists the same Actions, labels and keys as the row menu; nothing exists only here."]),
}
for name, (group, height, script, summary, rules) in comps.items():
    w(f"components/{name}/preview.html", preview(name, group, height, script))
    w(f"components/{name}/README.md", f"# {name}\n\n{summary}\n\n" + "\n".join(f"- {r}" for r in rules) + "\n")

# ---------------------------------------------------------------- README
readme = """Fyr is a calm control room for coding agents. Like a lighthouse, it watches everything and shines only when someone must act. The interface stays quiet, neutral and dense; one warm light, `beam`, marks what needs the user. Everything else is slate.

## Voice

- Say what happened and what the user can do: "Checks failed on head", "2 questions", "Settled". No exclamation marks, no emoji, no "Oops".
- Name things by what the user recognises: thread, question, pull request, host. Never internal names (projection, Rule, eseq).
- Times are relative under a day ("9 min", "2 h"), absolute after ("Mon 06:00"), always Europe/Stockholm.
- Sentence case everywhere, except section headers (`label`, uppercase).
- Swedish text keeps å, ä, ö. The name is always written Fyr.

## Colour

- `surface` is the page; `surface-raised` holds cards, rows and the thread pane; `surface-sunken` holds section headers and code. No other backgrounds.
- `ink` for text, `ink-muted` for metadata, `ink-faint` for icons and dividers only.
- `beam` is reserved. It fills the asking icon and the Needs you count, and `beam-wash` marks the Decision card header. Never use beam for buttons, links, brand decoration or charts.
- State colours carry meaning only with their icon: `signal-ink` (failed), `harbour` (blocked, links, focus), `calm` (settled, passing). Working, waiting, seen and snoozed are `ink-muted` on purpose.
- `night` fills the one primary button per view; text on it is `on-night`.
- Every text pair is at least 4.5:1 in both themes; icons and borders at least 3:1. The focus ring is a 2px `harbour` outline with 2px offset.

## Type

- IBM Plex Sans for the interface, IBM Plex Mono for commands, paths, hashes, PR numbers and host names.
- Four sizes only: `meta` and `label` 12px, `row` 13px, `body` 15px, `title` 20px. `code` is 12px mono.
- One `title` per view. Row titles are `row` (500); done rows go to 600, nothing else changes weight.
- Use `font-variant-numeric: tabular-nums` for times and counts.

## Space and shape

- A 4px grid: `space-1` to `space-8`. Rows use `space-3` vertical and `space-4` horizontal padding.
- `radius-sm` for badges and keys, `radius-md` for controls, `radius-lg` for cards, the palette and toasts.
- Rows and cards have hairlines (`line`), not shadows. `shadow-overlay` is only for the palette, menus and toasts.

## Motion and calm

- Nothing moves under the pointer or the keyboard focus. The inbox reorders only when the pointer leaves the list, with a 150 ms move.
- Working shows one static icon per row; the only spinner is in the thread header of a running thread.
- Respect `prefers-reduced-motion`: no movement at all.
- Notifications only when something enters Needs you. Sound is off by default.

## Layout

- Desktop: the inbox (360–420px) on the left, the thread on the right, Decisions inline at the top of the thread.
- Phone (below 640px): one column; the inbox, then the thread; touch targets at least 44px; the Decision card works from a push notification.
- Every row explains its position in its reason text. Order is never a black box.

## Icons

- The nine state icons are part of the bundle (`StatusBadge`); they are geometric so they read at 14px: disc (asking), diamond (failed), ring with centre (blocked), dot (done), quarter arc (working), dashed ring (waiting), open ring (seen), crescent (snoozed), check (settled).
- There is no logo yet; set the name in IBM Plex Sans 600.
"""
w("README.md", readme)

# ---------------------------------------------------------------- Cover
cover = """<!-- @dsCard height=300 -->
<!doctype html>
<html>
<head><meta charset="utf-8"><title>Cover</title>
<style>
  html, body { margin: 0; background: var(--surface); }
  .cv { position: relative; width: 960px; height: 300px; overflow: hidden; font-family: var(--font-sans); }
  .cv svg { position: absolute; left: 480px; top: 0; width: 480px; height: 300px; }
  .night { fill: var(--night); } .beam { fill: var(--beam); } .wash { fill: var(--beam-wash); }
  .harbour { fill: var(--harbour); } .sunk { fill: var(--surface-sunken); }
  .dot { fill: var(--ink-faint); } .dotl { fill: var(--beam); }
  .t { position: absolute; left: 40px; bottom: 40px; width: 420px; }
  .name { font: 600 120px/0.92 var(--font-sans); letter-spacing: -0.03em; color: var(--ink); margin: 0; }
  .tag { font: 400 14px/20px var(--font-sans); color: var(--ink-muted); margin: 16px 0 0; }
</style></head>
<body>
<div class="cv">
  <!-- blocks: night slab 288x300 (the tower), beam 96x96 (the lamp), beam-wash band 192x120, harbour 96x48, surface-sunken 192x72
       arrangement: one tall slab bleeding off top and bottom with the lamp set into its upper edge; satellites stepped to the right
       pattern: precise, mono, a 4px grid -> a dot grid at space-6 (24px) pitch; dots inside the beam-wash band light up in beam (the light falling on the water)
       steps and radii: blocks sized in multiples of space-6; corners radius-sm (3px) on satellites, square on the slab -->
  <svg viewBox="0 0 480 300" aria-hidden="true">
    <rect class="sunk" x="0" y="216" width="192" height="72" rx="3"/>
    <rect class="night" x="192" y="-12" width="168" height="324"/>
    <rect class="beam" x="228" y="36" width="96" height="96" rx="3"/>
    <rect class="wash" x="360" y="60" width="120" height="120" rx="3"/>
    <rect class="harbour" x="360" y="204" width="96" height="48" rx="3"/>
    <g>
      <circle class="dot" cx="24" cy="24" r="1.6"/><circle class="dot" cx="48" cy="24" r="1.6"/><circle class="dot" cx="72" cy="24" r="1.6"/><circle class="dot" cx="96" cy="24" r="1.6"/><circle class="dot" cx="120" cy="24" r="1.6"/><circle class="dot" cx="144" cy="24" r="1.6"/>
      <circle class="dot" cx="24" cy="48" r="1.6"/><circle class="dot" cx="48" cy="48" r="1.6"/><circle class="dot" cx="72" cy="48" r="1.6"/><circle class="dot" cx="96" cy="48" r="1.6"/><circle class="dot" cx="120" cy="48" r="1.6"/><circle class="dot" cx="144" cy="48" r="1.6"/>
      <circle class="dot" cx="24" cy="72" r="1.6"/><circle class="dot" cx="48" cy="72" r="1.6"/><circle class="dot" cx="72" cy="72" r="1.6"/><circle class="dot" cx="96" cy="72" r="1.6"/><circle class="dot" cx="120" cy="72" r="1.6"/><circle class="dot" cx="144" cy="72" r="1.6"/>
      <circle class="dotl" cx="384" cy="84" r="2"/><circle class="dotl" cx="408" cy="84" r="2"/><circle class="dotl" cx="432" cy="84" r="2"/><circle class="dotl" cx="456" cy="84" r="2"/>
      <circle class="dotl" cx="384" cy="108" r="2"/><circle class="dotl" cx="408" cy="108" r="2"/><circle class="dotl" cx="432" cy="108" r="2"/><circle class="dotl" cx="456" cy="108" r="2"/>
      <circle class="dotl" cx="384" cy="132" r="2"/><circle class="dotl" cx="408" cy="132" r="2"/><circle class="dotl" cx="432" cy="132" r="2"/><circle class="dotl" cx="456" cy="132" r="2"/>
      <circle class="dotl" cx="384" cy="156" r="2"/><circle class="dotl" cx="408" cy="156" r="2"/><circle class="dotl" cx="432" cy="156" r="2"/><circle class="dotl" cx="456" cy="156" r="2"/>
    </g>
  </svg>
  <div class="t"><h1 class="name">Fyr</h1><p class="tag">A calm control room for coding agents. It shines only when you must act.</p></div>
</div>
</body>
</html>
"""
w("components/Cover/preview.html", cover)

print("ok")
