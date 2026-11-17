#!/usr/bin/env python3
"""Builds the five visual design directions in design/directions/.

Each output file is self-contained HTML + CSS (Google Fonts only). The same
content renders in every direction twice (light, dark), then once more with the
stress data (extreme text lengths, counts, missing PR, 6-line question).
Run: python3 design/generators/make_directions.py
"""
import html
import os

OUT = os.path.join(os.path.dirname(__file__), "..", "directions")
e = html.escape

# ---------------------------------------------------------------- content
LONG140 = ("Investigate why the nightly Playwright run on the staging cluster times out "
           "only when map tiles come from the secondary CDN region again, EU")
LONGPROJ = "acme-platform-infrastructure-terraform-modules-eu-north-1"
BRANCH = "feature/very-long-branch-name-without-any-spaces-that-keeps-going-2026-11-14-final"
URL = "https://github.com/acme/web/actions/runs/12345678901/job/98765432109?check_suite_focus=true"
Q6 = ("The staging deploy for acme/web fails at the database migration step because the new column "
      "listings.lasso_polygon is NOT NULL and 18402 existing rows have no value. I can backfill it from the "
      "saved search areas, which takes about 40 minutes on staging and locks the table for writes, or I can "
      "make the column nullable now and backfill in a background job after the deploy. The second option keeps "
      "the site writable, but the map filter shows no lasso for older listings until the job finishes, "
      "probably 2 to 3 hours. Which do you want?")
assert len(LONG140) == 140, len(LONG140)

NORMAL = dict(
    away=("14:02", "3 done · 2 PRs merged · 1 failed"),
    counts=dict(needs="4", work="4", seen="5", snoozed="3", settled="11"),
    work_hint="1 waiting for a build slot",
    snooze_hint="next: tomorrow 06:00",
    needs=[
        dict(st="a", title="Fix map lasso on mobile", proj="acme/web", pr=("ok", "#1234 ✓"), time="2 min",
             lead="2 questions", meta="Which gesture should close the filter sheet?", sel=True),
        dict(st="f", title="Speed up e2e suite", proj="acme/web", pr=None, time="9 min",
             lead="API error", meta="stopped after 14 min · r to retry"),
        dict(st="b", title="Listing cards v2", proj="acme/web", pr=("fail", "#88 ✗ CI"), time="21 min",
             lead="CI failed", meta="lint: 2 errors in CardGrid.tsx"),
        dict(st="d", title="Triage sprint issues", proj="acme/ops", pr=None, time="1 h",
             lead="Done", meta="Filed 6 issues, closed 2 duplicates"),
    ],
    work=[
        dict(st="w", title="Babysit PR #4170", proj="acme/web", time="3 min", tool="Bash git rebase origin/main", subs="0", key="1"),
        dict(st="w", title="Map perf", proj="acme/web", time="12 min", tool="Bash yarn vitest src/map", subs="2", key="2"),
        dict(st="h", title="Deploy check", proj="acme/infra", time="9 min", tool="waiting on monitor: deploy", subs="0", key="3"),
        dict(st="w", title="Search index", proj="fyr", time="22 min", tool="Read src/index/build.rs", subs="1", key="4"),
    ],
    seen=[
        dict(st="s", title="Lasso polygon perf", proj="acme/web", pr=("merged", "merged"), time="14:20", meta="PR merged"),
        dict(st="s", title="Nix flake update", proj="dotfiles", pr=None, time="13:05", meta="seen"),
    ],
    thread=dict(
        title="Fix map lasso on mobile", proj="acme/web", model="sonnet 5.5", branch="fix/map-lasso-mobile",
        pr=("ok", "#1234 ✓"), st="a", changes="+42 −18", started="13:58", elapsed="9 min",
        convo=[
            ("user", "14:00", "The lasso tool closes the filter sheet on iPhone mini. Fix it and open a PR."),
            ("asst", "14:01", "I traced it to <code>FilterSheet.tsx</code>: the sheet closes on <code>pointerdown</code> on the backdrop, and the lasso starts on the same event."),
            ("tool", "Read", "src/map/FilterSheet.tsx", "0.4s", "c"),
            ("subs", "3", "explore gestures", "1m 40s"),
            ("tool", "Edit", "src/map/FilterSheet.tsx  +42 −18", "1s", "c"),
            ("tool", "Bash", "yarn test src/map/lasso", "12s", "c"),
            ("asst", "14:07", "The lasso no longer closes the sheet, and the tests pass. Before I wire the close gesture, two choices for you."),
        ],
        q=dict(idx="1", total="2", by="explore gestures", text="Which gesture should close the filter sheet?",
               opts=[("1", "Swipe down", "Matches the iOS sheet the app already uses."),
                     ("2", "Tap outside", "Simpler; conflicts with the lasso on small screens."),
                     ("3", "Both", "Most forgiving; two code paths to test."),
                     ("0", "Other…", "")],
               nxt="Which screens get the new sheet?", nxt_kind="multi-select"),
        composer="Answer question 1 in your own words…",
    ),
)

STRESS = dict(
    away=("Mon 06:00", "1234 done · 999 PRs merged · 0 failed · host build-server-eu-north-1-very-long-hostname offline since 05:12"),
    counts=dict(needs="4", work="1234", seen="9999", snoozed="0", settled="9999"),
    work_hint="1233 more · 7 waiting for a build slot on build-server-eu-north-1-very-long-hostname",
    snooze_hint="none",
    needs=[
        dict(st="a", title="Fix", proj="a", pr=None, time="0 s", lead="1 question", meta="Ok?", sel=True),
        dict(st="f", title=LONG140, proj=LONGPROJ, pr=("fail", "#12345 ✗ CI"), time="59 min",
             lead="API error", meta=BRANCH),
        dict(st="b", title=URL, proj="acme/web", pr=("merged", "merged"), time="12 d",
             lead="CI failed", meta=URL),
        dict(st="d", title="Ändra översättningar för sökfältet på kartan så att å, ä och ö visas rätt", proj="acme/web", pr=("ok", "#7 ✓"), time="Mon 06:00",
             lead="Done", meta="Översättningarna är klara och granskade; 0 fel, 1234 strängar."),
    ],
    work=[
        dict(st="w", title="Fix", proj="a", time="0 s", tool="starting", subs="0", key="1"),
        dict(st="w", title=LONG140, proj=LONGPROJ, time="999 h", tool="Bash " + BRANCH, subs="1234", key="9"),
    ],
    seen=[
        dict(st="s", title="x", proj=LONGPROJ, pr=("merged", "merged"), time="Mon 06:00", meta="PR merged"),
        dict(st="s", title=URL, proj="a", pr=None, time="0 s", meta="seen"),
    ],
    thread=dict(
        title=LONG140, proj=LONGPROJ, model="opus 5.5 (1M context)", branch=BRANCH,
        pr=None, st="f", changes="+12345 −9876", started="Mon 06:00", elapsed="999 h",
        convo=[
            ("user", "06:00", "Deploy is red again, see " + URL),
            ("asst", "06:01", "The migration <code>db/migrate/20261114_add_lasso_polygon_to_listings_with_backfill_and_index.rb</code> fails on staging. Branch <code>" + BRANCH + "</code>."),
            ("tool", "Bash", "psql \"$STAGING_DATABASE_URL\" --no-psqlrc --quiet --tuples-only -c 'select count(*) from listings where lasso_polygon is null'", "59m 59s", "f"),
            ("subs", "1234", "explore-every-migration-in-the-repository-and-summarise-the-backfill-strategy", "999h"),
            ("asst", "06:09", "x"),
        ],
        q=dict(idx="1", total="9999", by="explore-every-migration-in-the-repository-and-summarise-the-backfill-strategy", text=Q6,
               opts=[("1", "A", ""),
                     ("2", "Make the column nullable and backfill in a background job", "Keeps writes open. Job log: " + URL),
                     ("3", LONG140, "Short."),
                     ("0", "Other…", "")],
               nxt=LONG140, nxt_kind="multi-select"),
        composer="Answer question 1 in your own words…",
    ),
)

WORD = dict(a="Asking", f="Failed", b="Blocked", d="Done", w="Working", h="Waiting", s="Seen", z="Snoozed", c="Settled")
SCLS = dict(a="s-ask", f="s-fail", b="s-block", d="s-done", w="s-work", h="s-wait", s="s-seen", z="s-snz", c="s-set")

# State icons from screens-v1.html; the knock-out colour follows the row background via --knock.
_ICON = dict(
    a='<circle cx="8" cy="8" r="7" fill="currentColor"/><path d="M6.2 6.1a1.9 1.9 0 1 1 2.6 1.8c-.5.2-.8.6-.8 1.1v.4" fill="none" stroke="var(--knock)" stroke-width="1.5" stroke-linecap="round"/><circle cx="8" cy="11.6" r=".9" fill="var(--knock)"/>',
    f='<path d="M8 .9 15.1 8 8 15.1.9 8Z" fill="currentColor"/><path d="M8 4.6v4.2" stroke="var(--knock)" stroke-width="1.7" stroke-linecap="round"/><circle cx="8" cy="11.2" r=".95" fill="var(--knock)"/>',
    b='<rect x="2" y="2" width="12" height="12" rx="3" fill="none" stroke="currentColor" stroke-width="1.7"/><rect x="5" y="7" width="6" height="2" rx="1" fill="currentColor"/>',
    d='<circle cx="8" cy="8" r="4" fill="currentColor"/>',
    w='<circle cx="8" cy="8" r="5.5" fill="none" stroke="currentColor" stroke-opacity=".3" stroke-width="1.7"/><path d="M8 2.5a5.5 5.5 0 0 1 5.5 5.5" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/>',
    h='<circle cx="8" cy="8" r="5.5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-dasharray="2.2 2.1"/>',
    s='<rect x="4" y="4" width="8" height="8" rx="2" fill="none" stroke="currentColor" stroke-width="1.5"/>',
    z='<path d="M10.5 2.6a5.6 5.6 0 1 0 3 8.9A4.6 4.6 0 0 1 10.5 2.6Z" fill="currentColor"/>',
    c='<path d="M3.2 8.4 6.5 11.6 12.8 4.6" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"/>',
)


def ic(st, size=16, extra=""):
    return (f'<svg class="ic {SCLS[st]} {extra}" width="{size}" height="{size}" viewBox="0 0 16 16" '
            f'aria-label="{WORD[st]}" role="img">{_ICON[st]}</svg>')


def pr(p, cls="pr"):
    if not p:
        return f'<span class="{cls} none" aria-label="no pull request"></span>'
    kind, text = p
    return f'<span class="{cls} {kind}">{e(text)}</span>'


def lead(r):
    return f'<b class="{SCLS[r["st"]]}">{e(r["lead"])}</b>'


