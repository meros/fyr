#!/usr/bin/env python3
"""Builds the V0.164 mockups: frames/*.html and the boards i1.html and i2.html.

The frames are the real front: base/*.html is the DOM of web/dist in mock mode (?mock=rail, origin/main dffbf23,
17 Nov) at 1280 and 390 px, with its scripts removed, and app.css is the built stylesheet. This script only swaps
the parts V0.164 changes; the new CSS is rogue.css. Run: python3 design/rogue-agents/mock/build.py
"""
import os
import re

HERE = os.path.dirname(os.path.abspath(__file__))


def base(name):
    return open(os.path.join(HERE, "base", name + ".html")).read()


def swap(d, old, new, count=1):
    assert old in d, "not in the base: " + old[:70]
    return d.replace(old, new, count)


def rswap(d, pat, new):
    out, n = re.subn(pat, lambda _: new, d, count=1, flags=re.S)
    assert n == 1, "no match: " + pat[:70]
    return out


TRI = '<svg class="und-ic" width="{0}" height="{0}" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 2.2 14.3 13.2H1.7Z" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"/><path d="M8 6.6v3.2" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/><circle cx="8" cy="11.4" r=".9" fill="currentColor"/></svg>'
PAUSE = '<svg class="und-ic" width="{0}" height="{0}" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="6.2" fill="none" stroke="currentColor" stroke-width="1.6"/><path d="M6.4 5.6v4.8M9.6 5.6v4.8" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>'
PI = '<span class="pi line" aria-hidden="true" style="width: 14px; height: 14px; --pc: var(--p-teal); font-size: 6px;"><svg viewBox="0 0 24 24" width="10" height="10" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"></path><path d="M2 12h20"></path></svg></span>'

CMD = "node test/layout.e2e.mjs"
ROW_OLD = '<span class="tslot" title="The heavy job this thread\'s slot runs, and the thread\'s memory now">yarn build · 3.0 GB</span> · Bash ' + CMD + ' · 1 agent · <span class="tcpu hot" title="CPU of this thread and everything it started, over the last seconds">2.6 cores</span></i></span>'
RAIL_PAT = r'<span class="rbar hot" title="[^"]*"><span class="trk"><i style="width: 100%;"></i></span><small class="mono">1 waits</small></span>'
WHY_OLD = "The next waits: 2.0 of 2.0 in use"

HEAD = '<link rel="stylesheet" href="../rogue.css">\n<script>var q=new URLSearchParams(location.search).get("theme");if(q)document.documentElement.setAttribute("data-theme",q)</script>\n</head>'
VIEW = '<script>addEventListener("load",function(){document.querySelectorAll("[data-view]").forEach(function(e){e.scrollIntoView({block:e.dataset.view||"%s"})})})</script>\n</body>'


def finish(d, block="center"):
    d = swap(d, "</head>", HEAD)
    return swap(d, "</body>", VIEW % block)


def common(d, w):
    """What every frame shares: the thread runs the layout harness, and the queue says who it waits for."""
    d = d.replace("yarn vitest src/map --reporter=verbose", CMD).replace("Bash yarn vitest src/map", "Bash " + CMD)
    return d.replace(WHY_OLD, "The next waits: needs ≈ 45 % CPU, 20 % free (Map perf uses 52 % " + w["adj"] + ")")


# ---------- the row ----------
def row(d, w, variant, paused=False):
    tip = "Heavy work Fyr did not admit: this thread uses 2.8 GB and 2.1 cores above its idle level, for 3 min. It counts against the host's heavy jobs."
    if paused:
        line = '<span class="tund" title="%s" data-view>%s%s · paused: memory</span> · Bash %s · 1 agent' % (tip, PAUSE.format(12), w["adj"], CMD)
        return swap(d, ROW_OLD, line + "</i></span>")
    if variant == "a":
        line = '<span class="tund" title="%s" data-view>%s%s · 2.8 GB · 2.1 cores</span> · Bash %s · 1 agent' % (tip, TRI.format(12), w["adj"], CMD)
        return swap(d, ROW_OLD, line + "</i></span>")
    line = 'Bash %s · 1 agent · <span class="tcpu hot">2.1 cores</span>' % CMD
    chip = '<span class="cl"><span class="pr und" title="%s" data-view>%s 2.8 GB</span></span>' % (tip, w["adj"])
    return swap(d, ROW_OLD, line + "</i>" + chip + "</span>")


