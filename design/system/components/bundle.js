/* @ds-bundle: {"format":4,"namespace":"Fyr","components":[{"name":"Button"},{"name":"Kbd"},{"name":"StatusBadge"},{"name":"PRBadge"},{"name":"HostChip"},{"name":"SectionHeader"},{"name":"ThreadRow"},{"name":"DecisionCard"},{"name":"Toast"},{"name":"Palette"}]} */
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