# ---------------------------------------------------------------- shared CSS
TOKENS = """
.t-light{--surface:#f6f7f8;--raised:#fff;--sunken:#edeff2;--select:#e6ecf3;--line:#dde1e6;--line-strong:#b4bcc6;
--ink:#111820;--ink-muted:#56616d;--ink-faint:#7d8894;--night:#14212d;--on-night:#f6f7f8;
--beam:#e8960f;--beam-mark:#b86e00;--beam-ink:#8f5300;--on-beam:#1a1204;--beam-wash:#fdf1dc;--beam-icon:#8f5300;
--signal-ink:#b42318;--harbour:#2f6fb0;--harbour-wash:#e7f0fa;--calm:#2e7d5b;--knock:var(--raised);color-scheme:light}
.t-dark{--surface:#0f1419;--raised:#161d24;--sunken:#0b0f13;--select:#1c2733;--line:#26303a;--line-strong:#3d4957;
--ink:#e8ecf0;--ink-muted:#9aa6b2;--ink-faint:#6f7b87;--night:#2a3a4a;--on-night:#f6f7f8;
--beam:#f5a524;--beam-mark:#f5a524;--beam-ink:#f5b54a;--on-beam:#1a1204;--beam-wash:#2a2112;--beam-icon:#f5a524;
--signal-ink:#ff8a7d;--harbour:#6fa8e8;--harbour-wash:#15253a;--calm:#5cc49a;--knock:var(--raised);color-scheme:dark}
"""

BASE = """
*{box-sizing:border-box}
html,body{margin:0;background:#80878f}
body{padding:28px 60px 60px;color:#fff;font-family:var(--ui)}
.doc h1{margin:0;font:600 22px/28px var(--ui)}
.doc p{margin:6px 0 0;font:400 14px/20px var(--ui);max-width:1000px}
.cap{margin:28px 0 8px;font:600 12px/16px var(--ui);letter-spacing:.06em;text-transform:uppercase}
.frame{width:1280px;height:800px;overflow:hidden;border-radius:10px;box-shadow:0 10px 40px rgba(0,0,0,.25);
 background:var(--surface);color:var(--ink);font-family:var(--ui);-webkit-font-smoothing:antialiased;font-size:13px}
.s-ask{color:var(--beam-icon)}.s-fail{color:var(--signal-ink)}.s-block{color:var(--harbour)}.s-done{color:var(--ink)}
.s-work,.s-wait,.s-seen,.s-snz{color:var(--ink-muted)}.s-set{color:var(--calm)}
svg.ic{display:block;flex:none}
.ell{white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-width:0}
.prose{overflow-wrap:anywhere;min-width:0}
.tnum{font-variant-numeric:tabular-nums}
code{font-family:var(--mono);font-size:.92em}
b{font-weight:600}
"""


def page(fname, title, idea, fonts, css, render):
    body = (f'<header class="doc"><h1>{e(title)}</h1><p>{e(idea)}</p></header>'
            f'<div class="cap">Light · 1280 × 800</div><div class="frame t-light">{render(NORMAL)}</div>'
            f'<div class="cap">Dark · 1280 × 800</div><div class="frame t-dark">{render(NORMAL)}</div>'
            f'<div class="cap">Stress test · 3 and 140 character titles, long project, branch and URL, '
            f'0 and 4-digit counts, missing PR, 6-line question</div>'
            f'<div class="frame t-light">{render(STRESS)}</div>'
            f'<div class="cap">Stress test · dark</div><div class="frame t-dark">{render(STRESS)}</div>')
    doc = ('<!doctype html>\n<html lang="en"><head><meta charset="utf-8">'
           '<meta name="viewport" content="width=1400">'
           f'<title>Fyr · {e(title)}</title>'
           '<link rel="preconnect" href="https://fonts.googleapis.com">'
           '<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>'
           f'<link rel="stylesheet" href="https://fonts.googleapis.com/css2?{fonts}&amp;display=swap">'
           f'<style>{TOKENS}{BASE}{css}</style></head><body>{body}</body></html>\n')
    with open(os.path.join(OUT, fname), "w") as f:
        f.write(doc)


# ================================================================ 01 Ledger
LEDGER_CSS = """
:root{--ui:"IBM Plex Sans Condensed",system-ui,sans-serif;--mono:"IBM Plex Mono",ui-monospace,monospace}
.app{display:grid;grid-template-columns:500px minmax(0,1fr);height:100%}
.ib{display:flex;flex-direction:column;min-width:0;min-height:0;background:var(--raised);border-right:1px solid var(--line-strong)}
.top{display:flex;align-items:center;gap:10px;height:44px;padding:0 12px;border-bottom:1px solid var(--line);flex:none}
.brand{font:600 16px/20px var(--ui)}
.host{display:inline-flex;align-items:center;gap:5px;font:400 12px/16px var(--mono);color:var(--ink-muted)}
.host i{width:6px;height:6px;border-radius:50%;background:var(--calm)}
.filter{flex:1;min-width:0;display:flex;align-items:center;gap:8px;height:28px;padding:0 8px;border:1px solid var(--line);border-radius:3px;color:var(--ink-muted);background:var(--surface)}
kbd{font:500 11px/14px var(--mono);padding:0 4px;min-width:16px;text-align:center;border:1px solid var(--line-strong);border-bottom-width:2px;border-radius:3px;color:var(--ink-muted);background:var(--raised);display:inline-block;flex:none}
.filter kbd{margin-left:auto}
.away{display:flex;gap:8px;align-items:center;height:28px;padding:0 12px;background:var(--harbour-wash);border-bottom:1px solid var(--line);font-size:12px;flex:none;min-width:0}
.away .x{margin-left:auto;color:var(--ink-muted);flex:none}
/* the ledger grid: fixed columns shared by the column header and every row */
.grid{display:grid;grid-template-columns:16px minmax(0,1fr) 100px 84px 64px;column-gap:8px;padding:0 12px;align-items:center}
.rules{background-image:linear-gradient(var(--line),var(--line)),linear-gradient(var(--line),var(--line)),linear-gradient(var(--line),var(--line));
 background-size:1px 100%;background-repeat:no-repeat;background-position:219px 0,327px 0,419px 0}
.colh{height:24px;font:600 11px/14px var(--ui);letter-spacing:.06em;text-transform:uppercase;color:var(--ink-muted);border-bottom:1px solid var(--line-strong);flex:none}
.colh .r,.age{text-align:right}
.list{flex:1;min-height:0;overflow:hidden}
.sec{display:flex;align-items:center;gap:8px;height:26px;padding:0 12px;background:var(--sunken);border-bottom:1px solid var(--line);font:600 11px/14px var(--ui);letter-spacing:.06em;text-transform:uppercase;color:var(--ink-muted)}
.sec .n{font:500 11px/16px var(--mono);letter-spacing:0;font-variant-numeric:tabular-nums}
.sec.att .n{background:var(--beam);color:var(--on-beam);border-radius:2px;padding:0 4px}
.sec .h{margin-left:auto;text-transform:none;letter-spacing:0;font-weight:400;font-size:12px;min-width:0;max-width:60%}
.sec .fold{font:400 11px/1 var(--mono)}
.rw{height:30px;border-bottom:1px solid var(--line)}
.rw.two{height:48px;grid-template-rows:20px 16px;row-gap:2px;align-content:center}
.rw.sel{background-color:var(--select);--knock:var(--select);box-shadow:inset 3px 0 0 var(--ink)}
.rw .t{font:500 13px/20px var(--ui)}
.rw.dn .t{font-weight:600}
.rw .p{font:400 12px/16px var(--mono);color:var(--ink-muted)}
.rw .m{grid-column:2/6;font:400 12px/16px var(--ui);color:var(--ink-muted)}
.rw .tool{font:400 12px/16px var(--mono);color:var(--ink-muted);margin-left:8px}
.age{font:400 12px/16px var(--mono);color:var(--ink-muted);font-variant-numeric:tabular-nums;white-space:nowrap;overflow:hidden}
.rw.work .t{color:var(--ink-muted)}
.pr{font:500 11px/16px var(--mono);color:var(--ink-muted);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;font-variant-numeric:tabular-nums}
.pr.ok{color:var(--calm)}.pr.fail{color:var(--signal-ink)}.pr.merged{color:var(--ink-muted)}
.subs{font:400 12px/16px var(--mono);color:var(--ink-muted);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
/* thread */
.th{display:flex;flex-direction:column;min-width:0;min-height:0}
.thh{background:var(--raised);border-bottom:1px solid var(--line);padding:10px 20px 0;flex:none}
.crumb{display:flex;gap:6px;font:400 12px/16px var(--mono);color:var(--ink-muted);min-width:0}
.crumb span{white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-width:0}
.crumb .pj{flex:0 1 auto;max-width:45%}.crumb .br{flex:1 1 0}
.ttl{display:flex;gap:12px;align-items:flex-start;margin-top:4px}
.tt{flex:1;min-width:0;font:600 19px/24px var(--ui);display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere}
.acts{display:flex;gap:6px;flex:none;align-items:center}
.btn{display:inline-flex;align-items:center;gap:6px;height:28px;padding:0 10px;border-radius:3px;border:1px solid var(--line-strong);background:var(--raised);color:var(--ink);font:500 13px/18px var(--ui);white-space:nowrap}
.btn[disabled]{opacity:.5}
.btn.p{background:var(--night);border-color:var(--night);color:var(--on-night)}
.btn.p kbd{background:transparent;color:var(--on-night);border-color:rgba(246,247,248,.4)}
.facts{display:grid;grid-template-columns:110px 150px 110px minmax(0,1fr) 90px;margin-top:10px;border-top:1px solid var(--line)}
.facts>div{padding:5px 10px 6px;border-right:1px solid var(--line);min-width:0}
.facts>div:first-child{padding-left:0}.facts>div:last-child{border-right:0}
.facts .k{display:block;font:600 10px/14px var(--ui);letter-spacing:.06em;text-transform:uppercase;color:var(--ink-muted)}
.facts .v{display:flex;gap:6px;align-items:center;font:500 13px/18px var(--ui);min-width:0}
.facts .v.mono{font:400 12px/18px var(--mono)}
.tabs{display:flex;gap:18px;border-top:1px solid var(--line)}
.tabs span{padding:6px 0;font:500 13px/18px var(--ui);color:var(--ink-muted);border-bottom:2px solid transparent;white-space:nowrap}
.tabs span.on{color:var(--ink);border-color:var(--ink)}
.tabs em{font:400 11px/16px var(--mono);font-style:normal;margin-left:6px}
.convo{flex:1;min-height:0;overflow:hidden;display:flex;flex-direction:column;justify-content:flex-end;padding:8px 20px}
.ent{display:grid;grid-template-columns:56px minmax(0,1fr);column-gap:12px;padding:8px 0;border-bottom:1px solid var(--line)}
.ent .g{font:400 11px/16px var(--mono);color:var(--ink-muted);padding-top:3px}
.ent .g b{display:block;font:600 11px/16px var(--ui);letter-spacing:.04em;text-transform:uppercase}
.ent .tx{font:400 15px/23px var(--ui);max-width:640px}
.tools{display:flex;flex-direction:column}
.tl{display:grid;grid-template-columns:16px 52px minmax(0,1fr) 64px;column-gap:8px;align-items:center;height:24px;font:400 12px/16px var(--mono);color:var(--ink);border-top:1px dashed var(--line)}
.tl:first-child{border-top:0}
.tl .nm{font-weight:500}
.tl .d{text-align:right;color:var(--ink-muted);font-variant-numeric:tabular-nums;white-space:nowrap;overflow:hidden}
.tl .s{font-family:var(--ui);font-size:13px;color:var(--ink-muted)}
.dock{flex:none;margin:0 20px;border:1px solid var(--line-strong);border-bottom:0;background:var(--raised);max-height:52%;display:flex;flex-direction:column;min-height:0}
.dh{display:flex;gap:8px;align-items:center;height:28px;padding:0 10px;background:var(--beam-wash);border-bottom:1px solid var(--line);font-size:12px;color:var(--ink-muted);min-width:0;flex:none;--knock:var(--beam-wash)}
.dh b{color:var(--beam-ink);flex:none}
.dh .by{min-width:0}
.dh .ml{margin-left:auto;flex:none}
.qb{padding:8px 10px 6px;overflow:hidden;min-height:0;display:flex;flex-direction:column}
.q{font:500 15px/22px var(--ui);overflow-wrap:anywhere;margin-bottom:6px;overflow:auto;min-height:0;flex:0 1 auto}
.opt{display:grid;grid-template-columns:24px minmax(0,200px) minmax(0,1fr);column-gap:10px;align-items:center;height:30px;border-top:1px solid var(--line);flex:none}
.opt.on{background:var(--harbour-wash);box-shadow:inset 2px 0 0 var(--harbour)}
.opt kbd{margin-left:4px}
.opt .a{font:500 13px/18px var(--ui)}
.opt .d{font:400 12px/16px var(--ui);color:var(--ink-muted)}
.nxt{display:flex;gap:8px;align-items:center;height:26px;padding:0 10px;border-top:1px solid var(--line);font-size:12px;color:var(--ink-muted);flex:none;min-width:0}
.nxt .ml{margin-left:auto;flex:none}
.cmp{flex:none;margin:0 20px 14px;border:1px solid var(--line-strong);background:var(--raised);display:flex;align-items:center;gap:10px;height:40px;padding:0 6px 0 12px}
.cmp .in{flex:1;min-width:0;font:400 14px/20px var(--ui);color:var(--ink-muted)}
.cmp .hint{font-size:12px;color:var(--ink-muted);white-space:nowrap}
"""