# ---------- the rail ----------
def rail(d, w, variant):
    tip = "Heavy jobs on desk: CPU 100 % of the budget (admitted 48 %, ADJ 52 %), memory 61 % (admitted 41 %, ADJ 20 %); 1 waiting.&#10;CAP: Map perf (acme/web), 2.8 GB and 2.1 cores for 3 min".replace("ADJ", w["adj"]).replace("CAP", w["Adj"])
    bar = '<span class="rbar hot" title="%s"><span class="trk"><i style="width: 48%%;"></i><i class="und" style="width: 52%%;"></i></span><small class="mono">1 waits</small></span>' % tip
    who = "Map perf (acme/web): 2.8 GB and 2.1 cores, %s for 3 min. Open the thread" % w["adj"]
    if variant == "a":
        btn = '<button class="rund" title="%s" aria-label="1 thread runs heavy work Fyr did not admit: Map perf">%s1</button>' % (who, TRI.format(9))
    else:
        btn = '<button class="rund icons" title="%s" aria-label="1 thread runs heavy work Fyr did not admit: Map perf">%s</button>' % (who, PI)
    return rswap(d, RAIL_PAT, bar + btn)


# ---------- the thread ----------
def told(w):
    return '<div class="cv proc" data-view="end"><div class="ql und"><div class="qlh click" role="button" tabindex="0" aria-expanded="false">%s<span class="ell">Fyr told the agent: heavy work it did not ask for (2.8 GB, 2.1 cores); ask before a heavy job</span></div></div></div>' % TRI.format(12)


def strip(w, phone=False, paused=False, state="flag"):
    if paused:
        text = '<b>Paused by Fyr: memory is short on desk (6 % free).</b> Frozen, not killed; it resumes when memory has recovered for 30 s, within 10 min at most.<br><span class="mono">' + CMD + '</span> <span class="dim">· 2.8 GB · %s</span>' % w["adj"]
        return '<div class="undb paused" role="status">%s<span class="undb-b">%s</span></div>' % (PAUSE.format(14), text)
    text = "<b>" + w["title"] + '</b> · 2.8 GB (20 % of desk) · 2.1 cores (52 %) · 3 min<br><span class="mono">' + CMD + '</span> <span class="dim">· counted against desk\'s heavy jobs · agent told 14:29</span>'
    btns = '<span class="undb-a"><button class="btn">Add as heavy job…</button><button class="btn">Dismiss</button></span>'
    return '<div class="undb" role="status">%s<span class="undb-b">%s</span>%s</div>' % (TRI.format(14), text, btns)


def thread_desk(d, w, variant, paused=False):
    if variant == "a":
        chip = '<button class="chip und" title="Heavy work Fyr did not admit: 2.8 GB (20 %% of desk), 2.1 cores (52 %%), 3 min. Click for the command and Dismiss">%s<span>%s · 2.8 GB</span></button>' % (TRI.format(12), w["Adj"])
        d = swap(d, '<span style="color: var(--ink);">Working</span></span>', '<span style="color: var(--ink);">Working</span></span>' + chip)
    else:
        d = swap(d, '</div><div class="agents">', "</div>" + strip(w, paused=paused) + '<div class="agents">')
    return swap(d, '<div class="here"', told(w) + '<div class="here"')


