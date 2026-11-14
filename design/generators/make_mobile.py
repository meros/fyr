#!/usr/bin/env python3
"""Generate design/mobile/mobile.html: Fyr phone mocks at 390 x 844 (Rail tokens, Geist)."""
import html, sys

OUT = sys.argv[1]

# ---- icons (the nine state icons of 04-rail / screens-v1; waiting is the dashed ring) ----
SVG = {
 "ask": '<circle cx="8" cy="8" r="7" fill="currentColor"/><path d="M6.2 6.1a1.9 1.9 0 1 1 2.6 1.8c-.5.2-.8.6-.8 1.1v.4" fill="none" stroke="var(--knock)" stroke-width="1.5" stroke-linecap="round"/><circle cx="8" cy="11.6" r=".9" fill="var(--knock)"/>',
 "fail": '<path d="M8 .9 15.1 8 8 15.1.9 8Z" fill="currentColor"/><path d="M8 4.6v4.2" stroke="var(--knock)" stroke-width="1.7" stroke-linecap="round"/><circle cx="8" cy="11.2" r=".95" fill="var(--knock)"/>',
 "block": '<rect x="2" y="2" width="12" height="12" rx="3" fill="none" stroke="currentColor" stroke-width="1.7"/><rect x="5" y="7" width="6" height="2" rx="1" fill="currentColor"/>',
 "done": '<circle cx="8" cy="8" r="4" fill="currentColor"/>',
 "work": '<circle cx="8" cy="8" r="5.5" fill="none" stroke="currentColor" stroke-opacity=".3" stroke-width="1.7"/><path d="M8 2.5a5.5 5.5 0 0 1 5.5 5.5" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/>',
 "wait": '<circle cx="8" cy="8" r="5.5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-dasharray="2.2 2.1"/>',
 "seen": '<rect x="4" y="4" width="8" height="8" rx="2" fill="none" stroke="currentColor" stroke-width="1.5"/>',
 "snz": '<path d="M10.5 2.6a5.6 5.6 0 1 0 3 8.9A4.6 4.6 0 0 1 10.5 2.6Z" fill="currentColor"/>',
 "set": '<path d="M3.2 8.4 6.5 11.6 12.8 4.6" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"/>',
}
LABEL = {"ask": "Asking", "fail": "Failed", "block": "Blocked", "done": "Done", "work": "Working",
         "wait": "Waiting", "seen": "Seen", "snz": "Snoozed", "set": "Settled"}


def ic(s, n=16, extra=""):
    return (f'<svg class="ic s-{s} {extra}" width="{n}" height="{n}" viewBox="0 0 16 16" role="img" '
            f'aria-label="{LABEL[s]}">{SVG[s]}</svg>')


G = {  # glyphs for UI controls (not state icons)
 "back": '<svg width="22" height="22" viewBox="0 0 22 22"><path d="M13.5 4.5 7 11l6.5 6.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>',
 "more": '<svg width="22" height="22" viewBox="0 0 22 22"><circle cx="5" cy="11" r="1.7" fill="currentColor"/><circle cx="11" cy="11" r="1.7" fill="currentColor"/><circle cx="17" cy="11" r="1.7" fill="currentColor"/></svg>',
 "bell": '<svg width="20" height="20" viewBox="0 0 16 16"><path d="M4 11.5V7.2a4 4 0 0 1 8 0v4.3l1.2 1.2H2.8Z" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/><path d="M6.6 14.2a1.5 1.5 0 0 0 2.8 0" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>',
 "bellon": '<svg width="20" height="20" viewBox="0 0 16 16"><path d="M4 11.5V7.2a4 4 0 0 1 8 0v4.3l1.2 1.2H2.8Z" fill="currentColor" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/><path d="M6.6 14.2a1.5 1.5 0 0 0 2.8 0" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>',
 "belloff": '<svg width="20" height="20" viewBox="0 0 16 16"><path d="M4 11.5V7.2a4 4 0 0 1 8 0v4.3l1.2 1.2H2.8Z" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/><path d="M2.5 2.5l11 11" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>',
 "inbox": '<svg width="22" height="22" viewBox="0 0 22 22"><path d="M3.5 12.5 6 4.5h10l2.5 8v5h-15Z M3.5 12.5h4.5l1 2h4l1-2h4.5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"/></svg>',
 "search": '<svg width="22" height="22" viewBox="0 0 22 22"><circle cx="9.5" cy="9.5" r="5.5" fill="none" stroke="currentColor" stroke-width="1.7"/><path d="m13.8 13.8 4.4 4.4" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/></svg>',
 "plus": '<svg width="18" height="18" viewBox="0 0 18 18"><path d="M9 3.5v11M3.5 9h11" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg>',
 "send": '<svg width="20" height="20" viewBox="0 0 20 20"><path d="M10 15.5v-11M5 9l5-5 5 5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>',
 "stop": '<svg width="16" height="16" viewBox="0 0 16 16"><rect x="3.5" y="3.5" width="9" height="9" rx="1.5" fill="currentColor"/></svg>',
 "clip": '<svg width="22" height="22" viewBox="0 0 22 22"><rect x="5" y="4" width="12" height="15" rx="2" fill="none" stroke="currentColor" stroke-width="1.6"/><rect x="8" y="2.5" width="6" height="3.5" rx="1" fill="var(--raised)" stroke="currentColor" stroke-width="1.6"/></svg>',
 "photo": '<svg width="22" height="22" viewBox="0 0 22 22"><rect x="3" y="4.5" width="16" height="13" rx="2" fill="none" stroke="currentColor" stroke-width="1.6"/><circle cx="8" cy="9" r="1.6" fill="currentColor"/><path d="m4 16 5-5 3.5 3.5 2-2L18.5 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"/></svg>',
 "x": '<svg width="20" height="20" viewBox="0 0 20 20"><path d="m5 5 10 10M15 5 5 15" stroke="currentColor" stroke-width="1.9" stroke-linecap="round"/></svg>',
 "chev": '<svg width="16" height="16" viewBox="0 0 16 16"><path d="m4 6 4 4 4-4" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/></svg>',
 "chevup": '<svg width="18" height="18" viewBox="0 0 16 16"><path d="m4 10 4-4 4 4" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/></svg>',
 "install": '<svg width="20" height="20" viewBox="0 0 16 16"><rect x="4" y="1.8" width="8" height="12.4" rx="1.8" fill="none" stroke="currentColor" stroke-width="1.4"/><path d="M8 4.8v5M6 8l2 2 2-2" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"/></svg>',
 "undo": '<svg width="18" height="18" viewBox="0 0 18 18"><path d="M6 4 2.5 7.5 6 11M3 7.5h7.5a4 4 0 0 1 0 8H8" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/></svg>',
}

e = html.escape

# ---- stress data (same as design/generators/make_directions.py) ----
T140 = "Investigate why the nightly Playwright run on the staging cluster times out only when map tiles come from the secondary CDN region again, EU"
P59 = "norban-platform-infrastructure-terraform-modules-eu-north-1"
BR = "feature/very-long-branch-name-without-any-spaces-that-keeps-going-2026-11-14-final"
URL = "https://github.com/NorbanDev/norban-web/actions/runs/12345678901/job/98765432109?check_suite_focus=true"
SV = "Ändra översättningar för sökfältet på kartan så att å, ä och ö visas rätt"
QLONG = ("The staging deploy for norban/web fails at the database migration step because the new column "
         "listings.lasso_polygon is NOT NULL and 18402 existing rows have no value. I can backfill it from the "
         "saved search areas, which takes about 40 minutes on staging and locks the table for writes, or I can "
         "make the column nullable now and backfill in a background job after the deploy. The second option keeps "
         "the site writable, but the map filter shows no lasso for older listings until the job finishes, probably "
         "2 to 3 hours. Which do you want?")
HOST = "build-server-eu-north-1-very-long-hostname"

# ---- frame chrome ----
def sbar():
    return '<div class="sb"><span>9:41</span><span class="r">5G ▮▮▮ 82%</span></div>'

def home(cls=""):
    return f'<div class="hm {cls}"><i></i></div>'

def tap(n, x, y):
    return f'<span class="tap" style="left:{x}px;top:{y}px">{n}</span>'

def b44(g, label, cls=""):
    return f'<span class="b44 {cls}" role="button" aria-label="{label}">{G[g]}</span>'

# ---- inbox pieces ----
def inbox_header(bell="bell", conn="desk", install=True, extra=""):
    bl = {"bell": G["bell"], "on": G["bellon"], "off": G["belloff"]}[bell]
    ins = b44("install", "Install Fyr") if install else ""
    return (f'<header class="ih"><span class="brand">Fyr</span><span class="conn"><i class="{extra}"></i><span class="ell">{e(conn)}</span></span>'
            f'<span class="sp"></span>{ins}<span class="b44 bell-{bell}" aria-label="Notifications">{bl}</span>{b44("more","More")}</header>')

def strip(counts=(4, 4, 5, 3, 11), on=0):
    names = [("ask", "Needs"), ("work", "Working"), ("seen", "Seen"), ("snz", "Snoozed"), ("set", "Settled")]
    out = []
    for i, ((s, l), c) in enumerate(zip(names, counts)):
        cls = "seg" + (" on" if i == on else "") + (" att" if i == 0 and c else "")
        out.append(f'<span class="{cls}" role="button"><span class="c">{ic(s)}<span class="n">{c}</span></span><span class="l">{l}</span></span>')
    return '<nav class="strip">' + "".join(out) + "</nav>"

def away(when="14:02", txt="3 done · 2 PRs merged · 1 failed"):
    return (f'<div class="away"><span class="ell"><span style="flex:none">Since <b>{e(when)}</b> · </span>{e(txt)}</span>'
            f'{b44("x","Dismiss")}</div>')