def ledger(D):
    c = D["counts"]
    T = D["thread"]

    def sec(name, n, att=False, hint="", fold=False):
        f = '<span class="fold">▸</span>' if fold else ""
        h = f'<span class="h ell">{e(hint)}</span>' if hint else ""
        return f'<div class="sec{" att" if att else ""}">{f}{e(name)} <span class="n">{e(n)}</span>{h}</div>'

    rows = []
    for r in D["needs"]:
        cls = "rw two grid rules" + (" sel" if r.get("sel") else "") + (" dn" if r["st"] == "d" else "")
        rows.append(f'<div class="{cls}">{ic(r["st"])}<div class="t ell" title="{e(r["title"])}">{e(r["title"])}</div>'
                    f'<div class="p ell">{e(r["proj"])}</div>{pr(r["pr"])}<div class="age">{e(r["time"])}</div>'
                    f'<div class="m ell">{lead(r)} · {e(r["meta"])}</div></div>')
    work = []
    for r in D["work"]:
        work.append(f'<div class="rw grid rules work">{ic(r["st"])}<div class="t ell">{e(r["title"])}<span class="tool">{e(r["tool"])}</span></div>'
                    f'<div class="p ell">{e(r["proj"])}</div><div class="subs">{e(r["subs"])} sub</div><div class="age">{e(r["time"])}</div></div>')
    seen = []
    for r in D["seen"]:
        seen.append(f'<div class="rw grid rules work">{ic(r["st"])}<div class="t ell">{e(r["title"])}</div>'
                    f'<div class="p ell">{e(r["proj"])}</div>{pr(r["pr"])}<div class="age">{e(r["time"])}</div></div>')
    away = D["away"]
    inbox = (f'<aside class="ib"><div class="top"><span class="brand">Fyr</span><span class="host"><i></i>desk</span>'
             f'<div class="filter">Filter threads<kbd>/</kbd></div><kbd>⌘K</kbd></div>'
             f'<div class="away"><span class="ell">Since <b>{e(away[0])}</b>: {e(away[1])}</span><span class="x">Dismiss</span></div>'
             f'<div class="colh grid"><span></span><span>Thread</span><span>Project</span><span>PR</span><span class="r">Age</span></div>'
             f'<div class="list">{sec("Needs you", c["needs"], True)}{"".join(rows)}'
             f'{sec("Working", c["work"], hint=D["work_hint"])}{"".join(work)}'
             f'{sec("Seen", c["seen"], hint="⇧E settle")}{"".join(seen)}'
             f'{sec("Snoozed", c["snoozed"], hint=D["snooze_hint"], fold=True)}{sec("Settled", c["settled"], hint="last 24 h", fold=True)}</div></aside>')

    convo = []
    tools = []

    def flush():
        if tools:
            convo.append(f'<div class="ent"><div class="g"><b>Tools</b>{len(tools)}</div><div class="tools">{"".join(tools)}</div></div>')
            tools.clear()
    for it in T["convo"]:
        if it[0] == "tool":
            _, nm, arg, dur, st = it
            tools.append(f'<div class="tl">{ic(st, 14)}<span class="nm">{e(nm)}</span><span class="ell">{e(arg)}</span><span class="d">{e(dur)}</span></div>')
        elif it[0] == "subs":
            _, n, lbl, dur = it
            tools.append(f'<div class="tl">{ic("c", 14)}<span class="nm">Task</span><span class="s ell">{e(n)} subagents · {e(lbl)} ▸</span><span class="d">{e(dur)}</span></div>')
        else:
            flush()
            who = "You" if it[0] == "user" else "Claude"
            txt = e(it[2]) if it[0] == "user" else it[2]
            convo.append(f'<div class="ent"><div class="g"><b>{who}</b>{e(it[1])}</div><div class="tx prose">{txt}</div></div>')
    flush()
    q = T["q"]
    opts = "".join(f'<div class="opt{" on" if k == "1" else ""}"><kbd>{k}</kbd><span class="a ell">{e(a)}</span><span class="d ell">{e(d)}</span></div>'
                   for k, a, d in q["opts"])
    stc = SCLS[T["st"]]
    thread = (f'<section class="th"><div class="thh"><div class="crumb"><span class="pj">{e(T["proj"])}</span><span>/</span><span class="br">{e(T["branch"])}</span></div>'
              f'<div class="ttl"><div class="tt" title="{e(T["title"])}">{e(T["title"])}</div>'
              f'<div class="acts"><button class="btn">Snooze <kbd>s</kbd></button><button class="btn" disabled>Settle <kbd>e</kbd></button></div></div>'
              f'<div class="facts"><div><span class="k">State</span><span class="v {stc}">{ic(T["st"], 14)}{WORD[T["st"]]}</span></div>'
              f'<div><span class="k">Model</span><span class="v mono"><span class="ell">{e(T["model"])}</span></span></div>'
              f'<div><span class="k">PR</span><span class="v mono">{pr(T["pr"]) if T["pr"] else "none"}</span></div>'
              f'<div><span class="k">Branch</span><span class="v mono"><span class="ell">{e(T["branch"])}</span></span></div>'
              f'<div><span class="k">Elapsed</span><span class="v mono tnum">{e(T["elapsed"])}</span></div></div>'
              f'<div class="tabs"><span class="on">Conversation</span><span>Changes<em>{e(T["changes"])}</em></span><span>PR<em>{e(T["pr"][1]) if T["pr"] else "none"}</em></span></div></div>'
              f'<div class="convo">{"".join(convo)}</div>'
              f'<div class="dock"><div class="dh">{ic("a", 14)}<b>Question {e(q["idx"])} of {e(q["total"])}</b><span class="by ell">asked by subagent “{e(q["by"])}”</span><span class="ml">no default</span></div>'
              f'<div class="qb"><div class="q">{e(q["text"])}</div>{opts}</div>'
              f'<div class="nxt"><kbd>Tab</kbd><span class="ell">Next: {e(q["nxt"])}</span><span class="ml">{e(q["nxt_kind"])}</span></div></div>'
              f'<div class="cmp"><span class="in ell">{e(T["composer"])}</span><span class="hint">⏎ answers · ⌘⏎ queues</span><button class="btn p">Answer <kbd>⏎</kbd></button></div></section>')
    return f'<div class="app">{inbox}{thread}</div>'