def thread_phone(d, w, variant, paused=False):
    if variant == "a":
        chip = '<button class="m-chip und">%s<span>%s · 2.8 GB</span></button>' % (TRI.format(14), w["Adj"])
        d = swap(d, '<button class="m-chipsd"', chip + '<button class="m-chipsd"')
    else:
        d = swap(d, '</div><div class="agents">', "</div>" + strip(w, phone=True, paused=paused) + '<div class="agents">')
    return swap(d, '<div class="here"', told(w) + '<div class="here"')


# ---------- Settings ----------
def sr(title, desc, end="", mono=False, tri=False, cls=" undrow"):
    t = '<span class="sr-t%s">%s%s</span>' % (" mono" if mono else "", TRI.format(13) if tri else "", title)
    e = '<span class="sr-e">%s</span>' % end if end else ""
    return '<div class="sr%s"><span class="sr-b">%s<span class="sr-d">%s</span></span>%s</div>' % (cls, t, desc, e)


def group(title, rows, foot, label=None, view=False):
    return '<section class="sg" aria-label="%s"%s><div class="sg-h"><span class="sg-t">%s</span></div><div class="sg-c">%s</div><div class="sg-f">%s</div></section>' % (label or title, " data-view" if view else "", title, "".join(rows), foot)


def suggest(cmd, seen, pat, kind):
    return ('<div class="sr col"><span class="sr-b"><span class="cmd">%s</span><span class="sr-d">%s</span></span>'
            '<span class="inl"><input class="mono" type="text" value="%s" aria-label="Pattern for %s" spellcheck="false"><select aria-label="Kind"><option>%s</option></select><button type="button" class="btn">Add</button><button type="button" class="btn">Ignore</button></span></div>') % (cmd, seen, pat, cmd, kind)


def settings_groups(w, it):
    now = group(
        w["now"],
        [
            sr("Map perf", "acme/web · <span class=\"mono\">%s</span> · 2.8 GB (20 %%) · 2.1 cores (52 %%) · 3 min · agent told" % CMD, '<button type="button" class="btn">Open</button><button type="button" class="btn">Dismiss</button>', tri=True),
            sr("Babysit PR #4170", "acme/web · <span class=\"mono\">yarn pre-push:check</span> · 1.6 GB (11 %) · 1.2 cores (30 %) · 50 s · agent told", '<button type="button" class="btn">Open</button><button type="button" class="btn">Dismiss</button>', tri=True),
        ],
        "Threads that use clearly more than their idle level without having asked for a heavy job. Fyr tells the agent once, counts the use against this host's heavy jobs, and pauses the newest one only when memory is short. Nothing is killed.",
        view=True,
    )
    sug = group(
        "Suggested heavy jobs",
        [
            suggest(CMD, "Seen 3 times in 2 threads, last 14:29 in Map perf · up to 2.8 GB, 2.1 cores, 4 min", r"node (\S*/)?layout\.e2e\.mjs", "e2e"),
            suggest("yarn pre-push:check", "Seen once, 14:31 in Babysit PR #4170 · up to 1.6 GB, 1.2 cores, 2 min", r"yarn pre-push:check", "test"),
        ],
        "The commands behind that use. Once added, Fyr refuses the command until its thread has asked for a heavy job, as it does for make test. Ignore hides a suggestion for good.",
    )
    flag = group(
        "When a thread counts as heavy",
        [
            '<label class="sr"><span class="sr-b"><span class="sr-t">CPU above its idle level</span><span class="sr-d">1.0 of desk\'s 4 cores</span></span><span class="sr-e"><input class="pq num" type="text" value="25" inputmode="numeric" aria-label="CPU share"><span class="unit">% of this host</span></span></label>',
            '<label class="sr"><span class="sr-b"><span class="sr-t">Memory above its idle level</span><span class="sr-d">1.1 GB of the 14.0 GB desk gives its agents</span></span><span class="sr-e"><input class="pq num" type="text" value="8" inputmode="numeric" aria-label="Memory share"><span class="unit">% of this host</span></span></label>',
            '<label class="sr"><span class="sr-b"><span class="sr-t">For at least</span><span class="sr-d">Shorter bursts (a typecheck, one test file) never count</span></span><span class="sr-e"><input class="pq num" type="text" value="45" inputmode="numeric" aria-label="Seconds"><span class="unit">seconds</span></span></label>',
        ],
        "A thread's idle level is what its agent uses between jobs (the CLI and the servers it keeps running), learned per thread.",
    )
    return now + sug + flag