def sec(t, n, h="", cls="", btn=""):
    hh = f'<span class="h ell">{e(h)}</span>' if h else ""
    b = f'<span class="sbtn">{e(btn)}</span>' if btn else ""
    return f'<div class="sec {cls}"><span>{e(t)}</span><span class="n">{n}</span>{hh}{b}</div>'

def pr(txt, cls):
    if not txt:
        return '<span class="pr none"></span>'
    return f'<span class="pr {cls}">{e(txt)}</span>'

def needs(s, t, age, word, meta, prt="", prc="", cls=""):
    return (f'<div class="rw {cls}"><span class="tile">{ic(s)}</span><span class="t ell">{e(t)}</span>'
            f'<span class="age">{e(age)}</span><span class="m ell"><b class="s-{s}">{e(word)}</b> · {e(meta)}</span>'
            f'{pr(prt, prc)}</div>')

def one(s, t, meta, age, prt="", prc="", cls="", lead=None):
    lead = lead if lead is not None else ic(s)
    p = f'<span class="pr {prc}">{e(prt)}</span>' if prt else "<span></span>"
    return (f'<div class="one {cls}">{lead}<span class="t ell">{e(t)}<i>{e(meta)}</i></span>{p}'
            f'<span class="age">{e(age)}</span></div>')

def bnav(n=4, on="inbox"):
    badge = f'<em class="bdg">{n}</em>' if n else ""
    return (f'<nav class="bn"><span class="bi {"on" if on=="inbox" else ""}">{G["inbox"]}<span>Inbox</span>{badge}</span>'
            f'<span class="bnew">{G["plus"]}<span>New</span></span>'
            f'<span class="bi {"on" if on=="search" else ""}">{G["search"]}<span>Search</span></span></nav>')

def normal_list():
    return ('<div class="list">' + sec("Needs you", 4) +
        needs("ask", "Fix map lasso on mobile", "2 min", "2 questions", "norban/web · Which gesture should close the filter sheet?", "#1234 ✓", "ok") +
        needs("fail", "Speed up e2e suite", "9 min", "API error", "norban/web · stopped after 14 min") +
        needs("block", "Listing cards v2", "21 min", "CI failed", "norban/web · lint: 2 errors in CardGrid.tsx", "#88 ✗ CI", "fail") +
        needs("done", "Triage sprint issues", "1 h", "Done", "norban/ops · Filed 6 issues, closed 2 duplicates", cls="dn") +
        sec("Working", 4, "1 waiting for a build slot") +
        one("work", "Babysit PR #4170", "git rebase", "3 min") +
        one("work", "Map perf", "2 sub · yarn vitest", "12 min") +
        one("wait", "Deploy check", "monitor: deploy", "9 min") +
        sec("Seen", 5, btn="Settle merged") +
        one("seen", "Lasso polygon perf", "norban/web", "14:20", "merged", "merged") +
        "</div>")

def stress_list():
    return ('<div class="list">' + sec("Needs you", 9999) +
        needs("ask", "Fix", "0 s", "1 question", "a · Ok?") +
        needs("fail", T140, "59 min", "API error", P59 + " · " + BR, "#12345 ✗ CI", "fail") +
        needs("block", URL, "12 d", "CI failed", "norban/web · " + URL, "merged", "merged") +
        needs("done", SV, "Mon 06:00", "Done", "norban/web · Översättningarna är klara; 0 fel, 1234 strängar.", "#7 ✓", "ok", cls="dn") +
        sec("Working", 9999, "7 waiting for a build slot on " + HOST) +
        one("work", "Fix", "starting", "0 s") +
        one("work", T140, "1234 sub · Bash " + BR, "999 h") +
        sec("Seen", 9999, btn="Settle merged") +
        one("seen", "x", P59, "Mon 06:00", "merged", "merged") +
        one("seen", URL, "a", "0 s") +
        sec("Snoozed", 0, "none", "col") +
        "</div>")

def phone(theme, body, cls=""):
    return f'<div class="ph t-{theme} {cls}">{body}</div>'

FIGS = []
def fig(fid, theme, cap, body, note="", cls=""):
    FIGS.append((fid, f'<figure class="fig" id="{fid}"><figcaption><a href="#{fid}">{e(cap)}</a></figcaption>'
                      f'{phone(theme, body, cls)}' + (f'<p class="note">{note}</p>' if note else "") + '</figure>'))

def group(title, rule, ids):
    return (f'<section class="grp"><h2>{e(title)}</h2><p class="rnote">{rule}</p><div class="row">' +
            "".join(h for fid, h in FIGS if fid in ids) + '</div></section>')

# ================= frames =================
# 1 inbox
def inbox(theme, stress=False):
    if stress:
        return (sbar() + inbox_header(conn=HOST) + strip((9999, 9999, 9999, 0, 9999)) +
                away("Mon 06:00", "1234 done · 999 PRs merged · 0 failed · host " + HOST + " offline since 05:12") +
                stress_list() + bnav(9999) + home())
    return sbar() + inbox_header() + strip() + away() + normal_list() + bnav() + home()

fig("inbox-light", "light", "Inbox · light", inbox("light"))
fig("inbox-dark", "dark", "Inbox · dark", inbox("dark"))
fig("inbox-stress", "light", "Inbox · stress", inbox("light", True),
    "Counts above 9999 show as 10k+. Titles end in one-line ellipsis; the age and PR badge never shrink.")

# 2 section switch
def section_switch(theme, stress=False):
    c = (9999, 9999, 9999, 0, 9999) if stress else (4, 4, 5, 3, 11)
    body = sbar() + inbox_header(install=False) + strip(c, on=2)
    lst = '<div class="list">' + sec("Seen", c[2], btn="Settle merged", cls="pin")
    lst += '<div class="grp-h">PR merged · 2</div>'
    if stress:
        lst += one("seen", T140, P59, "Mon 06:00", "merged", "merged") + one("seen", "x", "a", "0 s", "merged", "merged")
        lst += '<div class="grp-h">Other · 9997</div>' + one("seen", SV, "norban/web", "13:05") + one("seen", URL, "dotfiles", "11:48")
        lst += '<div class="more">Show 9993 more</div>'
        lst += sec("Snoozed", 0, "none", "col") + sec("Settled", 9999, "last 24 h", "col")
    else:
        lst += one("seen", "Lasso polygon perf", "norban/web", "14:20", "merged", "merged") + one("seen", "Card grid spacing", "norban/web", "14:31", "merged", "merged")
        lst += '<div class="grp-h">Other · 3</div>' + one("seen", "Nix flake update", "dotfiles", "13:05") + one("seen", "Bump Playwright to 1.58", "norban/web", "11:48") + one("seen", "Rewrite onboarding copy", "norban/web", "10:12")
        lst += sec("Snoozed", 3, "next: tomorrow 06:00", "exp") + one("snz", "Recipe scaler", "until tomorrow 06:00", "Tue") + sec("Settled", 11, "last 24 h", "col")
    lst += "</div>"
    return body + lst + bnav(9999 if stress else 4) + home() + ("" if stress else tap(1, 196, 120))

fig("switch-light", "light", "Section switch · tap Seen", section_switch("light"),
    "Tap 1 scrolls the one list to Seen. The strip follows the scroll, so the active segment always names the section at the top.")
fig("switch-dark", "dark", "Section switch · dark", section_switch("dark"))
fig("switch-stress", "light", "Section switch · stress", section_switch("light", True))

# 3 row actions
def swipe_rows(stress=False):
    a = needs("ask", "Fix map lasso on mobile" if not stress else T140, "2 min", "2 questions", "norban/web · Which gesture should close the filter sheet?", "#1234 ✓", "ok")
    settle = ('<div class="swp"><div class="under l set">' + ic("set", 18) + '<span>Settle</span></div>'
              '<div class="mv" style="transform:translateX(116px)">' +
              needs("done", "Triage sprint issues", "1 h", "Done", "norban/ops · Filed 6 issues, closed 2 duplicates", cls="dn") + '</div></div>')
    snooze = ('<div class="swp"><div class="under r snz"><span>Snooze…</span>' + ic("snz", 18) + '</div>'
              '<div class="mv" style="transform:translateX(-116px)">' +
              needs("block", "Listing cards v2", "21 min", "CI failed", "norban/web · lint: 2 errors in CardGrid.tsx", "#88 ✗ CI", "fail") + '</div></div>')
    blocked = ('<div class="swp"><div class="under l no"><span>Answer first</span></div>'
               '<div class="mv" style="transform:translateX(96px)">' + a + '</div></div>')
    if stress:
        settle = settle.replace("Settle</span>", "Settle when merged</span>")
    return blocked + settle + snooze

def row_actions(theme, stress=False):
    lst = '<div class="list">' + sec("Needs you", 4) + swipe_rows(stress)
    lst += needs("fail", "Speed up e2e suite", "9 min", "API error", "norban/web · stopped after 14 min")
    lst += sec("Working", 4) + one("work", "Map perf", "2 sub · yarn vitest", "12 min") + "</div>"
    toast = ('<div class="toast" style="bottom:106px"><span class="ell">Settled “Triage sprint issues”</span>'
             '<span class="tb">' + G["undo"] + 'Undo</span></div>')
    return sbar() + inbox_header() + strip() + lst + toast + bnav() + home()

fig("swipe-light", "light", "Row actions · swipe", row_actions("light"),
    "Right swipe settles (row 2), left swipe opens Snooze (row 3). On an asking row the swipe shows the reason and snaps back (row 1).")