# ================================================================ 02 Console
CONSOLE_CSS = """
:root{--ui:"JetBrains Mono",ui-monospace,monospace;--mono:"JetBrains Mono",ui-monospace,monospace;--prose:"Inter",system-ui,sans-serif}
.t-light{--surface:#fbfbfa;--raised:#fbfbfa;--knock:#fbfbfa}
.t-dark{--surface:#0c1014;--raised:#0c1014;--knock:#0c1014;--sunken:#11171d}
.frame{font-size:12px;line-height:20px}
.app{display:grid;grid-template-columns:432px minmax(0,1fr);grid-template-rows:minmax(0,1fr) 24px;height:100%}
.ib{display:flex;flex-direction:column;min-width:0;min-height:0;border-right:1px solid var(--line-strong)}
.top{display:flex;gap:12px;align-items:center;height:32px;padding:0 12px;border-bottom:1px solid var(--line);flex:none;min-width:0}
.top .brand{font-weight:700}
.top .path{color:var(--ink-muted)}
.top .ml{margin-left:auto;color:var(--ink-muted);display:flex;gap:12px;flex:none}
.k{color:var(--ink);font-weight:500}
.k::before{content:"[";color:var(--ink-faint)}.k::after{content:"]";color:var(--ink-faint)}
.away{height:24px;display:flex;gap:8px;padding:0 12px;align-items:center;color:var(--ink);border-bottom:1px dashed var(--line-strong);flex:none;min-width:0}
.away .tag{flex:none;color:var(--harbour)}
.list{flex:1;min-height:0;overflow:hidden;padding-bottom:8px}
.sec{display:flex;align-items:center;gap:8px;height:28px;padding:4px 12px 0;color:var(--ink-muted);font-weight:500;letter-spacing:.04em;text-transform:uppercase;font-size:11px;min-width:0}
.sec::after{content:"";order:2;flex:1;min-width:16px;border-top:1px solid var(--line-strong)}
.sec .n{order:3;font-variant-numeric:tabular-nums;letter-spacing:0;color:var(--ink)}
.sec.att .n{background:var(--beam);color:var(--on-beam);padding:0 5px;font-weight:700}
.sec .h{order:4;text-transform:none;letter-spacing:0;font-weight:400;max-width:55%}
.rw{display:grid;grid-template-columns:16px minmax(0,1fr) auto 72px;column-gap:10px;padding:2px 12px 2px 12px;align-items:center;position:relative}
.rw.two{grid-template-rows:20px 20px;height:46px}
.rw.one{height:24px}
.rw.sel{background:var(--select);--knock:var(--select)}
.rw.sel::before{content:"";position:absolute;left:0;top:0;bottom:0;width:4px;background:var(--ink)}
.rw .t{font-weight:500;color:var(--ink)}
.rw.dn .t{font-weight:700}
.rw .m{grid-column:2/5;color:var(--ink-muted)}
.rw .m{display:flex;gap:6px;min-width:0;white-space:nowrap}.rw .m b{flex:none}.rw .m i{font-style:normal;flex:none}
.rw .m .pj{color:var(--ink);flex:0 1 auto;max-width:45%}.rw .m .mt{flex:1 1 0}
.rw.one .t{font-weight:400;color:var(--ink-muted)}
.rw.one .t i{font-style:normal;color:var(--ink-faint);margin-left:10px}
.age{text-align:right;color:var(--ink-muted);font-variant-numeric:tabular-nums;white-space:nowrap;overflow:hidden}
.pr{white-space:nowrap;max-width:120px;overflow:hidden;text-overflow:ellipsis;color:var(--ink-muted)}
.pr.ok{color:var(--calm)}.pr.fail{color:var(--signal-ink)}
.pr.none{width:0}
.mode{grid-column:1/3;display:flex;align-items:stretch;height:24px;background:var(--sunken);border-top:1px solid var(--line-strong);color:var(--ink-muted);min-width:0;white-space:nowrap}
.mode>span{display:flex;align-items:center;padding:0 10px;border-right:1px solid var(--line-strong);min-width:0}
.mode .m0{background:var(--night);color:var(--on-night);font-weight:700;letter-spacing:.06em;flex:none}
.mode .grow{flex:1;overflow:hidden}
.mode .grow span{overflow:hidden;text-overflow:ellipsis}
.mode .keys{flex:none;gap:10px}
.mode .host{flex:none;gap:6px}.mode .host i{width:6px;height:6px;background:var(--calm)}
.th{display:flex;flex-direction:column;min-width:0;min-height:0}
.thh{padding:8px 20px 0;border-bottom:1px solid var(--line);flex:none}
.tt{font-weight:700;font-size:15px;line-height:22px;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere}
.tt::before{content:"# ";color:var(--ink-faint)}
.kv{display:flex;gap:16px;color:var(--ink-muted);min-width:0;margin-top:2px}
.kv span{white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-width:0;flex:0 1 auto}
.kv .st{display:inline-flex;gap:6px;align-items:center;flex:none}
.kv .brn{flex:1 1 0}
.kv i{font-style:normal;color:var(--ink-faint)}
.tabs{display:flex;gap:0;margin-top:6px}
.tabs span{padding:2px 12px;color:var(--ink-muted);border:1px solid transparent;border-bottom:0;white-space:nowrap}
.tabs span.on{color:var(--ink);border-color:var(--line-strong);background:var(--surface);margin-bottom:-1px;font-weight:500}
.convo{flex:1;min-height:0;overflow:hidden;display:flex;flex-direction:column;justify-content:flex-end;padding:8px 20px;gap:10px}
.u{display:grid;grid-template-columns:16px minmax(0,1fr);gap:8px;color:var(--ink);background:var(--sunken);padding:6px 10px;border-left:2px solid var(--line-strong)}
.u::before{content:"›";color:var(--ink-muted);font-weight:700}
.a{font:400 14px/22px var(--prose);max-width:680px;padding-left:24px;position:relative}
.a::before{content:"";position:absolute;left:4px;top:8px;width:7px;height:7px;border-radius:50%;background:var(--ink)}
.tool{display:grid;grid-template-columns:16px 40px minmax(0,1fr) 64px;column-gap:8px;align-items:center;height:20px;color:var(--ink-muted);padding-left:0}
.tool .nm{color:var(--ink);font-weight:500}
.tool .d{text-align:right;font-variant-numeric:tabular-nums;white-space:nowrap;overflow:hidden}
.tool .arg::before{content:"(";color:var(--ink-faint)}.tool .arg::after{content:")";color:var(--ink-faint)}
.box{flex:none;margin:12px 20px 0;border:1px solid var(--line-strong);display:flex;flex-direction:column;max-height:54%;min-height:0;position:relative}
.box .lg{position:absolute;top:-11px;left:10px;padding:0 6px;background:var(--surface);display:flex;gap:6px;align-items:center;max-width:calc(100% - 20px);--knock:var(--surface)}
.box .lg b{color:var(--beam-ink);flex:none}
.box .lg span{min-width:0}
.qb{padding:12px 12px 6px;overflow:hidden;display:flex;flex-direction:column;min-height:0}
.q{font:500 14px/21px var(--prose);overflow-wrap:anywhere;margin-bottom:8px;overflow:auto;min-height:0;flex:0 1 auto}
.opt{display:grid;grid-template-columns:28px minmax(0,26ch) minmax(0,1fr);column-gap:10px;height:22px;align-items:center;flex:none}
.opt.on{background:var(--harbour-wash);outline:1px solid var(--harbour)}
.opt .a2{font-weight:500}
.opt .d{color:var(--ink-muted)}
.nxt{display:flex;gap:8px;height:24px;align-items:center;padding:0 12px;border-top:1px dashed var(--line-strong);color:var(--ink-muted);flex:none;min-width:0}
.nxt .ml{margin-left:auto;flex:none}
.cmp{flex:none;margin:8px 20px 10px;border:1px solid var(--line-strong);height:34px;display:flex;align-items:center;gap:8px;padding:0 10px}
.cmp .pt{color:var(--ink);font-weight:700}
.cmp .cur{width:8px;height:16px;background:var(--ink);flex:none}
.cmp .in{flex:1;min-width:0;color:var(--ink-faint)}
.cmp .hint{color:var(--ink-muted);flex:none}
"""


def console(D):
    c = D["counts"]
    T = D["thread"]

    def sec(name, n, att=False, hint=""):
        h = f'<span class="h ell">{e(hint)}</span>' if hint else ""
        return f'<div class="sec{" att" if att else ""}"><span>{e(name)}</span><span class="n">{e(n)}</span>{h}</div>'

    needs = "".join(
        f'<div class="rw two{" sel" if r.get("sel") else ""}{" dn" if r["st"] == "d" else ""}">{ic(r["st"])}'
        f'<span class="t ell">{e(r["title"])}</span>{pr(r["pr"])}<span class="age">{e(r["time"])}</span>'
        f'<span class="m">{lead(r)}<i>·</i><span class="pj ell">{e(r["proj"])}</span><i>·</i><span class="mt ell">{e(r["meta"])}</span></span></div>' for r in D["needs"])
    work = "".join(
        f'<div class="rw one">{ic(r["st"])}<span class="t ell">{e(r["title"])}<i>{e(r["tool"])}</i></span>'
        f'<span class="k">⌘{e(r["key"])}</span><span class="age">{e(r["time"])}</span></div>' for r in D["work"])
    seen = "".join(
        f'<div class="rw one">{ic(r["st"])}<span class="t ell">{e(r["title"])}<i>{e(r["proj"])}</i></span>'
        f'{pr(r["pr"])}<span class="age">{e(r["time"])}</span></div>' for r in D["seen"])
    inbox = (f'<aside class="ib"><div class="top"><span class="brand">fyr</span><span class="path">~/inbox</span>'
             f'<span class="ml"><span><span class="k">/</span> filter</span><span><span class="k">⌘K</span></span></span></div>'
             f'<div class="away"><span class="tag">since {e(D["away"][0])}</span><span class="ell">{e(D["away"][1])}</span></div>'
             f'<div class="list">{sec("Needs you", c["needs"], True)}{needs}{sec("Working", c["work"], hint=D["work_hint"])}{work}'
             f'{sec("Seen", c["seen"], hint="⇧E settle")}{seen}{sec("Snoozed", c["snoozed"], hint=D["snooze_hint"])}{sec("Settled", c["settled"], hint="24 h")}</div></aside>')
    items = []
    for it in T["convo"]:
        if it[0] == "user":
            items.append(f'<div class="u"><span class="prose">{e(it[2])}</span></div>')
        elif it[0] == "asst":
            items.append(f'<div class="a prose">{it[2]}</div>')
        elif it[0] == "tool":
            _, nm, arg, dur, st = it
            items.append(f'<div class="tool">{ic(st, 12)}<span class="nm">{e(nm)}</span><span class="arg ell">{e(arg)}</span><span class="d">{e(dur)}</span></div>')
        else:
            _, n, lbl, dur = it
            items.append(f'<div class="tool">{ic("c", 12)}<span class="nm">Task</span><span class="ell">{e(n)} subagents · {e(lbl)} ▸</span><span class="d">{e(dur)}</span></div>')
    q = T["q"]
    opts = "".join(f'<div class="opt{" on" if k == "1" else ""}"><span class="k">{k}</span><span class="a2 ell">{e(a)}</span><span class="d ell">{e(d)}</span></div>' for k, a, d in q["opts"])
    thread = (f'<section class="th"><div class="thh"><div class="tt" title="{e(T["title"])}">{e(T["title"])}</div>'
              f'<div class="kv"><span class="st {SCLS[T["st"]]}">{ic(T["st"], 12)}{WORD[T["st"]].lower()}</span><span>{e(T["proj"])}</span>'
              f'<span class="brn"><i>on </i>{e(T["branch"])}</span><span>{e(T["model"])}</span>{pr(T["pr"]) if T["pr"] else "<span>no PR</span>"}</div>'
              f'<div class="tabs"><span class="on">conversation</span><span>changes {e(T["changes"])}</span><span>pr</span></div></div>'
              f'<div class="convo">{"".join(items)}</div>'
              f'<div class="box"><div class="lg">{ic("a", 12)}<b>Q{e(q["idx"])}/{e(q["total"])}</b><span class="ell">asked by {e(q["by"])}</span></div>'
              f'<div class="qb"><div class="q">{e(q["text"])}</div>{opts}</div>'
              f'<div class="nxt"><span class="k">tab</span><span class="ell">{e(q["nxt"])}</span><span class="ml">{e(q["nxt_kind"])}</span></div></div>'
              f'<div class="cmp"><span class="pt">›</span><span class="cur"></span><span class="in ell">{e(T["composer"])}</span><span class="hint">⏎ answer · ⌘⏎ queue</span></div></section>')
    mode = (f'<div class="mode"><span class="m0">ANSWER</span><span class="grow"><span>inbox › {e(T["title"])}</span></span>'
            f'<span class="keys"><span><span class="k">1-3</span> pick</span><span><span class="k">0</span> other</span><span><span class="k">n</span> next needing you</span><span><span class="k">s</span> snooze</span></span>'
            f'<span class="host"><i></i>desk</span><span class="tnum">14:07</span></div>')
    return f'<div class="app">{inbox}{thread}{mode}</div>'