def settings_desk(w, it):
    d = base("desk-set")
    return swap(d, '<section class="sg" aria-label="Heavy jobs">', settings_groups(w, it) + '<section class="sg" aria-label="Heavy jobs">')


def settings_phone(w, it):
    d = base("phone")
    m = re.search(r'<section class="sg" aria-label="Heavy jobs">.*?</section>', base("desk-set"), flags=re.S).group(0)
    back = '<svg width="22" height="22" viewBox="0 0 24 24" aria-hidden="true"><path d="M15 5l-7 7 7 7" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"></path></svg>'
    page = ('<div id="app"><div class="m-app"><div class="m-screen m-settings"><div class="m-nh"><button class="b44" aria-label="Back">%s</button><div class="m-nt ell">Automation</div><span class="sp"></span></div>'
            '<div class="m-nbody"><div class="set phone"><div class="stp" data-page="automation">%s%s</div></div></div></div></div></div>') % (back, settings_groups(w, it), m)
    return rswap(d, r'<div id="app">.*</div>\s*(?=</body>)', page + "\n")


# ---------- the frames ----------
W1 = {"adj": "undeclared", "Adj": "Undeclared", "title": "Heavy work Fyr did not admit", "now": "Undeclared now"}


def frames(it, w, variants):
    out = {}
    for v in variants:
        d = common(base("desk-open"), w)
        d = thread_desk(rail(row(d, w, v), w, v), w, v)
        out["i%d-desk-%s" % (it, v)] = finish(d)
        p = row(common(base("phone"), w), w, v)
        out["i%d-phone-inbox-%s" % (it, v)] = finish(p)
        t = thread_phone(common(base("phone-open"), w), w, v)
        out["i%d-phone-thread-%s" % (it, v)] = finish(t, "end")
    out["i%d-desk-settings" % it] = finish(settings_desk(w, it), "start")
    out["i%d-phone-settings" % it] = finish(settings_phone(w, it), "start")
    return out


BOARD = """<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>%(title)s</title>
<link rel="stylesheet" href="app.css">
<style>
html,body{height:auto;overflow:auto}
body{margin:0;background:var(--surface);color:var(--ink);font:400 14px/21px var(--ui)}
.bd{max-width:1340px;margin:0 auto;padding:20px 16px 80px}
.bd h1{font:600 20px/26px var(--ui);margin:0 0 4px}
.bd h2{font:600 15px/22px var(--ui);margin:36px 0 2px}
.bd p{margin:0 0 10px;color:var(--ink-muted);max-width:86ch}
.bd .top{display:flex;gap:12px;align-items:baseline;flex-wrap:wrap}
.bd .top .sp{margin-left:auto}
.fr{border:1px solid var(--line-strong);border-radius:10px;overflow:hidden;background:var(--raised);position:relative}
.fr iframe{border:0;display:block;transform-origin:0 0}
.fr.desk{width:100%%}
.ph{display:flex;gap:20px;flex-wrap:wrap;align-items:flex-start}
.ph figure{margin:0}
.ph .fr{width:390px;max-width:100%%}
figcaption,.cap{font:500 12px/16px var(--ui);color:var(--ink-muted);margin:0 0 6px}
.tag{display:inline-block;padding:1px 7px;border-radius:9px;background:var(--night);color:var(--on-night);font:600 11px/16px var(--ui);margin-right:6px}
</style></head><body><div class="bd">
<div class="top"><h1>%(title)s</h1><span class="sp"></span><span class="seg"><button data-t="light" class="on">Light</button><button data-t="dark">Dark</button></span></div>
%(body)s
</div>
<script>
function fit(){document.querySelectorAll('.fr').forEach(function(f){var i=f.firstElementChild,w=+i.getAttribute('width'),h=+i.getAttribute('height'),s=Math.min(1,f.clientWidth/w);i.style.transform='scale('+s+')';f.style.height=Math.round(h*s)+'px'})}
addEventListener('resize',fit);fit();
document.querySelectorAll('.seg button').forEach(function(b){b.onclick=function(){var t=b.dataset.t;document.documentElement.setAttribute('data-theme',t);document.querySelectorAll('.seg button').forEach(function(x){x.classList.toggle('on',x===b)});document.querySelectorAll('iframe').forEach(function(i){i.src=i.src.split('?')[0]+'?theme='+t})}});
</script></body></html>
"""