def peek_sheet(theme, stress=False):
    title = T140 if stress else "Fix map lasso on mobile"
    proj = P59 if stress else "norban/web"
    brn = BR if stress else "fix/map-lasso-mobile"
    back = sbar() + inbox_header() + strip() + normal_list() + bnav() + home()
    sheet = ('<div class="scrim"></div><div class="sheet"><div class="grab"></div>'
             f'<div class="pk"><div class="pkh">{ic("ask")}<span class="pkt">{e(title)}</span></div>'
             f'<div class="pkm ell">{e(proj)} · {e(brn)} · #1234 ✓</div>'
             '<div class="pkb">Claude: The lasso no longer closes the sheet, and the tests pass. Before I wire the close gesture, two choices for you.</div>'
             '<div class="pkq"><b class="s-ask">2 questions</b> · Which gesture should close the filter sheet?</div>'
             '<div class="pkn">Peek does not mark the thread seen.</div></div>'
             '<div class="mi"><span>' + G["chev"].replace('m4 6 4 4 4-4', 'm6 4 4 4-4 4') + '</span><span>Open thread</span><span class="w">tap</span></div>'
             '<div class="mi dis"><span>' + ic("set") + '</span><span>Settle<small>Answer the question first</small></span><span class="w">swipe →</span></div>'
             '<div class="mi"><span>' + ic("snz") + '</span><span>Snooze…</span><span class="w">← swipe</span></div>'
             '<div class="mi"><span>' + ic("seen") + '</span><span>Mark unseen</span><span class="w"></span></div>'
             '<div class="mi"><span class="ck"></span><span>Select</span><span class="w"></span></div>'
             '<div class="mi"><span>' + G["x"] + '</span><span>Archive</span><span class="w"></span></div>'
             '</div>')
    return back + sheet

fig("peek-light", "light", "Row actions · long press", peek_sheet("light"),
    "Long press is peek plus the row's actions. Each action shows its swipe, so the sheet teaches the gestures.")
fig("peek-dark", "dark", "Row actions · long press · dark", peek_sheet("dark"))

def select_mode(theme, stress=False):
    hdr = ('<header class="ih sel">' + b44("x", "Cancel selection") + f'<span class="brand sm">{"9999" if stress else "2"} selected</span>'
           '<span class="sp"></span><span class="tbtn">Select all</span></header>')
    ck = '<span class="ck on"></span>'
    cko = '<span class="ck"></span>'
    lst = '<div class="list">' + sec("Seen", 9999 if stress else 5) + '<div class="grp-h">PR merged · 2</div>'
    if stress:
        lst += one("seen", T140, P59, "Mon 06:00", "merged", "merged", "picked", ck) + one("seen", "x", "a", "0 s", "merged", "merged", "picked", ck)
        lst += one("seen", SV, "norban/web", "13:05", lead=cko) + one("seen", URL, BR, "11:48", lead=cko)
    else:
        lst += one("seen", "Lasso polygon perf", "norban/web", "14:20", "merged", "merged", "picked", ck) + one("seen", "Card grid spacing", "norban/web", "14:31", "merged", "merged", "picked", ck)
        lst += '<div class="grp-h">Other · 3</div>' + one("seen", "Nix flake update", "dotfiles", "13:05", lead=cko) + one("seen", "Bump Playwright to 1.58", "norban/web", "11:48", lead=cko) + one("seen", "Rewrite onboarding copy", "norban/web", "10:12", lead=cko)
    lst += sec("Snoozed", 0 if stress else 3, cls="col") + "</div>"
    n = "9999" if stress else "2"
    bar = (f'<div class="selbar"><span class="bt p">Settle {n}</span><span class="bt">Snooze…</span>{b44("more","More actions")}</div>')
    return sbar() + hdr + lst + bar + home()

fig("select-light", "light", "Row actions · select (x, Shift+E)", select_mode("light"),
    "Select from the long-press sheet; then a tap toggles. The selection bar replaces the bottom bar. One toast, one Undo.")
fig("rowact-stress", "light", "Row actions · stress", row_actions("light", True).replace(
    normal_list(), ""), "")
FIGS.pop()
fig("rowact-stress", "light", "Row actions · stress", select_mode("light", True))
fig("peek-stress", "light", "Long press · stress", peek_sheet("light", True))

# 4 thread
def thread_header(title, state, proj, brn, prt, prc, tabs=True, chg="+42 −18", compact=False):
    if compact:
        return (f'<header class="tth"><div class="tr1">{b44("back","Back")}<span class="tt one-l">{e(title)}</span>'
                f'<span class="b44">{ic(state)}</span></div></header>')
    st = ic(state, 14, "spin" if state == "work" else "")
    chips = (f'<div class="chips" role="button" aria-label="Thread details"><span class="chip st">{st}<span>{LABEL[state]}</span></span>'
             f'<span class="chip pj"><span>{e(proj)}</span></span><span class="chip m brn"><span>{e(brn)}</span></span>' +
             (f'<span class="chip prc {prc}">{e(prt)}</span>' if prt else '<span class="chip prc">no PR</span>') + '</div>')
    tb = (f'<div class="tabs"><span class="on">Conversation</span><span class="ell">Changes<em>{e(chg)}</em></span><span>PR</span></div>' if tabs else "")
    return (f'<header class="tth"><div class="tr1">{b44("back","Back")}<span class="tt">{e(title)}</span>'
            f'{b44("more","Thread actions")}</div>{chips}{tb}</header>')

def composer(run=False, text="", placeholder="Message Claude…"):
    inner = f'<span class="in typed">{e(text)}<i class="caret"></i></span>' if text else f'<span class="in">{e(placeholder)}</span>'
    btn = f'<span class="send stop" aria-label="Stop">{G["stop"]}</span>' if run and not text else f'<span class="send" aria-label="Send">{G["send"]}</span>'
    return f'<div class="cmpw"><div class="cmp">{b44("plus","Attach image","att")}{inner}{btn}</div></div>'

def al(s, nm, txt, d, cls=""):
    return f'<div class="al {cls}">{ic(s,14)}<span class="nm">{e(nm)}</span><span class="ell">{e(txt)}</span><span class="d">{e(d)}</span></div>'

def thread_body(stress=False):
    if stress:
        return ('<div class="convo">'
            f'<div class="you prose">Deploy is red again, see {e(URL)}</div>'
            f'<div class="cl prose">The migration <code>db/migrate/20261114_add_lasso_polygon_to_listings_with_backfill_and_index.rb</code> fails on staging. Branch <code>{e(BR)}</code>.</div>'
            '<div class="act">' + al("fail", "Bash", "psql \"$STAGING_DATABASE_URL\" --no-psqlrc --quiet --tuples-only -c 'select count(*) from listings'", "59m 59s", "err") +
            '<div class="al hit">' + ic("set", 14) + '<span class="nm">Task</span><span class="s ell">1234 subagents · explore-every-migration-in-the-repository-and-summarise ▸</span><span class="d">999h</span></div>' +
            al("set", "Read", "x", "0s") + '<div class="al more">+ 9996 more tools</div></div>'
            '<div class="cl prose">x</div></div>')
    return ('<div class="convo">'
        '<div class="you prose">Map perf: the vitest suite for src/map takes 38 s. Find the slow tests and fix them.</div>'
        '<div class="cl prose">Three files take 31 of the 38 seconds. I sent three subagents to look at them in parallel.</div>'
        '<div class="act">' + al("set", "Read", "src/map/lasso.test.ts", "0.4s") +
        '<div class="al hit open">' + ic("work", 14) + '<span class="nm">Task</span><span class="s ell">3 subagents · profile slow tests ▾</span><span class="d">1m 40s</span></div>'
        '<div class="subw"><div class="subh ell">' + ic("set", 14) + 'lasso tests · 4 tools · done</div>'
        '<div class="subh ell">' + ic("work", 14) + 'tile cache tests · Bash yarn vitest tiles</div>'
        '<div class="subh ell">' + ic("set", 14) + 'snapshot tests · 2 tools · done</div></div>' +
        al("set", "Edit", "src/map/lasso.test.ts  +12 −40", "1s") + al("work", "Bash", "yarn vitest src/map", "38s") + '</div>'
        '<div class="sys">' + ic("work", 14) + '<span>Working · Bash yarn vitest src/map · 38 s</span></div></div>')

def thread(theme, stress=False):
    if stress:
        h = thread_header(T140, "fail", P59, BR, "", "", chg="+12345 −9876")
        return sbar() + h + thread_body(True) + composer() + home("raised0")
    h = thread_header("Map perf", "work", "norban/web", "perf/map-vitest", "#4182 ✓", "ok")
    return sbar() + h + thread_body() + composer(run=True) + home("raised0")

fig("thread-light", "light", "Thread · working", thread("light"),
    "Header: back, 2-line title, ⋯. One chip row (tap for details). Tabs 44 px. The send button becomes Stop while a run is live.")
fig("thread-dark", "dark", "Thread · dark", thread("dark"))

def kb(theme):
    rows = ["qwertyuiop", "asdfghjkl", "⇧zxcvbnm⌫", "123 ␣ ⏎"]
    out = ['<div class="kbd"><div class="kbs"><span>I</span><span>The</span><span>I’m</span></div>']
    for r in rows[:3]:
        out.append('<div class="kr">' + "".join(f'<span>{c}</span>' for c in r) + '</div>')
    out.append('<div class="kr last"><span class="w2">123</span><span class="w5">space</span><span class="w2">return</span></div></div>')
    return "".join(out)

def thread_kb(theme):
    h = thread_header("Map perf", "work", "", "", "", "", compact=True)
    body = ('<div class="convo">'
            '<div class="act">' + al("set", "Edit", "src/map/lasso.test.ts  +12 −40", "1s") + al("work", "Bash", "yarn vitest src/map", "38s") + '</div>'
            '<div class="sys">' + ic("work", 14) + '<span>Working · Bash yarn vitest src/map · 38 s</span></div></div>')
    return sbar() + h + body + composer(text="Also check the tile cache test, it flakes on CI") + kb(theme)