# ================================================================ 03 Reader
READER_CSS = """
:root{--ui:"Inter",system-ui,sans-serif;--serif:"Newsreader",Georgia,serif;--mono:"IBM Plex Mono",ui-monospace,monospace}
.t-light{--surface:#fbfaf8;--raised:#fff;--knock:#fff}
.app{display:grid;grid-template-columns:384px minmax(0,1fr);height:100%}
.ib{display:flex;flex-direction:column;min-width:0;min-height:0;background:var(--surface);border-right:1px solid var(--line)}
.top{display:flex;align-items:center;gap:10px;height:48px;padding:0 16px;flex:none}
.brand{font:600 15px/20px var(--ui);letter-spacing:-.01em}
.host{font:400 12px/16px var(--mono);color:var(--ink-muted);display:flex;gap:5px;align-items:center}
.host i{width:6px;height:6px;border-radius:50%;background:var(--calm)}
.top .ml{margin-left:auto}
kbd{font:500 11px/14px var(--ui);padding:1px 5px;min-width:18px;text-align:center;border-radius:4px;background:var(--sunken);color:var(--ink-muted);display:inline-block;flex:none}
.away{margin:0 12px 6px;padding:6px 10px;border-radius:6px;background:var(--harbour-wash);font:400 12px/16px var(--ui);flex:none;display:flex;gap:6px;min-width:0}
.list{flex:1;min-height:0;overflow:hidden;padding:0 8px}
.sec{display:flex;align-items:baseline;gap:8px;padding:12px 8px 4px;font:600 11px/16px var(--ui);letter-spacing:.06em;text-transform:uppercase;color:var(--ink-muted);min-width:0}
.sec .n{font-variant-numeric:tabular-nums;letter-spacing:0}
.sec.att .n{background:var(--beam);color:var(--on-beam);border-radius:8px;padding:0 6px}
.sec .h{margin-left:auto;text-transform:none;letter-spacing:0;font-weight:400;min-width:0;max-width:60%}
.card{display:grid;grid-template-columns:16px minmax(0,1fr) auto;column-gap:10px;grid-template-rows:20px 22px 16px;height:74px;padding:8px 10px;border-radius:8px;background:var(--raised);border:1px solid var(--line);margin-bottom:4px;align-content:center}
.card.sel{border-color:var(--ink-muted);box-shadow:0 0 0 1px var(--ink-muted)}
.card .ic{margin-top:2px}
.card .t{font:500 14px/20px var(--ui)}
.card.dn .t{font-weight:600}
.card .age{font:400 12px/20px var(--ui);color:var(--ink-muted);font-variant-numeric:tabular-nums;text-align:right;white-space:nowrap}
.card .qt{grid-column:2/4;font:italic 400 15px/22px var(--serif);color:var(--ink)}
.card .m{grid-column:2/4;display:flex;gap:8px;align-items:center;font:400 12px/16px var(--ui);color:var(--ink-muted);min-width:0}
.card .m .pj{flex:0 1 auto}
.card .m b{flex:none;font-weight:500}
.pr{font:500 11px/16px var(--mono);white-space:nowrap;flex:none;color:var(--ink-muted)}
.pr.ok{color:var(--calm)}.pr.fail{color:var(--signal-ink)}
.ln{display:grid;grid-template-columns:16px minmax(0,1fr) auto;column-gap:10px;align-items:center;height:30px;padding:0 10px;border-radius:6px;font:400 13px/18px var(--ui);color:var(--ink-muted)}
.ln .t{color:var(--ink)}
.ln .t i{font-style:normal;color:var(--ink-muted);font-size:12px;margin-left:8px;font-family:var(--mono)}
.ln .age{font-size:12px;font-variant-numeric:tabular-nums;white-space:nowrap}
.th{display:flex;flex-direction:column;min-width:0;min-height:0;background:var(--raised)}
.thh{padding:20px 0 0;border-bottom:1px solid var(--line);flex:none}
.col{width:680px;max-width:calc(100% - 64px);margin:0 auto;min-width:0}
.meta{display:flex;gap:10px;align-items:center;font:400 12px/16px var(--ui);color:var(--ink-muted);min-width:0}
.meta>span{white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-width:0;flex:none}
.meta>.pj{flex:0 1 auto}
.meta .st{display:inline-flex;gap:6px;align-items:center;font-weight:600;flex:none}
.meta .brn{font-family:var(--mono);flex:1 1 0;min-width:96px}
.meta .ml{margin-left:auto;display:flex;gap:8px;flex:none}
.tt{font:500 26px/32px var(--serif);letter-spacing:-.01em;margin:6px 0 10px;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere}
.tabs{display:flex;gap:22px}
.tabs span{padding:8px 0;font:500 13px/18px var(--ui);color:var(--ink-muted);border-bottom:2px solid transparent;white-space:nowrap}
.tabs span.on{color:var(--ink);border-color:var(--ink)}
.tabs em{font-style:normal;font-family:var(--mono);font-size:11px;margin-left:6px}
.convo{flex:1;min-height:0;overflow:hidden;display:flex;flex-direction:column;justify-content:flex-end;padding:16px 0}
.convo .col{display:flex;flex-direction:column;gap:14px}
.who{font:600 11px/16px var(--ui);letter-spacing:.06em;text-transform:uppercase;color:var(--ink-muted);margin-bottom:2px}
.you{font:400 14px/22px var(--ui);padding:8px 12px;border-radius:8px;background:var(--sunken)}
.cl{font:400 17px/27px var(--serif)}
.note{display:flex;flex-direction:column;border-left:2px solid var(--line);padding-left:12px;gap:2px}
.nl{display:grid;grid-template-columns:14px 44px minmax(0,1fr) 56px;column-gap:8px;align-items:center;font:400 12px/20px var(--ui);color:var(--ink-muted);height:20px}
.nl .nm{font-weight:600;color:var(--ink)}
.nl code{color:var(--ink-muted)}
.nl .d{text-align:right;font-variant-numeric:tabular-nums;white-space:nowrap;overflow:hidden}
.slip{flex:none;width:720px;max-width:calc(100% - 32px);margin:0 auto;border:1px solid var(--line-strong);border-radius:12px 12px 0 0;border-bottom:0;background:var(--raised);box-shadow:0 -6px 24px rgba(17,24,32,.08);max-height:56%;display:flex;flex-direction:column;min-height:0;overflow:hidden}
.sh{display:flex;gap:8px;align-items:center;padding:8px 16px;background:var(--beam-wash);font:400 12px/16px var(--ui);color:var(--ink-muted);flex:none;min-width:0;--knock:var(--beam-wash)}
.sh b{color:var(--beam-ink);flex:none}
.sh .ml{margin-left:auto;flex:none}
.sb{padding:10px 16px 8px;display:flex;flex-direction:column;gap:4px;min-height:0;overflow:hidden}
.q{font:500 19px/26px var(--serif);overflow-wrap:anywhere;margin-bottom:6px;overflow:auto;min-height:0;flex:0 1 auto}
.op{display:grid;grid-template-columns:22px minmax(0,1fr);column-gap:10px;align-items:start;padding:5px 8px;border-radius:6px;height:46px;flex:none}
.op.short{height:30px}
.op.on{background:var(--harbour-wash);box-shadow:inset 0 0 0 1px var(--harbour)}
.op kbd{margin-top:1px}
.op .a{font:500 14px/20px var(--ui)}
.op .d{font:400 12px/16px var(--ui);color:var(--ink-muted)}
.cmp{flex:none;width:720px;max-width:calc(100% - 32px);margin:0 auto 14px;border:1px solid var(--line-strong);border-top:1px solid var(--line);border-radius:0 0 12px 12px;display:flex;gap:10px;align-items:center;height:44px;padding:0 8px 0 16px}
.cmp .in{flex:1;min-width:0;font:400 14px/20px var(--ui);color:var(--ink-muted)}
.cmp .hint{font:400 12px/16px var(--ui);color:var(--ink-muted);white-space:nowrap}
.btn{display:inline-flex;align-items:center;gap:6px;height:30px;padding:0 12px;border-radius:6px;border:1px solid var(--line-strong);background:var(--raised);color:var(--ink);font:500 13px/18px var(--ui);white-space:nowrap}
.btn.p{background:var(--night);border-color:var(--night);color:var(--on-night)}
.btn.p kbd{background:rgba(255,255,255,.14);color:var(--on-night)}
.btn.g{border-color:transparent;background:transparent;color:var(--ink-muted)}
"""


def reader(D):
    c = D["counts"]
    T = D["thread"]

    def sec(name, n, att=False, hint=""):
        h = f'<span class="h ell">{e(hint)}</span>' if hint else ""
        return f'<div class="sec{" att" if att else ""}">{e(name)} <span class="n">{e(n)}</span>{h}</div>'

    needs = "".join(
        f'<div class="card{" sel" if r.get("sel") else ""}{" dn" if r["st"] == "d" else ""}">{ic(r["st"])}<span class="t ell">{e(r["title"])}</span>'
        f'<span class="age">{e(r["time"])}</span>'
        f'<span class="qt ell">{"“" + e(r["meta"]) + "”" if r["st"] == "a" else e(r["meta"])}</span>'
        f'<span class="m">{lead(r)}<span class="pj ell">{e(r["proj"])}</span>{pr(r["pr"])}</span></div>' for r in D["needs"])
    work = "".join(f'<div class="ln">{ic(r["st"])}<span class="t ell">{e(r["title"])}<i>{e(r["tool"])}</i></span><span class="age">{e(r["time"])}</span></div>' for r in D["work"])
    seen = "".join(f'<div class="ln">{ic(r["st"])}<span class="t ell">{e(r["title"])}<i>{e(r["meta"])}</i></span><span class="age">{e(r["time"])}</span></div>' for r in D["seen"])
    inbox = (f'<aside class="ib"><div class="top"><span class="brand">Fyr</span><span class="host"><i></i>desk</span><span class="ml"><kbd>⌘K</kbd></span></div>'
             f'<div class="away"><span style="flex:none">Since <b>{e(D["away"][0])}</b>:</span><span class="ell">{e(D["away"][1])}</span></div>'
             f'<div class="list">{sec("Needs you", c["needs"], True)}{needs}{sec("Working", c["work"], hint=D["work_hint"])}{work}'
             f'{sec("Seen", c["seen"])}{seen}{sec("Snoozed", c["snoozed"], hint=D["snooze_hint"])}{sec("Settled", c["settled"], hint="last 24 h")}</div></aside>')
    items = []
    tools = []

    def flush():
        if tools:
            items.append(f'<div class="note">{"".join(tools)}</div>')
            tools.clear()
    for it in T["convo"]:
        if it[0] == "tool":
            _, nm, arg, dur, st = it
            tools.append(f'<div class="nl">{ic(st, 12)}<span class="nm">{e(nm)}</span><code class="ell">{e(arg)}</code><span class="d">{e(dur)}</span></div>')
        elif it[0] == "subs":
            _, n, lbl, dur = it
            tools.append(f'<div class="nl">{ic("c", 12)}<span class="nm">Task</span><span class="ell">{e(n)} subagents · {e(lbl)} ▸</span><span class="d">{e(dur)}</span></div>')
        else:
            flush()
            if it[0] == "user":
                items.append(f'<div><div class="who">You · {e(it[1])}</div><div class="you prose">{e(it[2])}</div></div>')
            else:
                items.append(f'<div class="cl prose">{it[2]}</div>')
    flush()
    q = T["q"]
    opts = "".join(f'<div class="op{" on" if k == "1" else ""}{" short" if not d else ""}"><kbd>{k}</kbd><div style="min-width:0"><div class="a ell">{e(a)}</div>'
                   + (f'<div class="d ell">{e(d)}</div>' if d else "") + '</div></div>' for k, a, d in q["opts"])
    thread = (f'<section class="th"><div class="thh"><div class="col"><div class="meta"><span class="st {SCLS[T["st"]]}">{ic(T["st"], 14)}{WORD[T["st"]]}</span>'
              f'<span class="pj">{e(T["proj"])}</span><span class="brn">{e(T["branch"])}</span><span>{e(T["model"])}</span>{pr(T["pr"]) if T["pr"] else "<span>no PR</span>"}'
              f'<span class="ml"><button class="btn g">Snooze <kbd>s</kbd></button></span></div>'
              f'<div class="tt">{e(T["title"])}</div><div class="tabs"><span class="on">Conversation</span><span>Changes<em>{e(T["changes"])}</em></span><span>PR</span></div></div></div>'
              f'<div class="convo"><div class="col">{"".join(items)}</div></div>'
              f'<div class="slip"><div class="sh">{ic("a", 14)}<b>Question {e(q["idx"])} of {e(q["total"])}</b><span class="ell">from subagent “{e(q["by"])}”</span><span class="ml"><kbd>Tab</kbd> next</span></div>'
              f'<div class="sb"><div class="q">{e(q["text"])}</div>{opts}</div></div>'
              f'<div class="cmp"><span class="in ell">{e(T["composer"])}</span><span class="hint">⌘⏎ queue</span><button class="btn p">Answer <kbd>⏎</kbd></button></div></section>')
    return f'<div class="app">{inbox}{thread}</div>'