def desk(name, cap, h=800):
    return '<p class="cap">%s</p><div class="fr desk"><iframe src="frames/%s.html" width="1280" height="%d" title="%s"></iframe></div>' % (cap, name, h, name)


def phones(items):
    return '<div class="ph">' + "".join('<figure><figcaption>%s</figcaption><div class="fr"><iframe src="frames/%s.html" width="390" height="%d" title="%s"></iframe></div></figure>' % (c, n, h, n) for n, c, h in items) + "</div>"


def board1():
    intro = """
<p>Iteration 1 of V0.164: a thread that runs heavy work without asking for a heavy job. Every frame is the real front (mock mode, 17 Nov) with only the new parts changed; the buttons do nothing. The story in all frames: Map perf runs <code>node test/layout.e2e.mjs</code> (2.8 GB, 2.1 cores) and never called slot_acquire; Search index asked properly and waits.</p>"""
    a = """
<h2><span class="tag">A</span>Quiet: a line of text, a count on the rail, a chip on the thread</h2>
<p>The row's second line starts with the word and the use, in the beam colour. The host's meter shows the use Fyr did not admit as a hatched part; under it a count opens the thread. The thread has a chip, and the conversation shows the one message the agent got.</p>
""" + desk("i1-desk-a", "Desktop, 1280 px") + phones([("i1-phone-inbox-a", "Phone inbox", 844), ("i1-phone-thread-a", "Phone thread", 844)])
    b = """
<h2><span class="tag">B</span>Louder: a chip on the row, the thread's icon on the rail, a strip on the thread</h2>
<p>The row gets a chip like a pull request's. The rail shows the thread's project icon under the host. The thread has a strip under its header with the command, the numbers and the two things the owner can do.</p>
""" + desk("i1-desk-b", "Desktop, 1280 px") + phones([("i1-phone-inbox-b", "Phone inbox", 844), ("i1-phone-thread-b", "Phone thread", 844)])
    c = """
<h2>Settings → Automation → Heavy jobs (the same in A and B)</h2>
<p>Three new groups above today's list of learned jobs: who does it now, the commands behind it as suggested heavy jobs, and the thresholds of this host.</p>
""" + desk("i1-desk-settings", "Desktop, 1280 × 1000", 1000) + phones([("i1-phone-settings", "Phone (the whole page)", 1560)])
    t = "Heavy work without asking · iteration 1"
    return {"i1": BOARD % {"title": t, "body": intro + a + b + c}, "i1-a": BOARD % {"title": t + " · A", "body": a}, "i1-b": BOARD % {"title": t + " · B", "body": b}, "i1-settings": BOARD % {"title": t + " · Settings", "body": c}}

# ---------- iteration 2: the owner's choices (A's row and rail, B's strip, "undeclared", Dismiss hides) ----------
BABY = r'Bash git rebase origin/main · <span class="tcpu[^>]*>0.3 cores</span>'