fig("thread-kb", "light", "Thread · keyboard open", thread_kb("light"),
    "With the keyboard open the header drops to one 44 px row and the tabs hide. The composer sits on the visualViewport bottom.")
fig("thread-stress", "light", "Thread · stress", thread("light", True))

# 5 question card
def ask_header(i, n, multi=False, collapse=True):
    dots = "".join(f'<i class="{"on" if k == i else ("done" if k < i else "")}"></i>' for k in range(min(n, 5)))
    more = f'<span class="qof">{i+1} of {n}</span>' if n > 5 else ""
    return (f'<div class="ah">{ic("ask",14)}<b>Question {i+1} of {n}</b>{more}<span class="dots">{dots}</span>'
            f'<span class="sp"></span>{"<span class=mt>multi-select</span>" if multi else ""}{b44("chev","Collapse card")}</div>')

def opt(a, d="", on=False, multi=False, other=False):
    cls = "op" + (" on" if on else "") + (" multi" if multi else "")
    dd = f'<span class="d">{e(d)}</span>' if d else ""
    return f'<div class="{cls}"><span class="mk"></span><span class="ot"><span class="a">{e(a)}</span>{dd}</span></div>'

def thread_top_for_card(title="Fix map lasso on mobile", proj="norban/web", brn="fix/map-lasso-mobile", prt="#1234 ✓", ctx="Claude: The lasso no longer closes the sheet, and the tests pass…"):
    return (sbar() + f'<header class="tth"><div class="tr1">{b44("back","Back")}<span class="tt">{e(title)}</span>{b44("more","Thread actions")}</div>'
            f'<div class="chips"><span class="chip st">{ic("ask",14)}<span>Asking</span></span><span class="chip pj"><span>{e(proj)}</span></span>'
            f'<span class="chip m brn"><span>{e(brn)}</span></span>' + (f'<span class="chip prc ok">{e(prt)}</span>' if prt else '<span class="chip prc">no PR</span>') + '</div></header>'
            f'<div class="peekc ell">{e(ctx)}</div>')

def abar(primary, enabled=True, left=None):
    l = f'<span class="bt">{e(left)}</span>' if left else ""
    return f'<div class="abar">{l}<span class="bt p {"" if enabled else "dis"}">{e(primary)}</span></div>'

def q1(theme):
    card = ('<div class="ask">' + ask_header(0, 2) +
            '<div class="abody"><div class="asker ell">Asked by subagent “explore gestures”</div>'
            '<div class="q">Which gesture should close the filter sheet?</div><div class="opts">' +
            opt("Swipe down", "Matches the iOS sheet the app already uses.", on=True) +
            opt("Tap outside", "Simpler; conflicts with the lasso on small screens.") +
            opt("Both", "Most forgiving; two code paths to test.") +
            opt("Other…", "Answer in your own words.") + '</div></div>' +
            abar("Answer · 1 of 2 left", False) + '</div>')
    return thread_top_for_card() + card + home("raised0") + tap(2, 330, 352)

fig("q1-light", "light", "Question 1 of 2 · from the push", q1("light"),
    "The push opens here (tap 1). Tap 2 picks Swipe down; a single-select pick moves to question 2 after 250 ms.")
fig("q1-dark", "dark", "Question 1 · dark", q1("dark"))

def q2(theme):
    card = ('<div class="ask">' + ask_header(1, 2, multi=True) +
            '<div class="abody"><div class="prev"><span class="ell"><b>1</b> Swipe down</span><span class="ed">Change</span></div>'
            '<div class="q">Which screens get the new sheet?</div><div class="opts">' +
            opt("Map", "The filter sheet on the map view.", on=True, multi=True) +
            opt("Listing", "The sort and filter sheet on listings.", multi=True) +
            opt("Saved searches", "The edit sheet for a saved search.", multi=True) +
            opt("Other…", "Answer in your own words.", multi=True) + '</div></div>' +
            abar("Answer 2 questions") + '</div>')
    return thread_top_for_card() + card + home("raised0") + tap(3, 330, 362) + tap(4, 330, 740)

fig("q2-dark", "dark", "Question 2 of 2 · multi-select", q2("dark"),
    "Tap 3 toggles Map. Tap 4 sends both answers. Multi-select never advances by itself.")
fig("q2-light", "light", "Question 2 · light", q2("light"))

def q_other(theme):
    card = ('<div class="ask">' + ask_header(0, 2) +
            '<div class="abody"><div class="q">Which gesture should close the filter sheet?</div><div class="opts">' +
            opt("Swipe down") + opt("Other…", on=True) + '</div>'
            '<div class="otherf"><span class="typed">Swipe down, but only on the handle<i class="caret"></i></span></div></div>' +
            abar("Next question") + '</div>')
    return (sbar() + thread_header("Fix map lasso on mobile", "ask", "", "", "", "", compact=True) + card + kb(theme))

fig("q-other", "light", "Question · Other with keyboard", q_other("light"),
    "Other opens the answer field inside the card, above the keyboard. The tiles above it shrink to one line each.")

def q_stress(theme):
    card = ('<div class="ask">' + ask_header(0, 9999) +
            '<div class="abody scroll"><div class="asker ell">Asked by subagent “explore-every-migration-in-the-repository-and-summarise-the-backfill-strategy”</div>'
            f'<div class="q clamp">{e(QLONG)}</div><span class="rd">Read all</span><div class="opts">' +
            opt("A", on=True) +
            opt("Make the column nullable and backfill in a background job", "Keeps writes open. Job log: " + URL) +
            opt(T140, "Short.") + opt("Other…", "Answer in your own words.") + '</div></div>'
            '<div class="below">↓ 2 more options</div>' +
            abar("Answer · 9998 left", False) + '</div>')
    return thread_top_for_card(T140, P59, BR, "", "Claude: " + URL) + card + home("raised0")

fig("q-stress", "light", "Question · stress", q_stress("light"),
    "The question clamps to 6 lines with Read all. Option labels never clamp; descriptions clamp to 2 lines. A hint shows options below the fold.")
fig("q-stress-dark", "dark", "Question · stress · dark", q_stress("dark"))

# 6 approval / effect
def approval(theme, stress=False):
    cmd = ("psql \"$STAGING_DATABASE_URL\" --no-psqlrc --quiet --tuples-only -c 'select count(*) from listings where lasso_polygon is null' && curl -fsS " + URL) if stress else "yarn test src/map/lasso --run"
    cwd = "~/src/" + P59 if stress else "~/src/norban-web"
    card = ('<div class="ask">' + f'<div class="ah">{ic("ask",14)}<b>Approval</b><span class="sp"></span>' +
            ('<span class="mt">+9999 more</span>' if stress else "") + f'{b44("chev","Collapse card")}</div>'
            '<div class="abody"><div class="asker ell">Asked by ' + ('subagent “explore-every-migration-in-the-repository”' if stress else 'the thread') + '</div>'
            '<div class="q">Allow Bash?</div>'
            f'<pre class="cmd">{e(cmd)}</pre><div class="kv ell">in {e(cwd)} · on {e(HOST if stress else "desk")}</div><div class="opts">' +
            opt("Allow", "Run it once.", on=True) + opt("Deny", "The agent is told no and continues.") + '</div></div>' +
            abar("Allow") + '</div>')
    title = T140 if stress else "Speed up e2e suite"
    return thread_top_for_card(title if stress else "Map perf", P59 if stress else "norban/web", BR if stress else "perf/map-vitest", "", "Claude: " + ("x" if stress else "I want to rerun the lasso tests with the new timeout.")) + card + home("raised0")

def effect(theme, stress=False):
    card = ('<div class="ask">' + f'<div class="ah">{ic("ask",14)}<b>Fyr action</b><span class="sp"></span>{b44("chev","Collapse card")}</div>'
            '<div class="abody"><div class="asker ell">Asked by the thread · the agent does not wait</div>'
            f'<div class="q">Merge {"#12345" if stress else "#4170"} with squash</div>'
            f'<pre>{{"pr": "{e(URL if stress else "https://github.com/NorbanDev/norban-web/pull/4170")}",\n "method": "squash"}}</pre><div class="opts">' +
            opt("Approve", "CI is green and the review approved. Fyr runs it; the agent does not wait.", on=True) +
            opt("Decline", "Nothing is run.") + '</div></div>' + abar("Approve") + '</div>')
    return thread_top_for_card("Babysit PR #4170" if not stress else SV, P59 if stress else "norban/web", BR if stress else "fix/babysit-4170", "#4170 ✓", "Claude: CI is green and the review is approved. I asked Fyr to merge.") + card + home("raised0")

fig("approval-light", "light", "Approval card", approval("light"),
    "Every Decision on the phone is: pick a tile, then one commit button. The button names the pick.")
fig("effect-dark", "dark", "Fyr action (effect) card", effect("dark"))
fig("effect-light", "light", "Fyr action · light", effect("light"))
fig("approval-stress", "light", "Approval · stress", approval("light", True),
    "Commands wrap anywhere in a mono block of at most 8 lines, then scroll inside the block.")