# ================================================================ 04 Rail
RAIL_CSS = """
:root{--ui:"Geist",system-ui,sans-serif;--mono:"Geist Mono",ui-monospace,monospace}
.app{display:grid;grid-template-columns:76px 372px minmax(0,1fr);height:100%}
.rail{display:flex;flex-direction:column;align-items:stretch;background:var(--sunken);border-right:1px solid var(--line);padding:10px 6px;gap:4px;min-height:0;--knock:var(--sunken)}
.rail .brand{font:600 15px/20px var(--ui);text-align:center;margin-bottom:10px}
.ri{display:flex;flex-direction:column;align-items:center;gap:2px;padding:8px 0 6px;border-radius:8px;color:var(--ink-muted)}
.ri.on{background:var(--raised);color:var(--ink);box-shadow:0 0 0 1px var(--line);--knock:var(--raised)}
.ri .n{font:500 13px/18px var(--mono);font-variant-numeric:tabular-nums;color:var(--ink);max-width:100%;overflow:hidden}
.ri.att .n{background:var(--beam);color:var(--on-beam);border-radius:9px;padding:0 6px}
.ri .l{font:500 10px/12px var(--ui);letter-spacing:.02em}
.ri .k{font:400 10px/12px var(--mono);color:var(--ink-faint)}
.rail .foot{margin-top:auto;display:flex;flex-direction:column;align-items:center;gap:4px;font:400 10px/12px var(--mono);color:var(--ink-muted)}
.rail .foot i{width:8px;height:8px;border-radius:50%;background:var(--calm)}
.ib{display:flex;flex-direction:column;min-width:0;min-height:0;background:var(--raised);border-right:1px solid var(--line)}
.top{display:flex;align-items:center;gap:8px;height:48px;padding:0 12px;border-bottom:1px solid var(--line);flex:none}
.search{flex:1;min-width:0;display:flex;align-items:center;height:30px;padding:0 10px;border-radius:8px;background:var(--sunken);color:var(--ink-muted);font:400 13px/18px var(--ui)}
kbd{font:500 11px/14px var(--mono);padding:1px 5px;min-width:18px;text-align:center;border-radius:5px;border:1px solid var(--line);background:var(--raised);color:var(--ink-muted);display:inline-block;flex:none}
.search kbd{margin-left:auto}
.away{display:flex;gap:6px;align-items:center;margin:8px 10px 0;padding:6px 10px;border-radius:8px;background:var(--harbour-wash);font:400 12px/16px var(--ui);flex:none;min-width:0}
.list{flex:1;min-height:0;overflow:hidden;padding:0 6px 8px}
.sec{display:flex;align-items:center;gap:8px;height:30px;padding:8px 8px 0;font:500 12px/16px var(--ui);color:var(--ink-muted);min-width:0}
.sec .n{font:400 12px/16px var(--mono);font-variant-numeric:tabular-nums}
.sec .h{margin-left:auto;font-size:11px;min-width:0;max-width:62%}
.rw{display:grid;grid-template-columns:28px minmax(0,1fr) auto;column-gap:10px;grid-template-rows:20px 18px;height:52px;padding:7px 8px;border-radius:10px;align-content:center}
.rw.sel{background:var(--select);--knock:var(--select)}
.tile{grid-row:1/3;width:28px;height:28px;border-radius:8px;background:var(--sunken);display:grid;place-items:center;align-self:center}
.rw.sel .tile{background:var(--raised);--knock:var(--raised)}
.rw .t{font:500 13px/20px var(--ui)}
.rw.dn .t{font-weight:600}
.rw .age{font:400 12px/20px var(--mono);font-variant-numeric:tabular-nums;color:var(--ink-muted);text-align:right;white-space:nowrap}
.rw .m{font:400 12px/18px var(--ui);color:var(--ink-muted)}
.rw .pr{justify-self:end;align-self:center}
.pr{display:inline-flex;align-items:center;height:18px;padding:0 7px;border-radius:9px;border:1px solid var(--line);font:500 11px/16px var(--mono);color:var(--ink-muted);white-space:nowrap;max-width:110px;overflow:hidden;text-overflow:ellipsis}
.pr.ok{color:var(--calm)}.pr.fail{color:var(--signal-ink);border-color:var(--signal-ink)}.pr.merged{background:var(--sunken)}
.pr.none{display:none}
.one{display:grid;grid-template-columns:28px minmax(0,1fr) auto 64px;column-gap:10px;align-items:center;height:30px;padding:0 8px;border-radius:8px;font:400 12px/16px var(--ui);color:var(--ink-muted)}
.one .ic{justify-self:center}
.one .t{font:500 13px/18px var(--ui);color:var(--ink-muted)}
.one .t i{font:400 11px/16px var(--mono);font-style:normal;margin-left:8px}
.one .age{font:400 12px/16px var(--mono);font-variant-numeric:tabular-nums;text-align:right;white-space:nowrap;overflow:hidden}
.sb{font:400 11px/16px var(--mono);font-variant-numeric:tabular-nums;white-space:nowrap}
.th{display:flex;flex-direction:column;min-width:0;min-height:0;background:var(--surface)}
.thh{padding:14px 24px 0;background:var(--raised);border-bottom:1px solid var(--line);flex:none}
.tr{display:flex;gap:12px;align-items:flex-start}
.tt{flex:1;min-width:0;font:600 20px/26px var(--ui);letter-spacing:-.015em;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere}
.acts{display:flex;gap:6px;flex:none}
.btn{display:inline-flex;align-items:center;gap:6px;height:30px;padding:0 12px;border-radius:8px;border:1px solid var(--line);background:var(--raised);color:var(--ink);font:500 13px/18px var(--ui);white-space:nowrap}
.btn[disabled]{opacity:.5}
.btn.p{background:var(--night);border-color:var(--night);color:var(--on-night)}
.btn.p kbd{background:transparent;color:var(--on-night);border-color:rgba(255,255,255,.3)}
.chips{display:flex;gap:6px;margin-top:8px;min-width:0}
.chip{display:inline-flex;align-items:center;gap:6px;height:24px;padding:0 9px;border-radius:12px;background:var(--sunken);font:500 12px/16px var(--ui);color:var(--ink-muted);white-space:nowrap;min-width:0;flex:0 1 auto;overflow:hidden;--knock:var(--sunken)}
.chip span{overflow:hidden;text-overflow:ellipsis;min-width:0}
.chip.st{flex:none;background:transparent;border:1px solid var(--line-strong);color:var(--ink)}
.chip.m{font-family:var(--mono);font-weight:400}
.chip.pj{max-width:36%}
.chip.brn{flex:1 1 0;max-width:max-content}
.chip.pr{border:0;height:24px;border-radius:12px;padding:0 9px;max-width:none;flex:none}
.tabs{display:flex;gap:4px;margin-top:10px}
.tabs span{padding:6px 10px 8px;font:500 13px/18px var(--ui);color:var(--ink-muted);border-bottom:2px solid transparent;white-space:nowrap}
.tabs span.on{color:var(--ink);border-color:var(--ink)}
.tabs em{font:400 11px/16px var(--mono);font-style:normal;margin-left:6px}
.convo{flex:1;min-height:0;overflow:hidden;display:flex;flex-direction:column;justify-content:flex-end;gap:12px;padding:12px 24px}
.you{align-self:flex-end;max-width:72%;padding:8px 12px;border-radius:14px 14px 4px 14px;background:var(--night);color:var(--on-night);font:400 14px/21px var(--ui)}
.cl{max-width:680px;font:400 15px/23px var(--ui)}
.act{max-width:680px;border:1px solid var(--line);border-radius:12px;background:var(--raised);padding:4px 0}
.al{display:grid;grid-template-columns:16px 44px minmax(0,1fr) 60px;column-gap:10px;align-items:center;height:26px;padding:0 12px;font:400 12px/16px var(--mono);color:var(--ink-muted)}
.al .nm{color:var(--ink);font-weight:500;font-family:var(--ui)}
.al .s{font-family:var(--ui)}
.al .d{text-align:right;font-variant-numeric:tabular-nums;white-space:nowrap;overflow:hidden}
.ask{flex:none;margin:0 24px;border:1px solid var(--line-strong);border-radius:14px;background:var(--raised);max-height:58%;display:flex;flex-direction:column;min-height:0;overflow:hidden}
.ah{display:flex;gap:8px;align-items:center;padding:8px 14px;background:var(--beam-wash);font:400 12px/16px var(--ui);color:var(--ink-muted);flex:none;min-width:0;--knock:var(--beam-wash)}
.ah b{color:var(--beam-ink);flex:none}
.ah .ml{margin-left:auto;flex:none}
.ab{padding:10px 14px 12px;display:flex;flex-direction:column;min-height:0;overflow:hidden}
.q{font:500 16px/23px var(--ui);overflow-wrap:anywhere;margin-bottom:10px;overflow:auto;min-height:0;flex:0 1 auto}
.grid4{display:grid;grid-template-columns:1fr 1fr;gap:8px;flex:none}
.op{display:grid;grid-template-columns:minmax(0,1fr) auto;grid-template-rows:20px 32px;column-gap:8px;height:70px;padding:8px 10px;border:1px solid var(--line);border-radius:10px;min-width:0}
.op.on{border-color:var(--harbour);background:var(--harbour-wash);box-shadow:0 0 0 1px var(--harbour)}
.op .a{font:500 13px/20px var(--ui)}
.op .d{grid-column:1/3;font:400 12px/16px var(--ui);color:var(--ink-muted);display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere}
.cmp{flex:none;margin:10px 24px 16px;border:1px solid var(--line-strong);border-radius:14px;background:var(--raised);display:flex;align-items:center;gap:10px;height:46px;padding:0 8px 0 16px}
.cmp .in{flex:1;min-width:0;font:400 14px/20px var(--ui);color:var(--ink-muted)}
.cmp .hint{font:400 12px/16px var(--ui);color:var(--ink-muted);white-space:nowrap}
"""