def rail2(d, w, n):
    tip = "Heavy jobs on desk: CPU 100 % of the budget (admitted 18 %, undeclared 82 %), memory 72 % (admitted 41 %, undeclared 31 %); 1 waiting.&#10;Undeclared: Map perf (acme/web), 2.8 GB and 2.1 cores for 3 min; Babysit PR #4170 (acme/web), 1.6 GB and 1.2 cores for 50 s"
    bar = '<span class="rbar hot" title="%s"><span class="trk"><i style="width: 18%%;"></i><i class="und" style="width: 82%%;"></i></span><small class="mono">1 waits</small></span>' % tip
    btn = '<button class="rund" title="2 threads run undeclared heavy work: Map perf, Babysit PR #4170. Click for the list" aria-haspopup="menu" aria-label="2 threads run undeclared heavy work">%s%d</button>' % (TRI.format(9), n)
    return rswap(d, RAIL_PAT, bar + btn)


def baby(d, w):
    line = '<span class="tund">%sundeclared · 1.6 GB · 1.2 cores</span> · Bash yarn pre-push:check' % TRI.format(12)
    return rswap(d, BABY, line)


def settings_groups2(w):
    ob = '<button type="button" class="btn">Open</button>'
    now = group(
        "Undeclared now",
        [
            sr("Map perf", "acme/web · <span class=\"mono\">%s</span> · 2.8 GB (20 %%) · 2.1 cores (52 %%) · 3 min · agent told 14:29" % CMD, ob + '<button type="button" class="btn">Dismiss</button>', tri=True),
            sr("Babysit PR #4170", "acme/web · <span class=\"mono\">yarn pre-push:check</span> · 1.6 GB (11 %) · 1.2 cores (30 %) · 50 s · agent told 14:31", ob + '<button type="button" class="btn">Dismiss</button>', tri=True),
            sr("Nix flake update", "dotfiles · <span class=\"mono\">nix flake check</span> · 1.9 GB (14 %) · 0.2 cores · 22 min · dismissed, still counted", ob + '<button type="button" class="btn">Show again</button>', cls=" undrow off"),
        ],
        "Threads that use clearly more than their idle level without having asked for a heavy job. Fyr tells the agent once and counts the use against this host's heavy jobs, so jobs that asked wait instead of overloading it. Only when memory is short does Fyr pause the newest one; nothing is killed.",
        view=True,
    )
    sug = group(
        "Suggested heavy jobs",
        [
            suggest(CMD, "Seen 3 times in 2 threads, last 14:29 in Map perf · up to 2.8 GB, 2.1 cores, 4 min", r"node (\S*/)?layout\.e2e\.mjs", "e2e"),
            suggest("yarn pre-push:check", "Seen once, 14:31 in Babysit PR #4170 · up to 1.6 GB, 1.2 cores, 2 min", r"yarn pre-push:check", "test"),
        ],
        "The commands behind undeclared work. Once added, Fyr refuses the command until its thread has asked for a heavy job, as it does for make test. Ignore hides a suggestion for good.",
    )
    mine = group(
        "Heavy jobs you added",
        [sr("python3? build_admin_ny\\.py", "build · added 16 Nov from a suggestion · refused 4 times since", '<button type="button" class="btn">Remove</button>', mono=True, cls="")],
        "Besides the built-in ones (make test, go test ./..., playwright, nix build and the like).",
    )
    flag = group(
        "When a thread counts as heavy",
        [
            '<label class="sr"><span class="sr-b"><span class="sr-t">CPU above its idle level</span><span class="sr-d">Now 1.0 of desk\'s 4 cores</span></span><span class="sr-e"><input class="pq num" type="text" value="25" inputmode="numeric" aria-label="CPU share"><span class="unit">% of this host</span></span></label>',
            '<label class="sr"><span class="sr-b"><span class="sr-t">Memory above its idle level</span><span class="sr-d">Now 1.1 GB of the 14.0 GB desk gives its agents</span></span><span class="sr-e"><input class="pq num" type="text" value="8" inputmode="numeric" aria-label="Memory share"><span class="unit">% of this host</span></span></label>',
            '<label class="sr"><span class="sr-b"><span class="sr-t">For at least</span><span class="sr-d">Shorter bursts (a typecheck, one test file) never count</span></span><span class="sr-e"><input class="pq num" type="text" value="45" inputmode="numeric" aria-label="Seconds"><span class="unit">seconds</span></span></label>',
        ],
        "A thread's idle level is what its agent uses between jobs (the CLI and the servers it keeps running), learned per thread.",
    )
    return now + sug + mine + flag