# 7 new thread
def new_thread(theme, state="empty"):
    stress = state == "stress"
    hdr = f'<header class="nh">{b44("x","Close")}<span class="nt">New thread</span><span class="sp"></span></header>'
    cap = ('<div class="capr"><span class="capb">' + G["clip"] + '<span>Paste screenshot</span></span>'
           '<span class="capb">' + G["photo"] + '<span>Photos</span></span></div>')
    thumbs = ""
    if state != "empty":
        n = 2 if stress else 1
        thumbs = '<div class="thumbs">' + "".join('<span class="thumb"><span class="shot"></span><span class="tx">' + G["x"] + '</span></span>' for _ in range(n)) + '</div>'
    txt = {"empty": '<span class="ph-t">What should Claude do? Optional with a screenshot.</span>',
           "pasted": '<span class="ph-t">What should Claude do? Optional with a screenshot.</span>',
           "stress": e(SV + ". Se " + URL)}[state]
    proj = P59 if stress else "norban/web"
    prof = "claude-deepseek-endpoint-second-account · opus 5.5 (1M context)" if stress else "claude · sonnet 5.5"
    defs = (f'<div class="defs"><div class="dl">Starts in <span>last used</span></div>'
            f'<span class="dchip"><span class="ell">{e(proj)}</span>{G["chev"]}</span>'
            f'<span class="dchip"><span class="ell">{e(prof)}</span>{G["chev"]}</span></div>')
    off = ('<div class="offl">Offline. Fyr keeps this in the outbox and starts it when desk is back.</div>' if stress else "")
    body = f'<div class="nbody">{cap}{thumbs}<div class="ta">{txt}</div>{defs}{off}</div>'
    btn = abar("Queue" if stress else "Start", state != "empty")
    taps = ""
    if state == "empty":
        taps = tap(2, 100, 132)
    elif state == "pasted":
        taps = tap(4, 330, 762)
    return sbar() + hdr + body + btn + home("raised0") + taps

fig("new-empty", "light", "New thread · open (tap 1 was New)", new_thread("light", "empty"),
    "Tap 2 is Paste screenshot. iOS asks once more (tap 3, the system Paste bubble). Android Chrome asks for clipboard access once per site.")
fig("new-pasted", "dark", "New thread · screenshot attached", new_thread("dark", "pasted"),
    "Text is optional; 0.2 counts screenshot-only starts. Project and profile are the last used. Tap 4 starts.")
fig("new-stress", "light", "New thread · stress, offline", new_thread("light", "stress"))

# 8 snooze sheet
def snooze(theme, stress=False):
    back = sbar() + inbox_header() + strip() + normal_list() + bnav() + home()
    title = (f"9999 threads" if stress else "“Fix map lasso on mobile”")
    rows = [("In 1 hour", "15:41"), ("In 3 hours", "17:41"), ("This evening", "today 18:00"),
            ("Tomorrow morning", "Sun 06:00"), ("Monday morning", "Mon 06:00")]
    if stress:
        rows[2] = ("This evening", "passed")
    items = "".join(f'<div class="mi{" dis" if w=="passed" else ""}"><span>{ic("snz")}</span><span>{e(l)}</span><span class="w">{e(w)}</span></div>' for l, w in rows)
    sub = (f'<div class="shsub ell">{e(T140)} and 9998 more</div>' if stress else "")
    sheet = (f'<div class="scrim"></div><div class="sheet"><div class="grab"></div><div class="shh ell">Snooze {e(title)}</div>{sub}{items}'
             '<div class="mi"><span></span><span>Pick a date and time…</span><span class="w"></span></div>'
             '<div class="shf"><span class="bt">Cancel</span></div></div>')
    return back + sheet

fig("snooze-light", "light", "Snooze sheet", snooze("light"),
    "The five presets from 0.2, with the exact time on the right. One tap snoozes with a 5 s Undo toast.")
fig("snooze-dark", "dark", "Snooze sheet · dark", snooze("dark"))
fig("snooze-stress", "light", "Snooze sheet · stress", snooze("light", True),
    "A preset in the past is shown disabled with its reason, not hidden, so positions never move.")

# 9 notifications
def notif(title, body, when="now"):
    return (f'<div class="nf"><span class="nfi">F</span><span class="nfc"><span class="nfh"><b>Fyr</b><span>{e(when)}</span></span>'
            f'<span class="nft ell">{e(title)}</span><span class="nfb">{e(body)}</span></span></div>')

def lock(theme, stress=False):
    if stress:
        ns = (notif(T140, "Question 1 of 9999 · " + QLONG) +
              notif(SV, "Failed · API error after 59 min · " + P59, "1 min ago") +
              notif("Fyr", "Since Mon 06:00: 1234 done · 999 blocked", "2 min ago") +
              notif("Fix", "Question · Ok?", "3 min ago"))
    else:
        ns = (notif("Fix map lasso on mobile", "2 questions · Which gesture should close the filter sheet?") +
              notif("Map perf", "Approval · Allow Bash: yarn test src/map/lasso --run", "1 min ago") +
              notif("Babysit PR #4170", "Fyr action · Merge #4170 with squash?", "4 min ago") +
              notif("Speed up e2e suite", "Failed · API error after 14 min · norban/web", "9 min ago") +
              notif("Fyr", "Since 14:02: 3 done · 1 blocked", "12 min ago"))
    t = "" if stress else tap(1, 330, 330)
    return (f'<div class="lock">{sbar()}<div class="lt">14:03</div><div class="ld">Saturday 14 November</div>'
            f'<div class="nfs">{ns}</div>{home()}</div>' + t)

fig("push-light", "light", "Notifications", lock("light"),
    "Title is the thread title. The body starts with the inbox meta word (2 questions, Approval, Fyr action, Failed), then the subject. Tap 1 opens the card.")
fig("push-dark", "dark", "Notifications · dark", lock("dark"))
fig("push-stress", "light", "Notifications · stress", lock("light", True),
    "The OS clamps the title. The state word leads the body, so it survives any title length.")

# 10 installed-app states
def states(theme, stress=False):
    host = HOST if stress else "desk"
    def lab(t):
        return f'<div class="slab">{e(t)}</div>'
    out = sbar()
    out += lab("Live · bell on · installed (Install hidden)") + inbox_header("on", host, install=False)
    out += lab("Browser tab · bell off · Install shown") + inbox_header("bell", host, install=True)
    out += lab("Notifications blocked in site settings") + inbox_header("off", host, install=False)
    out += lab("iOS Safari, not installed: no bell; a one-time row in the list") + inbox_header("bell", host, install=True).replace('<span class="b44 bell-bell"', '<span class="b44 hide"')
    out += '<div class="iosrow"><span class="ell">Add Fyr to the Home Screen to get notifications.</span><span class="tbtn">How</span></div>'
    out += lab("Reconnecting (after 3 s without the stream)") + inbox_header("on", host, install=False, extra="rc")
    out += '<div class="cstrip"><span class="ell">Reconnecting to ' + e(host) + '…</span></div>'
    out += lab("Offline (after 15 s)") + inbox_header("on", host, install=False, extra="off")
    out += ('<div class="cstrip off"><span class="ell">Offline · showing ' + ("Mon 06:00" if stress else "14:02") + ' · ' +
            ("9999 waiting to send" if stress else "2 waiting to send") + '</span><span class="tbtn">Outbox</span></div>')
    out += lab("New build (V0.13): reloads when the app is in the background")
    out += '<div class="cstrip"><span class="ell">Fyr updated. Reloads when you leave.</span><span class="tbtn">Now</span></div>'
    return out

fig("states-light", "light", "Installed-app states", states("light"),
    "The header holds brand, host dot, Install (until installed), bell and ⋯. Connection problems are a strip under the header, never a toast.")
fig("states-dark", "dark", "Installed-app states · dark", states("dark"))
fig("states-stress", "light", "Installed-app states · stress", states("light", True))