def rail(D):
    c = D["counts"]
    T = D["thread"]

    def ri(st, n, label, key, cls=""):
        return f'<div class="ri {cls}">{ic(st)}<span class="n">{e(n)}</span><span class="l">{label}</span><span class="k">g{key}</span></div>'
    railh = (f'<nav class="rail"><div class="brand">Fyr</div>{ri("a", c["needs"], "Needs", "1", "att on")}{ri("w", c["work"], "Working", "2")}'
             f'{ri("s", c["seen"], "Seen", "3")}{ri("z", c["snoozed"], "Snoozed", "4")}{ri("c", c["settled"], "Settled", "5")}'
             f'<div class="foot"><i></i>desk</div></nav>')

    def sec(name, n, hint=""):
        h = f'<span class="h ell">{e(hint)}</span>' if hint else ""
        return f'<div class="sec">{e(name)}<span class="n">{e(n)}</span>{h}</div>'
    needs = "".join(
        f'<div class="rw{" sel" if r.get("sel") else ""}{" dn" if r["st"] == "d" else ""}"><span class="tile">{ic(r["st"])}</span>'
        f'<span class="t ell">{e(r["title"])}</span><span class="age">{e(r["time"])}</span>'
        f'<span class="m ell">{lead(r)} · {e(r["proj"])} · {e(r["meta"])}</span>{pr(r["pr"])}</div>' for r in D["needs"])
    work = "".join(f'<div class="one">{ic(r["st"])}<span class="t ell">{e(r["title"])}<i>{e(r["tool"])}</i></span><span class="sb">{e(r["subs"])} sub</span><span class="age">{e(r["time"])}</span></div>' for r in D["work"])
    seen = "".join(f'<div class="one">{ic(r["st"])}<span class="t ell">{e(r["title"])}<i>{e(r["proj"])}</i></span>{pr(r["pr"]) if r["pr"] else "<span></span>"}<span class="age">{e(r["time"])}</span></div>' for r in D["seen"])
    inbox = (f'<aside class="ib"><div class="top"><div class="search">Search threads<kbd>⌘K</kbd></div></div>'
             f'<div class="away"><span style="flex:none">Since <b>{e(D["away"][0])}</b></span><span class="ell">{e(D["away"][1])}</span></div>'
             f'<div class="list">{sec("Needs you", c["needs"])}{needs}{sec("Working", c["work"], D["work_hint"])}{work}'
             f'{sec("Seen", c["seen"], "⇧E settle")}{seen}{sec("Snoozed", c["snoozed"], D["snooze_hint"])}{sec("Settled", c["settled"], "last 24 h")}</div></aside>')
    items = []
    tools = []

    def flush():
        if tools:
            items.append(f'<div class="act">{"".join(tools)}</div>')
            tools.clear()
    for it in T["convo"]:
        if it[0] == "tool":
            _, nm, arg, dur, st = it
            tools.append(f'<div class="al">{ic(st, 14)}<span class="nm">{e(nm)}</span><span class="ell">{e(arg)}</span><span class="d">{e(dur)}</span></div>')
        elif it[0] == "subs":
            _, n, lbl, dur = it
            tools.append(f'<div class="al">{ic("c", 14)}<span class="nm">Task</span><span class="s ell">{e(n)} subagents · {e(lbl)} ▸</span><span class="d">{e(dur)}</span></div>')
        else:
            flush()
            if it[0] == "user":
                items.append(f'<div class="you prose">{e(it[2])}</div>')
            else:
                items.append(f'<div class="cl prose">{it[2]}</div>')
    flush()
    q = T["q"]
    opts = "".join(f'<div class="op{" on" if k == "1" else ""}"><span class="a ell">{e(a)}</span><kbd>{k}</kbd><span class="d">{e(d)}</span></div>' for k, a, d in q["opts"])
    prchip = f'<span class="chip pr {T["pr"][0]}">{e(T["pr"][1])}</span>' if T["pr"] else '<span class="chip" style="flex:none">no PR</span>'
    thread = (f'<section class="th"><div class="thh"><div class="tr"><div class="tt">{e(T["title"])}</div>'
              f'<div class="acts"><button class="btn">Snooze <kbd>s</kbd></button><button class="btn" disabled>Settle <kbd>e</kbd></button></div></div>'
              f'<div class="chips"><span class="chip st {SCLS[T["st"]]}">{ic(T["st"], 14)}<span style="color:var(--ink)">{WORD[T["st"]]}</span></span>'
              f'<span class="chip pj"><span>{e(T["proj"])}</span></span><span class="chip m brn"><span>{e(T["branch"])}</span></span>'
              f'<span class="chip m"><span>{e(T["model"])}</span></span>{prchip}</div>'
              f'<div class="tabs"><span class="on">Conversation</span><span>Changes<em>{e(T["changes"])}</em></span><span>PR</span></div></div>'
              f'<div class="convo">{"".join(items)}</div>'
              f'<div class="ask"><div class="ah">{ic("a", 14)}<b>Question {e(q["idx"])} of {e(q["total"])}</b><span class="ell">asked by subagent “{e(q["by"])}”</span><span class="ml"><kbd>Tab</kbd> {e(q["nxt_kind"])} next</span></div>'
              f'<div class="ab"><div class="q">{e(q["text"])}</div><div class="grid4">{opts}</div></div></div>'
              f'<div class="cmp"><span class="in ell">{e(T["composer"])}</span><span class="hint">⌘⏎ queue</span><button class="btn p">Answer <kbd>⏎</kbd></button></div></section>')
    return f'<div class="app">{railh}{inbox}{thread}</div>'


# ================================================================ 05 Spine
SPINE_CSS = """
:root{--ui:"Atkinson Hyperlegible Next",system-ui,sans-serif;--mono:"Atkinson Hyperlegible Mono",ui-monospace,monospace}
.frame{font-size:14px}
.app{display:grid;grid-template-columns:432px minmax(0,1fr);height:100%}
.ib{display:flex;flex-direction:column;min-width:0;min-height:0;background:var(--raised);border-right:2px solid var(--line)}
.top{display:flex;align-items:center;gap:10px;height:48px;padding:0 16px;border-bottom:1px solid var(--line);flex:none}
.brand{font:700 17px/22px var(--ui)}
.host{display:flex;gap:6px;align-items:center;font:400 12px/16px var(--mono);color:var(--ink-muted)}
.host i{width:7px;height:7px;border-radius:50%;background:var(--calm)}
.top .ml{margin-left:auto}
kbd{font:500 12px/16px var(--mono);padding:0 5px;min-width:18px;text-align:center;border:1.5px solid var(--line-strong);border-radius:4px;color:var(--ink);background:var(--raised);display:inline-block;flex:none}
.away{display:grid;grid-template-columns:84px minmax(0,1fr);height:32px;align-items:center;border-bottom:1px solid var(--line);font:400 13px/18px var(--ui);background:var(--harbour-wash);flex:none}
.away .tm{font:700 12px/16px var(--mono);text-align:right;padding-right:8px}
.list{flex:1;min-height:0;overflow:hidden}
.sec{display:grid;grid-template-columns:84px auto auto minmax(0,1fr);column-gap:10px;align-items:center;height:34px;padding-right:16px;border-bottom:1px solid var(--line);font:700 14px/20px var(--ui);color:var(--ink)}
.sec .n{font:500 13px/18px var(--mono);font-variant-numeric:tabular-nums;justify-self:end;padding-right:8px;grid-column:1;grid-row:1;color:var(--ink-muted)}
.sec.att .n{background:var(--beam);color:var(--on-beam);border-radius:4px;padding:0 5px;margin-right:8px;font-weight:700}
.sec .nm{grid-column:2}
.sec .h{grid-column:4;font:400 12px/16px var(--ui);color:var(--ink-muted);text-align:right}
.rw{display:grid;grid-template-columns:84px 18px minmax(0,1fr) auto;column-gap:10px;grid-template-rows:22px 20px;height:58px;align-content:center;border-bottom:1px solid var(--line);padding-right:16px}
.rw.sel{background:var(--select);--knock:var(--select);box-shadow:inset 4px 0 0 var(--harbour)}
.tm{font:400 12px/22px var(--mono);font-variant-numeric:tabular-nums;color:var(--ink-muted);text-align:right;padding-right:8px;border-right:2px solid var(--line);white-space:nowrap;overflow:hidden;grid-row:1/3;align-self:stretch;margin:-8px 0}
.rw .tm{padding-top:8px}
.rw .ic{margin-top:3px}
.rw .t{font:700 14px/22px var(--ui)}
.rw.dn .t{text-decoration:underline;text-underline-offset:3px;text-decoration-thickness:1px}
.rw .m{grid-column:3/5;font:400 13px/20px var(--ui);color:var(--ink-muted);display:flex;gap:6px;min-width:0}
.rw .m .w{flex:none;font-weight:700;color:var(--ink)}
.rw .m .pj{flex:0 1 auto;font-family:var(--mono);font-size:12px;max-width:40%}
.rw .m .tx{flex:1 1 0}
.rw .sw{grid-row:2;grid-column:2;font:700 9px/12px var(--ui);color:var(--ink-muted)}
.pr{font:500 12px/18px var(--mono);white-space:nowrap;padding:0 5px;border-radius:3px;border:1px solid var(--line-strong);color:var(--ink-muted);align-self:center;max-width:120px;overflow:hidden;text-overflow:ellipsis}
.pr.ok{color:var(--calm)}.pr.fail{color:var(--signal-ink);border-color:var(--signal-ink);border-width:1.5px;font-weight:700}
.pr.none{border:0;padding:0}
.one{display:grid;grid-template-columns:84px 18px minmax(0,1fr) auto;column-gap:10px;height:34px;align-items:center;border-bottom:1px solid var(--line);padding-right:16px;font:400 13px/18px var(--ui);color:var(--ink-muted)}
.one .tm{line-height:34px;margin:0;align-self:stretch;grid-row:auto;padding-top:0}
.one .t{color:var(--ink)}
.one .t i{font-style:normal;color:var(--ink-muted);font-family:var(--mono);font-size:12px;margin-left:8px}
.one .sb{font:400 12px/16px var(--mono);white-space:nowrap}
.th{display:flex;flex-direction:column;min-width:0;min-height:0;background:var(--surface)}
.thh{padding:14px 28px 0;background:var(--raised);border-bottom:1px solid var(--line);flex:none}
.tr{display:flex;gap:12px;align-items:flex-start}
.tt{flex:1;min-width:0;font:700 20px/26px var(--ui);display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere}
.btn{display:inline-flex;align-items:center;gap:6px;height:32px;padding:0 12px;border-radius:6px;border:1.5px solid var(--line-strong);background:var(--raised);color:var(--ink);font:700 13px/18px var(--ui);white-space:nowrap}
.btn[disabled]{opacity:.5}
.btn.p{background:var(--night);border-color:var(--night);color:var(--on-night)}
.btn.p kbd{background:transparent;color:var(--on-night);border-color:rgba(255,255,255,.4)}
.acts{display:flex;gap:6px;flex:none}
.facts{display:flex;gap:14px;margin-top:6px;font:400 13px/18px var(--ui);color:var(--ink-muted);min-width:0;align-items:center}
.facts>span{white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-width:0;flex:0 1 auto}
.facts .st{display:inline-flex;gap:6px;align-items:center;font-weight:700;color:var(--ink);flex:none;padding:1px 8px 1px 6px;border:1.5px solid var(--line-strong);border-radius:4px}
.facts .mono{font-family:var(--mono);font-size:12px}
.facts .brn{flex:1 1 0;min-width:96px}.facts .pj{max-width:36%}
.tabs{display:flex;gap:22px;margin-top:8px}
.tabs span{padding:7px 0;font:700 13px/18px var(--ui);color:var(--ink-muted);border-bottom:3px solid transparent;white-space:nowrap}
.tabs span.on{color:var(--ink);border-color:var(--ink)}
.tabs em{font:400 12px/16px var(--mono);font-style:normal;margin-left:6px}
.convo{flex:1;min-height:0;overflow:hidden;display:flex;flex-direction:column;justify-content:flex-end;padding:8px 28px 0}
.ev{display:grid;grid-template-columns:44px 20px minmax(0,1fr);column-gap:12px;position:relative;padding:6px 0}
.ev::before{content:"";position:absolute;left:65px;top:0;bottom:0;width:2px;background:var(--line-strong)}
.ev .tm{border:0;padding:0;margin:0;font-size:12px;line-height:22px;grid-row:auto}
.node{position:relative;z-index:1;display:grid;place-items:center;width:20px;height:22px;background:var(--surface);--knock:var(--surface)}
.node .sq{width:10px;height:10px;border:2px solid var(--ink)}
.node .dt{width:10px;height:10px;border-radius:50%;background:var(--ink)}
.node .tk{width:8px;height:2px;background:var(--ink-muted)}
.ev .who{font:700 12px/22px var(--ui);color:var(--ink-muted);text-transform:uppercase;letter-spacing:.06em}
.ev .tx{font:400 15px/23px var(--ui);max-width:640px}
.ev.tool{padding:1px 0}
.ev.tool .tx{display:grid;grid-template-columns:48px minmax(0,1fr) 64px;column-gap:8px;font:400 13px/22px var(--mono);color:var(--ink-muted)}
.ev.tool .nm{color:var(--ink);font-weight:500}
.ev.tool .d{text-align:right;font-variant-numeric:tabular-nums;white-space:nowrap;overflow:hidden}
.ev.tool .s{font-family:var(--ui)}
.ev.last::before{bottom:auto;height:100%}
.dock{flex:none;display:grid;grid-template-columns:44px 20px minmax(0,1fr);column-gap:12px;padding:0 28px;position:relative;max-height:58%;min-height:0}
.dock::before{content:"";position:absolute;left:93px;top:0;height:18px;width:2px;background:var(--line-strong)}
.dock .node{margin-top:12px;height:22px}
.card{grid-column:3;border:2px solid var(--ink);border-radius:8px;background:var(--raised);display:flex;flex-direction:column;min-height:0;overflow:hidden;margin-top:6px}
.ch{display:flex;gap:8px;align-items:center;height:32px;padding:0 12px;background:var(--beam-wash);border-bottom:1px solid var(--line);font:400 13px/18px var(--ui);color:var(--ink);flex:none;min-width:0}
.ch b{flex:none}
.ch .ml{margin-left:auto;flex:none;color:var(--ink-muted)}
.cb{padding:10px 12px;display:flex;flex-direction:column;gap:6px;min-height:0;overflow:hidden}
.q{font:700 16px/24px var(--ui);overflow-wrap:anywhere;overflow:auto;min-height:0;flex:0 1 auto;margin-bottom:2px}
.op{display:grid;grid-template-columns:26px minmax(0,1fr);column-gap:10px;align-items:center;height:44px;padding:0 10px;border:1.5px solid var(--line);border-radius:6px;flex:none}
.op.short{height:34px}
.op.on{border-color:var(--ink);background:var(--select)}
.op.on .a::after{content:"  ◂ selected";font-weight:400;color:var(--ink-muted);font-size:12px}
.op .a{font:700 14px/20px var(--ui)}
.op .d{font:400 13px/18px var(--ui);color:var(--ink-muted)}
.cmp{flex:none;margin:10px 28px 16px 92px;border:1.5px solid var(--line-strong);border-radius:8px;background:var(--raised);display:flex;align-items:center;gap:10px;height:46px;padding:0 8px 0 14px}
.cmp .in{flex:1;min-width:0;font:400 15px/22px var(--ui);color:var(--ink-muted)}
.cmp .hint{font:400 12px/16px var(--ui);color:var(--ink-muted);white-space:nowrap}
"""