def frames2(w):
    out = {}
    for name, paused in (("", False), ("-paused", True)):
        d = baby(common(base("desk-open"), w), w)
        d = row(d, w, "a", paused=paused)
        d = thread_desk(rail2(d, w, 2), w, "b", paused=paused)
        out["i2-desk" + name] = finish(d)
        t = thread_phone(common(base("phone-open"), w), w, "b", paused=paused)
        out["i2-phone-thread" + name] = finish(t, "end")
    out["i2-phone-inbox"] = finish(row(baby(common(base("phone"), w), w), w, "a"))
    g = settings_groups2(w)
    out["i2-desk-settings"] = finish(swap(base("desk-set"), '<section class="sg" aria-label="Heavy jobs">', g + '<section class="sg" aria-label="Heavy jobs">'), "start")
    global settings_groups
    keep = settings_groups
    settings_groups = lambda w_, it: g
    out["i2-phone-settings"] = finish(settings_phone(w, 2), "start")
    settings_groups = keep
    return out


def board2():
    a = """
<p>Iteration 2 of V0.164, with the owner's four answers: A's row and rail with B's strip on the thread; the word is "undeclared"; Dismiss hides the mark until the work stops (it still counts); under memory pressure undeclared work is paused before a job that asked. Two threads are flagged here: Map perf (<code>node test/layout.e2e.mjs</code>) and Babysit PR #4170 (<code>yarn pre-push:check</code>), the two jobs of 17 Nov.</p>
<h2>Flagged</h2>
<p>Rows: the word and the use start the second line. Rail: the hatched part of desk's meter is the undeclared use Fyr counts; the count under it opens the thread, or a menu when there are several. Thread: the strip names the use, the command, that it is counted and when the agent was told; the conversation keeps the one message.</p>
""" + desk("i2-desk", "Desktop, 1280 px") + phones([("i2-phone-inbox", "Phone inbox", 844), ("i2-phone-thread", "Phone thread", 844)]) + """
<h2>Paused under memory pressure</h2>
<p>Only while memory is really short: the newest undeclared work is frozen, never killed. The row says "undeclared · paused: memory" (V0.151's words); the strip turns to the harbour colour and says when it resumes. Its buttons go: there is nothing to decide while it is paused.</p>
""" + desk("i2-desk-paused", "Desktop, 1280 px") + phones([("i2-phone-thread-paused", "Phone thread", 844)])
    c = """
<h2>Settings → Automation → Heavy jobs</h2>
<p>New since iteration 1: a dismissed thread stays in the list, dimmed, with Show again (it is still counted); the patterns the owner added are listed with Remove.</p>
""" + desk("i2-desk-settings", "Desktop, 1280 × 1000", 1000) + phones([("i2-phone-settings", "Phone (the whole page)", 1900)])
    t = "Undeclared heavy work · iteration 2"
    return {"i2": BOARD % {"title": t, "body": a + c}, "i2-threads": BOARD % {"title": t, "body": a}, "i2-settings": BOARD % {"title": t + " · Settings", "body": c}}


def main():
    fs = frames(1, W1, ["a", "b"])
    for n, d in fs.items():
        open(os.path.join(HERE, "frames", n + ".html"), "w").write(d)
    for n, d in board1().items():
        open(os.path.join(HERE, n + ".html"), "w").write(d)
    fs2 = frames2(W1)
    for n, d in fs2.items():
        open(os.path.join(HERE, "frames", n + ".html"), "w").write(d)
    for n, d in board2().items():
        open(os.path.join(HERE, n + ".html"), "w").write(d)
    print("built", len(fs) + len(fs2), "frames")


if __name__ == "__main__":
    main()