# ================= page =================
CSS = r"""
.t-light{--surface:#f6f7f8;--raised:#fff;--sunken:#edeff2;--select:#e6ecf3;--line:#dde1e6;--line-strong:#b4bcc6;
--ink:#111820;--ink-muted:#56616d;--ink-faint:#7d8894;--night:#14212d;--on-night:#f6f7f8;
--beam:#e8960f;--beam-mark:#b86e00;--beam-ink:#8f5300;--on-beam:#1a1204;--beam-wash:#fdf1dc;--beam-icon:#8f5300;
--signal-ink:#b42318;--harbour:#2f6fb0;--harbour-wash:#e7f0fa;--calm:#2e7d5b;--knock:var(--raised);--scrim:rgba(17,24,32,.35);
--kb:#d3d6dc;--key:#fff;--lockbg:linear-gradient(170deg,#c9d3de,#e9edf1);--nfbg:rgba(255,255,255,.82);color-scheme:light}
.t-dark{--surface:#0f1419;--raised:#161d24;--sunken:#0b0f13;--select:#1c2733;--line:#26303a;--line-strong:#3d4957;
--ink:#e8ecf0;--ink-muted:#9aa6b2;--ink-faint:#6f7b87;--night:#2a3a4a;--on-night:#f6f7f8;
--beam:#f5a524;--beam-mark:#f5a524;--beam-ink:#f5b54a;--on-beam:#1a1204;--beam-wash:#2a2112;--beam-icon:#f5a524;
--signal-ink:#ff8a7d;--harbour:#6fa8e8;--harbour-wash:#15253a;--calm:#5cc49a;--knock:var(--raised);--scrim:rgba(0,0,0,.55);
--kb:#2b2f35;--key:#4a4f57;--lockbg:linear-gradient(170deg,#141a22,#05070a);--nfbg:rgba(40,46,54,.86);color-scheme:dark}
:root{--ui:"Geist",system-ui,sans-serif;--mono:"Geist Mono",ui-monospace,monospace}
*{box-sizing:border-box}
html,body{margin:0;background:#80878f}
body{padding:28px 40px 60px;color:#fff;font-family:var(--ui)}
.doc h1{margin:0;font:600 22px/28px var(--ui)}
.doc p,.rnote{margin:6px 0 0;font:400 14px/20px var(--ui);max-width:1000px}
.grp h2{margin:36px 0 0;font:600 17px/24px var(--ui)}
.row{display:flex;flex-wrap:wrap;gap:28px;align-items:flex-start;margin-top:14px}
.fig{margin:0;width:390px}
.fig figcaption{font:600 12px/16px var(--ui);letter-spacing:.04em;text-transform:uppercase;margin-bottom:8px}
.fig figcaption a{color:#fff;text-decoration:none}
.note{margin:8px 0 0;font:400 13px/18px var(--ui)}
body:has(.fig:target){padding:0;background:#000}
body:has(.fig:target) .doc,body:has(.fig:target) .grp h2,body:has(.fig:target) .rnote,
body:has(.fig:target) .fig:not(:target),body:has(.fig:target) .fig:target figcaption,body:has(.fig:target) .fig:target .note{display:none}
body:has(.fig:target) .row{margin:0}
body:has(.fig:target) .ph{border-radius:0;box-shadow:none}
@media (max-width:440px){body{padding:16px 0 40px}.doc,.grp h2,.rnote,.fig figcaption,.note{padding:0 16px}}

.ph{width:390px;height:844px;position:relative;overflow:hidden;display:flex;flex-direction:column;border-radius:30px;
 box-shadow:0 10px 40px rgba(0,0,0,.25);background:var(--surface);color:var(--ink);font-family:var(--ui);
 -webkit-font-smoothing:antialiased;font-size:15px}
.s-ask{color:var(--beam-icon)}.s-fail{color:var(--signal-ink)}.s-block{color:var(--harbour)}.s-done{color:var(--ink)}
.s-work,.s-wait,.s-seen,.s-snz{color:var(--ink-muted)}.s-set{color:var(--calm)}
svg.ic{display:block;flex:none}
.ell{white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-width:0}
.prose{overflow-wrap:anywhere;min-width:0}
code{font-family:var(--mono);font-size:.9em}
b{font-weight:600}
.sp{flex:1}
.spin{animation:spin 1.2s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}

.sb{flex:none;height:47px;display:flex;align-items:flex-end;justify-content:space-between;padding:0 30px 9px 40px;font:600 15px/20px var(--ui);background:var(--raised)}
.sb .r{font:500 12px/16px var(--mono)}
.hm{flex:none;height:34px;display:grid;place-items:center;background:var(--raised)}
.hm.raised0{background:var(--raised)}
.hm i{width:134px;height:5px;border-radius:3px;background:var(--ink)}
.tap{position:absolute;z-index:9;width:30px;height:30px;margin:-15px 0 0 -15px;border-radius:50%;background:#c026d3;color:#fff;
 font:700 15px/30px var(--ui);text-align:center;box-shadow:0 0 0 4px rgba(192,38,211,.3)}

.b44{width:44px;height:44px;display:grid;place-items:center;border-radius:12px;color:var(--ink-muted);flex:none}
.b44.hide{visibility:hidden;width:0}
.bell-on{color:var(--ink)}
.tbtn{flex:none;height:44px;padding:0 12px;display:grid;place-items:center;font:600 15px/20px var(--ui);color:var(--harbour)}

.ih{flex:none;display:flex;align-items:center;height:52px;padding:0 4px 0 16px;background:var(--raised);min-width:0}
.ih.sel{padding-left:4px;border-bottom:1px solid var(--line)}
.brand{font:600 20px/24px var(--ui);letter-spacing:-.015em;flex:none}
.brand.sm{font-size:17px}
.conn{display:flex;min-width:0;align-items:center;gap:6px;margin-left:10px;font:400 13px/16px var(--mono);color:var(--ink-muted);flex:0 1 auto}
.conn i{width:8px;height:8px;border-radius:50%;background:var(--calm);flex:none}
.conn i.rc{background:transparent;border:1.5px dashed var(--ink-muted)}
.conn i.off{background:transparent;border:1.5px solid var(--ink-muted)}

.strip{flex:none;display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:4px;padding:6px 8px;background:var(--sunken);border-block:1px solid var(--line);--knock:var(--sunken)}
.seg{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:4px;height:56px;border-radius:12px;color:var(--ink-muted);min-width:0}
.seg .c{display:flex;align-items:center;gap:4px;max-width:100%}
.seg .n{font:500 15px/20px var(--mono);font-variant-numeric:tabular-nums;color:var(--ink);white-space:nowrap}
.seg.att .n{background:var(--beam);color:var(--on-beam);border-radius:10px;padding:0 5px}
.seg .l{font:500 12px/14px var(--ui)}
.seg.on{background:var(--raised);color:var(--ink);box-shadow:0 0 0 1px var(--line);--knock:var(--raised)}

.away{flex:none;display:flex;align-items:center;height:44px;margin:8px 8px 0;padding:0 0 0 12px;border-radius:12px;background:var(--harbour-wash);font:400 13px/18px var(--ui);min-width:0}
.away>.ell{flex:1}
.list{flex:1;min-height:0;overflow:hidden;padding:0 8px 8px;background:var(--raised)}
.ph:has(.away) .list{margin-top:0}
.sec{display:flex;align-items:center;gap:8px;height:44px;padding:8px 8px 0;font:500 13px/16px var(--ui);color:var(--ink-muted);min-width:0}
.sec .n{font:400 13px/16px var(--mono);font-variant-numeric:tabular-nums;flex:none}
.sec .h{margin-left:auto;font-size:12px;max-width:62%}
.sec:has(.sbtn){height:56px}
.sec .sbtn{margin-left:auto;height:44px;padding:0 12px;border:1px solid var(--line);border-radius:10px;display:grid;place-items:center;font:500 13px/16px var(--ui);color:var(--ink)}
.sec.col::after{content:"▸";margin-left:8px;font-size:11px}
.sec.col .h{margin-left:auto}
.sec.col:not(:has(.h))::after{margin-left:auto}
.sec.exp::after{content:"▾";margin-left:8px;font-size:11px}
.sec.pin{background:var(--raised)}
.grp-h{height:32px;padding:10px 8px 0 46px;font:500 12px/16px var(--ui);color:var(--ink-muted)}
.more{height:44px;padding:0 8px 0 46px;font:500 13px/44px var(--ui);color:var(--harbour)}

.rw{display:grid;grid-template-columns:32px minmax(0,1fr) auto;column-gap:12px;grid-template-rows:22px 20px;align-content:center;height:68px;padding:0 8px;border-radius:12px;background:var(--raised);--knock:var(--raised)}
.tile{grid-row:1/3;width:32px;height:32px;border-radius:9px;background:var(--sunken);display:grid;place-items:center;align-self:center;--knock:var(--sunken)}
.rw .t{font:500 15px/22px var(--ui)}
.rw.dn .t{font-weight:600}
.rw .age{font:400 12px/22px var(--mono);font-variant-numeric:tabular-nums;color:var(--ink-muted);text-align:right;white-space:nowrap}
.rw .m{font:400 13px/20px var(--ui);color:var(--ink-muted)}
.rw .pr{justify-self:end;align-self:center}
.pr{display:inline-block;height:20px;padding:0 7px;border-radius:10px;border:1px solid var(--line);font:500 12px/18px var(--mono);color:var(--ink-muted);white-space:nowrap;max-width:96px;overflow:hidden;text-overflow:ellipsis}
.pr.ok{color:var(--calm)}.pr.fail{color:var(--signal-ink);border-color:var(--signal-ink)}.pr.merged{background:var(--sunken)}
.pr.none{display:none}
.one{display:grid;grid-template-columns:28px minmax(0,1fr) auto auto;column-gap:10px;align-items:center;height:48px;padding:0 8px;border-radius:10px;font:400 12px/16px var(--ui);color:var(--ink-muted)}
.one>.ic,.one>.ck{justify-self:center}
.one .t{font:500 15px/20px var(--ui);color:var(--ink-muted)}
.one .t i{font:400 12px/16px var(--mono);font-style:normal;margin-left:8px}
.one .age{font:400 12px/16px var(--mono);font-variant-numeric:tabular-nums;text-align:right;white-space:nowrap;min-width:40px}
.one.picked{background:var(--select)}
.ck{width:22px;height:22px;border:2px solid var(--line-strong);border-radius:6px;display:inline-block;flex:none}
.ck.on{background:var(--harbour);border-color:var(--harbour);box-shadow:inset 0 0 0 3px var(--raised)}
.one.picked .ck.on{box-shadow:inset 0 0 0 3px var(--select)}

.swp{position:relative;border-radius:12px;overflow:hidden;margin:2px 0}
.swp .mv{position:relative;box-shadow:0 0 0 1px var(--line),0 4px 14px rgba(0,0,0,.12);border-radius:12px}
.under{position:absolute;inset:0;display:flex;align-items:center;gap:8px;padding:0 18px;font:600 15px/20px var(--ui);border-radius:12px}
.under.l{justify-content:flex-start}.under.r{justify-content:flex-end}
.under.set{background:var(--calm);color:#fff}.under.set .ic{color:#fff}
.under.snz{background:var(--night);color:var(--on-night)}.under.snz .ic{color:var(--on-night)}
.under.no{background:var(--sunken);color:var(--ink-muted);font:500 12px/16px var(--ui);padding:0 12px;width:100%;white-space:nowrap}

.bn{flex:none;display:grid;grid-template-columns:1fr 1fr 1fr;align-items:center;height:60px;background:var(--raised);border-top:1px solid var(--line)}
.bi{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:2px;height:56px;font:500 12px/14px var(--ui);color:var(--ink-muted);position:relative}
.bi.on{color:var(--ink)}
.bdg{position:absolute;top:3px;left:calc(50% + 8px);font:500 12px/18px var(--mono);font-style:normal;background:var(--beam);color:var(--on-beam);border-radius:9px;padding:0 5px}
.bnew{justify-self:center;display:flex;align-items:center;gap:6px;height:44px;padding:0 18px 0 14px;border-radius:22px;background:var(--night);color:var(--on-night);font:600 15px/20px var(--ui)}

.toast{position:absolute;z-index:5;left:12px;right:12px;display:flex;align-items:center;gap:12px;min-height:52px;padding:4px 4px 4px 16px;border-radius:14px;background:var(--night);color:var(--on-night);font:500 14px/20px var(--ui);box-shadow:0 10px 30px rgba(0,0,0,.3)}
.toast>.ell{flex:1}
.toast .tb{margin-left:auto;display:flex;align-items:center;gap:6px;height:44px;padding:0 14px;border-radius:10px;border:1px solid rgba(255,255,255,.3);font:600 14px/20px var(--ui);flex:none}

.scrim{position:absolute;inset:0;background:var(--scrim);z-index:6}
.sheet{position:absolute;z-index:7;left:0;right:0;bottom:0;max-height:88%;display:flex;flex-direction:column;background:var(--raised);border-radius:20px 20px 0 0;padding:8px 0 34px;box-shadow:0 -10px 40px rgba(0,0,0,.25);--knock:var(--raised);overflow:hidden}
.grab{width:36px;height:5px;border-radius:3px;background:var(--line-strong);margin:0 auto 10px;flex:none}
.shh{padding:2px 20px 4px;font:600 17px/24px var(--ui)}
.shsub{padding:0 20px 6px;font:400 13px/18px var(--ui);color:var(--ink-muted)}
.mi{display:grid;grid-template-columns:24px minmax(0,1fr) auto;gap:14px;align-items:center;min-height:52px;padding:4px 20px;font:500 16px/22px var(--ui);color:var(--ink);flex:none}
.mi>span:first-child{display:grid;place-items:center;color:var(--ink-muted)}
.mi .w{font:400 13px/16px var(--mono);color:var(--ink-muted);white-space:nowrap}
.mi.dis{color:var(--ink-muted)}
.mi.dis .ic{color:var(--ink-muted)}
.mi small{display:block;font:400 13px/18px var(--ui);color:var(--ink-muted)}
.shf{display:flex;padding:8px 16px 0}.shf .bt{flex:1}
.pk{padding:0 20px 10px;border-bottom:1px solid var(--line);margin-bottom:4px}
.pkh{display:flex;gap:10px;align-items:flex-start}.pkh .ic{margin-top:4px}
.pkt{font:600 17px/24px var(--ui);display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere;min-width:0}
.pkm{margin:4px 0 0 26px;font:400 12px/16px var(--mono);color:var(--ink-muted)}
.pkb{margin:10px 0 0;font:400 15px/22px var(--ui);display:-webkit-box;-webkit-line-clamp:3;-webkit-box-orient:vertical;overflow:hidden}
.pkq{margin-top:6px;font:400 14px/20px var(--ui);color:var(--ink-muted);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.pkn{margin-top:6px;font:400 12px/16px var(--ui);color:var(--ink-muted)}

.selbar{flex:none;display:flex;gap:8px;align-items:center;padding:8px 8px 8px 12px;background:var(--raised);border-top:1px solid var(--line)}
.selbar .bt.p{flex:1}
.bt{height:52px;border-radius:14px;display:grid;place-items:center;padding:0 18px;border:1px solid var(--line-strong);font:600 16px/22px var(--ui);color:var(--ink);background:var(--raised);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-width:0}
.bt.p{background:var(--night);border-color:var(--night);color:var(--on-night)}
.bt.dis{opacity:.5}

.tth{flex:none;background:var(--raised);border-bottom:1px solid var(--line);--knock:var(--raised)}
.tr1{display:flex;align-items:center;min-height:52px;padding:0 4px}
.tt{flex:1;min-width:0;font:600 17px/22px var(--ui);letter-spacing:-.01em;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere;padding:4px 0}
.tt.one-l{-webkit-line-clamp:1;display:block;white-space:nowrap;text-overflow:ellipsis}
.chips{display:flex;gap:6px;align-items:center;height:44px;padding:0 16px 4px;min-width:0}
.chip{display:inline-flex;align-items:center;gap:6px;height:28px;padding:0 10px;border-radius:14px;background:var(--sunken);font:500 13px/16px var(--ui);color:var(--ink-muted);white-space:nowrap;min-width:0;flex:0 1 auto;overflow:hidden;--knock:var(--sunken)}
.chip span{overflow:hidden;text-overflow:ellipsis;min-width:0}
.chip.st{flex:none;background:transparent;border:1px solid var(--line-strong);color:var(--ink);--knock:var(--raised)}
.chip.st span{color:var(--ink)}
.chip.m{font-family:var(--mono);font-weight:400;font-size:12px}
.chip.pj{max-width:36%}
.chip.brn{flex:1 1 0;max-width:max-content;min-width:48px}
.chip.prc{flex:none;font:500 12px/16px var(--mono)}
.chip.prc.ok{color:var(--calm)}
.tabs{display:flex;padding:0 8px;min-width:0}
.tabs span{height:44px;padding:0 12px;display:flex;align-items:center;font:500 15px/20px var(--ui);color:var(--ink-muted);border-bottom:2px solid transparent;white-space:nowrap;flex:none}
.tabs span.ell{flex:0 1 auto;display:block;line-height:44px}
.tabs span.on{color:var(--ink);border-color:var(--ink)}
.tabs em{font:400 12px/16px var(--mono);font-style:normal;margin-left:6px}

.convo{flex:1;min-height:0;overflow:hidden;display:flex;flex-direction:column;justify-content:flex-end;gap:12px;padding:12px 16px}
.you{align-self:flex-end;max-width:85%;padding:10px 14px;border-radius:18px 18px 4px 18px;background:var(--night);color:var(--on-night);font:400 15px/22px var(--ui);flex:none}
.cl{font:400 16px/24px var(--ui);flex:none}
.act{border:1px solid var(--line);border-radius:12px;background:var(--raised);padding:4px 0;flex:none;--knock:var(--raised);min-width:0}
.al{display:grid;grid-template-columns:14px 40px minmax(0,1fr) 52px;column-gap:8px;align-items:center;height:32px;padding:0 12px;font:400 12px/16px var(--mono);color:var(--ink-muted)}
.al.hit{height:44px}
.al.hit.open{background:var(--surface)}
.al .nm{color:var(--ink);font:500 13px/16px var(--ui)}
.al .s{font-family:var(--ui)}
.al .d{text-align:right;font-variant-numeric:tabular-nums;white-space:nowrap;overflow:hidden}
.al.err .ell{color:var(--signal-ink)}
.al.more{display:block;height:44px;line-height:44px;font:500 13px/44px var(--ui);color:var(--harbour)}
.subw{margin:2px 12px 4px 34px;border-left:2px solid var(--line);padding-left:10px}
.subh{display:flex;align-items:center;gap:8px;height:30px;font:400 13px/16px var(--ui);color:var(--ink-muted)}
.sys{display:flex;align-items:center;gap:8px;font:400 13px/18px var(--ui);color:var(--ink-muted);flex:none;min-width:0}

.cmpw{flex:none;padding:8px 8px 6px;background:var(--surface)}
.cmp{display:flex;align-items:flex-end;gap:2px;min-height:54px;padding:4px;border:1px solid var(--line-strong);border-radius:27px;background:var(--raised)}
.cmp .att{border-radius:22px}
.cmp .in{flex:1;min-width:0;padding:11px 4px;font:400 16px/22px var(--ui);color:var(--ink-muted)}
.cmp .in.typed{color:var(--ink);overflow-wrap:anywhere}
.caret{display:inline-block;width:2px;height:20px;background:var(--harbour);vertical-align:-4px;margin-left:1px}
.send{width:44px;height:44px;border-radius:22px;background:var(--night);color:var(--on-night);display:grid;place-items:center;flex:none}

.kbd{flex:none;height:291px;background:var(--kb);padding:6px 3px 40px;display:flex;flex-direction:column;gap:11px}
.kbs{display:flex;justify-content:space-around;height:36px;align-items:center;font:400 16px/20px var(--ui);color:var(--ink)}
.kr{display:flex;justify-content:center;gap:6px;padding:0 3px}
.kr span{flex:0 0 33px;height:42px;border-radius:5px;background:var(--key);display:grid;place-items:center;font:400 20px/1 var(--ui);color:var(--ink);box-shadow:0 1px 0 rgba(0,0,0,.25)}
.kr.last .w2{flex:0 0 88px;font-size:15px}.kr.last .w5{flex:1 1 auto;font-size:15px}

.peekc{flex:none;padding:10px 16px;font:400 14px/20px var(--ui);color:var(--ink-muted);background:var(--surface)}
.ask{flex:1;min-height:0;display:flex;flex-direction:column;background:var(--raised);border-top:1px solid var(--line-strong);border-radius:18px 18px 0 0;box-shadow:0 -8px 24px rgba(0,0,0,.08);overflow:hidden;--knock:var(--raised);position:relative}
.ah{flex:none;display:flex;align-items:center;gap:8px;height:48px;padding:0 4px 0 16px;background:var(--beam-wash);--knock:var(--beam-wash);font:400 13px/18px var(--ui);color:var(--ink-muted);min-width:0}
.ah b{color:var(--beam-ink);flex:none;font-weight:600}
.ah .mt{font:400 12px/16px var(--ui);flex:none}
.ah .qof{display:none}
.dots{display:flex;gap:6px;margin-left:4px}
.dots i{width:8px;height:8px;border-radius:50%;border:1.5px solid var(--beam-ink)}
.dots i.on{background:var(--beam-ink)}
.dots i.done{background:var(--ink-muted);border-color:var(--ink-muted)}
.abody{flex:1;min-height:0;overflow:hidden;padding:12px 16px;display:flex;flex-direction:column;gap:10px}
.asker{font:400 13px/18px var(--ui);color:var(--ink-muted);flex:none}
.q{font:500 18px/26px var(--ui);overflow-wrap:anywhere;flex:none}
.q.clamp{display:-webkit-box;-webkit-line-clamp:6;-webkit-box-orient:vertical;overflow:hidden}
.rd{flex:none;height:44px;margin:-10px 0 -6px;display:flex;align-items:center;font:500 15px/20px var(--ui);color:var(--harbour)}
.opts{display:flex;flex-direction:column;gap:8px;flex:none}
.op{display:grid;grid-template-columns:22px minmax(0,1fr);column-gap:12px;align-items:start;min-height:56px;padding:15px 14px 13px;border:1px solid var(--line);border-radius:12px;background:var(--raised)}
.op .mk{width:22px;height:22px;border-radius:50%;border:2px solid var(--line-strong);margin-top:0}
.op.multi .mk{border-radius:6px}
.op.on{border-color:var(--harbour);background:var(--harbour-wash);box-shadow:0 0 0 1px var(--harbour)}
.op.on .mk{border-color:var(--harbour);background:var(--harbour);box-shadow:inset 0 0 0 4px var(--harbour-wash)}
.op.on.multi .mk{box-shadow:inset 0 0 0 3px var(--harbour-wash)}
.ot{min-width:0}
.op .a{display:block;font:500 16px/22px var(--ui);overflow-wrap:anywhere}
.op .d{display:-webkit-box;margin-top:2px;font:400 13px/18px var(--ui);color:var(--ink-muted);-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere}
.prev{flex:none;display:flex;align-items:center;gap:8px;height:44px;padding:0 4px 0 12px;border-radius:10px;background:var(--sunken);font:400 14px/20px var(--ui);color:var(--ink-muted);min-width:0}
.prev>.ell{flex:1}
.prev .ed{flex:none;height:44px;padding:0 10px;display:grid;place-items:center;font:500 14px/20px var(--ui);color:var(--harbour)}
.otherf{flex:none;padding:12px 14px;border:1px solid var(--harbour);border-radius:12px;font:400 16px/22px var(--ui)}
.below{flex:none;height:32px;text-align:center;font:500 13px/32px var(--ui);color:var(--ink-muted);background:linear-gradient(transparent,var(--raised) 40%);margin-top:-32px;position:relative;border-top:1px dashed var(--line)}
.abar{flex:none;display:flex;gap:8px;padding:8px 12px;border-top:1px solid var(--line);background:var(--raised)}
.abar .bt.p{flex:1}
pre{margin:0;flex:none;padding:10px 12px;border-radius:10px;background:var(--sunken);font:400 13px/19px var(--mono);white-space:pre-wrap;overflow-wrap:anywhere;color:var(--ink);max-height:calc(19px * 8 + 20px);overflow:hidden}
.kv{font:400 12px/16px var(--mono);color:var(--ink-muted);flex:none;margin-top:-4px}

.nh{flex:none;display:flex;align-items:center;height:52px;padding:0 4px;background:var(--raised);border-bottom:1px solid var(--line)}
.nt{font:600 17px/22px var(--ui)}
.nbody{flex:1;min-height:0;overflow:hidden;padding:16px;display:flex;flex-direction:column;gap:14px;background:var(--surface)}
.capr{display:grid;grid-template-columns:1fr 1fr;gap:10px;flex:none}
.capb{display:flex;align-items:center;justify-content:center;gap:8px;height:64px;border:1.5px dashed var(--line-strong);border-radius:14px;background:var(--raised);font:500 15px/20px var(--ui);color:var(--ink)}
.capb svg{color:var(--ink-muted);flex:none}
.thumbs{display:flex;gap:10px;flex:none}
.thumb{position:relative;width:104px;height:150px;border-radius:12px;border:1px solid var(--line);background:var(--raised);overflow:hidden}
.thumb .shot{position:absolute;inset:0;background:linear-gradient(180deg,var(--sunken) 0 16%,var(--raised) 16%)}
.thumb .shot::after{content:"";position:absolute;left:10px;right:30px;top:36px;height:8px;border-radius:2px;background:var(--line);box-shadow:0 16px 0 var(--line),14px 32px 0 var(--line),0 48px 0 var(--line)}
.thumb .tx{position:absolute;top:0;right:0;width:44px;height:44px;display:grid;place-items:center}
.thumb .tx svg{width:24px;height:24px;padding:4px;border-radius:12px;background:var(--night);color:var(--on-night)}
.ta{flex:none;min-height:96px;padding:12px 14px;border:1px solid var(--line-strong);border-radius:14px;background:var(--raised);font:400 16px/22px var(--ui);overflow-wrap:anywhere}
.ph-t{color:var(--ink-muted)}
.defs{display:flex;flex-direction:column;gap:8px;flex:none;min-width:0}
.dl{font:500 13px/18px var(--ui);color:var(--ink-muted)}
.dl span{font-weight:400;margin-left:6px}
.dchip{display:flex;align-items:center;gap:8px;height:48px;padding:0 14px;border:1px solid var(--line);border-radius:12px;background:var(--raised);font:500 15px/20px var(--ui);min-width:0}
.dchip .ell{flex:1}
.dchip svg{flex:none;color:var(--ink-muted)}
.offl{flex:none;padding:10px 12px;border-radius:12px;background:var(--harbour-wash);font:400 14px/20px var(--ui)}

.lock{position:absolute;inset:0;display:flex;flex-direction:column;background:var(--lockbg);color:var(--ink)}
.lock .sb,.lock .hm{background:transparent}
.lt{margin-top:24px;text-align:center;font:200 84px/1 var(--ui);letter-spacing:-.02em}
.ld{text-align:center;font:500 17px/24px var(--ui);margin-top:6px}
.nfs{flex:1;display:flex;flex-direction:column;gap:8px;justify-content:flex-end;padding:0 10px 16px;min-height:0}
.nf{display:grid;grid-template-columns:38px minmax(0,1fr);gap:10px;padding:12px 14px;border-radius:20px;background:var(--nfbg);backdrop-filter:blur(20px)}
.nfi{width:38px;height:38px;border-radius:9px;background:#14212d;color:#f6f7f8;display:grid;place-items:center;font:600 18px/1 var(--ui)}
.nfc{display:flex;flex-direction:column;min-width:0}
.nfh{display:flex;justify-content:space-between;font:400 13px/18px var(--ui);color:var(--ink-muted)}
.nfh b{font-weight:500}
.nft{font:600 15px/20px var(--ui)}
.nfb{font:400 15px/20px var(--ui);display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere}

.slab{flex:none;padding:10px 16px 4px;font:500 12px/16px var(--mono);color:var(--ink-muted);background:var(--surface)}
.iosrow,.cstrip{flex:none;display:flex;align-items:center;min-height:44px;padding:0 4px 0 16px;font:400 14px/20px var(--ui);background:var(--harbour-wash);min-width:0}
.iosrow>.ell,.cstrip>.ell{flex:1}
.cstrip.off{background:var(--sunken)}
"""