def spine(D):
    c = D["counts"]
    T = D["thread"]

    def sec(name, n, att=False, hint=""):
        return (f'<div class="sec{" att" if att else ""}"><span class="n">{e(n)}</span><span class="nm">{e(name)}</span><span></span>'
                f'<span class="h ell">{e(hint)}</span></div>')
    needs = "".join(
        f'<div class="rw{" sel" if r.get("sel") else ""}{" dn" if r["st"] == "d" else ""}"><span class="tm">{e(r["time"])}</span>{ic(r["st"], 18)}'
        f'<span class="t ell">{e(r["title"])}</span>{pr(r["pr"])}'
        f'<span class="m"><span class="w">{e(r["lead"])}</span><span class="pj ell">{e(r["proj"])}</span><span class="tx ell">{e(r["meta"])}</span></span></div>' for r in D["needs"])
    work = "".join(f'<div class="one"><span class="tm">{e(r["time"])}</span>{ic(r["st"], 18)}<span class="t ell">{e(r["title"])}<i>{e(r["tool"])}</i></span><span class="sb">{e(r["subs"])} sub</span></div>' for r in D["work"])
    seen = "".join(f'<div class="one"><span class="tm">{e(r["time"])}</span>{ic(r["st"], 18)}<span class="t ell">{e(r["title"])}<i>{e(r["proj"])}</i></span>{pr(r["pr"])}</div>' for r in D["seen"])
    inbox = (f'<aside class="ib"><div class="top"><span class="brand">Fyr</span><span class="host"><i></i>desk</span><span class="ml"><kbd>⌘K</kbd></span></div>'
             f'<div class="away"><span class="tm" style="border:0;margin:0;line-height:32px">{e(D["away"][0])}</span><span class="ell" style="padding:0 16px 0 10px">Since then: {e(D["away"][1])}</span></div>'
             f'<div class="list">{sec("Needs you", c["needs"], True)}{needs}{sec("Working", c["work"], hint=D["work_hint"])}{work}'
             f'{sec("Seen", c["seen"], hint="⇧E settle")}{seen}{sec("Snoozed", c["snoozed"], hint=D["snooze_hint"])}{sec("Settled", c["settled"], hint="last 24 h")}</div></aside>')
    items = []
    for it in T["convo"]:
        if it[0] in ("user", "asst"):
            node = '<span class="sq"></span>' if it[0] == "user" else '<span class="dt"></span>'
            txt = e(it[2]) if it[0] == "user" else it[2]
            who = "You" if it[0] == "user" else "Claude"
            items.append(f'<div class="ev"><span class="tm">{e(it[1])}</span><span class="node">{node}</span><div class="prose"><div class="who">{who}</div><div class="tx">{txt}</div></div></div>')
        elif it[0] == "tool":
            _, nm, arg, dur, st = it
            items.append(f'<div class="ev tool"><span class="tm"></span><span class="node">{ic(st, 12)}</span><div class="tx"><span class="nm">{e(nm)}</span><span class="ell">{e(arg)}</span><span class="d">{e(dur)}</span></div></div>')
        else:
            _, n, lbl, dur = it
            items.append(f'<div class="ev tool"><span class="tm"></span><span class="node">{ic("c", 12)}</span><div class="tx"><span class="nm">Task</span><span class="s ell">{e(n)} subagents · {e(lbl)} ▸</span><span class="d">{e(dur)}</span></div></div>')
    q = T["q"]
    opts = "".join(f'<div class="op{" on" if k == "1" else ""}{" short" if not d else ""}"><kbd>{k}</kbd><div style="min-width:0"><div class="a ell">{e(a)}</div>'
                   + (f'<div class="d ell">{e(d)}</div>' if d else "") + '</div></div>' for k, a, d in q["opts"])
    thread = (f'<section class="th"><div class="thh"><div class="tr"><div class="tt">{e(T["title"])}</div>'
              f'<div class="acts"><button class="btn">Snooze <kbd>s</kbd></button><button class="btn" disabled>Settle <kbd>e</kbd></button></div></div>'
              f'<div class="facts"><span class="st"><span class="{SCLS[T["st"]]}">{ic(T["st"], 14)}</span>{WORD[T["st"]]}</span><span class="pj">{e(T["proj"])}</span>'
              f'<span class="mono brn">{e(T["branch"])}</span><span class="mono">{e(T["model"])}</span>{pr(T["pr"]) if T["pr"] else "<span>No pull request</span>"}</div>'
              f'<div class="tabs"><span class="on">Conversation</span><span>Changes<em>{e(T["changes"])}</em></span><span>PR</span></div></div>'
              f'<div class="convo">{"".join(items)}</div>'
              f'<div class="dock"><span></span><span class="node">{ic("a", 16)}</span><div class="card"><div class="ch"><b>Question {e(q["idx"])} of {e(q["total"])}</b><span class="ell">asked by subagent “{e(q["by"])}”</span><span class="ml"><kbd>Tab</kbd> next</span></div>'
              f'<div class="cb"><div class="q">{e(q["text"])}</div>{opts}</div></div></div>'
              f'<div class="cmp"><span class="in ell">{e(T["composer"])}</span><span class="hint">⌘⏎ queue</span><button class="btn p">Answer <kbd>⏎</kbd></button></div></section>')
    return f'<div class="app">{inbox}{thread}</div>'


DIRECTIONS = [
    ("01-ledger.html", "01 Ledger",
     "A ruled table: every row shares fixed columns for project, PR and age, so the eye runs down one column at a time.",
     "family=IBM+Plex+Sans+Condensed:wght@400;500;600&amp;family=IBM+Plex+Mono:wght@400;500", LEDGER_CSS, ledger),
    ("02-console.html", "02 Console",
     "A terminal-grade view in one monospace face, with a modeline that always shows the mode and the keys that work right now.",
     "family=JetBrains+Mono:wght@400;500;700&amp;family=Inter:wght@400;500;600", CONSOLE_CSS, console),
    ("03-reader.html", "03 Reader",
     "A reading view: the thread is a centred column set in a serif, and the question is the largest text on screen.",
     "family=Newsreader:ital,opsz,wght@0,6..72,400;0,6..72,500;1,6..72,400&amp;family=Inter:wght@400;500;600&amp;family=IBM+Plex+Mono:wght@400;500", READER_CSS, reader),
    ("04-rail.html", "04 Rail",
     "A count rail on the far left holds the five sections as big numbers; the question card lays its options out as a 2 × 2 grid of keys.",
     "family=Geist:wght@400;500;600&amp;family=Geist+Mono:wght@400;500", RAIL_CSS, rail),
    ("05-spine.html", "05 Spine",
     "Time first: a time gutter runs down the inbox and becomes the timeline spine of the thread, which ends in the open question.",
     "family=Atkinson+Hyperlegible+Next:wght@400;500;700&amp;family=Atkinson+Hyperlegible+Mono:wght@400;500;700", SPINE_CSS, spine),
]

if __name__ == "__main__":
    os.makedirs(OUT, exist_ok=True)
    for fname, title, idea, fonts, css, fn in DIRECTIONS:
        page(fname, title, idea, fonts, css, fn)
        print("wrote", fname)