ids = lambda *a: set(a)
groups = [
 ("1 · Inbox", "The rail becomes the count strip: the same five sections, icons and counts, in the same order, under the header. The list stays one list (U1). Rows grow from 52 to 68 px and from 30 to 48 px.",
  ids("inbox-light", "inbox-dark", "inbox-stress")),
 ("2 · Section switch (g1 to g5)", "A strip segment scrolls the one list to its section and expands it if collapsed. It never filters, so counts and order stay explainable (U12).",
  ids("switch-light", "switch-dark", "switch-stress")),
 ("3 · Row actions (e, s, #, u, x, Shift+E, Space)", "One swipe per direction, long press for peek and every action, selection mode for bulk. Every deferred action ends in a 5 s Undo toast (U7).",
  ids("swipe-light", "peek-light", "peek-dark", "select-light", "rowact-stress", "peek-stress")),
 ("4 · Thread", "Back, title and ⋯ on top; chips and tabs below; the composer at the bottom. The keyboard collapses the header to one row.",
  ids("thread-light", "thread-dark", "thread-kb", "thread-stress")),
 ("5 · Question card (A2)", "The docked card fills the thread below the header. One question per step, full-width tiles, one sticky commit button.",
  ids("q1-light", "q1-dark", "q2-dark", "q2-light", "q-other", "q-stress", "q-stress-dark")),
 ("6 · Approval and Fyr action cards", "The same card: pick a tile, then the commit button that names the pick.",
  ids("approval-light", "effect-dark", "effect-light", "approval-stress")),
 ("7 · New thread from a screenshot (A3)", "Paste and Photos sit on top. Project and profile default to the last used on this device.",
  ids("new-empty", "new-pasted", "new-stress")),
 ("8 · Snooze sheet (s)", "A bottom sheet with the five presets and exact times.",
  ids("snooze-light", "snooze-dark", "snooze-stress")),
 ("9 · Notifications", "Push only for asking and failed; done and blocked only as a batch after 10 min away (U6).",
  ids("push-light", "push-dark", "push-stress")),
 ("10 · Installed-app states", "Install, bell, connection and update states in the inbox header.",
  ids("states-light", "states-dark", "states-stress")),
]

page = ['<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">'
        '<title>Fyr phone mocks</title><link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>'
        '<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Geist:wght@200;400;500;600;700&amp;family=Geist+Mono:wght@400;500&amp;display=swap">'
        f'<style>{CSS}</style></head><body>'
        '<header class="doc"><h1>Fyr on the phone</h1><p>Mocks for the translation rules in README.md, at 390 × 844 with iOS safe areas (47 px top, 34 px bottom). '
        'Rail tokens, Geist, and the nine state icons. Magenta numbered dots are tap counts, not UI. Open <code>mobile.html#q1-light</code> (any frame id) to see one frame alone.</p></header>']
for title, rule, s in groups:
    page.append(group(title, rule, s))
page.append('</body></html>')
open(OUT, "w").write("".join(page))
print("frames:", len(FIGS))
print(" ".join(f for f, _ in FIGS))
