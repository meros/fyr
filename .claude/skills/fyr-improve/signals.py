#!/usr/bin/env python3
"""Friction signals from a Fyr host's event log, for the fyr-improve skill.

Reads <data>/fyr.db read-only and prints, for a time window, what made threads
noisy, stuck or unclear: Fyr notices by template, notices that only woke an
agent for a short turn, the owner nudging a thread, unasked-question nudges,
merge refusals and failed effects, holds and freezes, run failures, titles and
sprawl. The output holds private data (titles, texts, paths): keep it in the
scratchpad, never in the repository.

Usage: signals.py [--since 24h|2026-11-25T08:00] [--db PATH] [--json]
"""
import argparse
import collections
import datetime as dt
import json
import os
import re
import sqlite3
import sys

# The owner asking a thread what goes on: the clearest sign that the state on screen did not say it.
PROD = re.compile(
    r"(are you (still )?(waiting|stuck|working|done)|what('s| is) (going on|happening|the status)|"
    r"\bstatus\b\??$|is (it|this) (stuck|running|done)|why (did|does|is)n?'?t? .{0,40}(stop|wait|hang|move)|"
    r"väntar du|hänger (den|det|du)|vad händer|vad gör du|är du klar|kör (den|du)|status\?)",
    re.I,
)
NOTICE = re.compile(r"^\[Fyr\] ?(.*)$", re.M)
# Strip the parts of a notice that change from one to the next, so notices group by template.
VARY = re.compile(r"(#\d+|\b[0-9a-f]{7,40}\b|\bt_[0-9a-f]+|\bc_[0-9a-f]+|\bd_[0-9a-f]+|\d+([.,]\d+)?( ?(%|s|min|GB|MB|h))?|\"[^\"]*\"|`[^`]*`)")
SHORT_WAKE_MS = 45_000


def since_arg(s):
    m = re.fullmatch(r"(\d+)([hd])", s)
    now = dt.datetime.now(dt.timezone.utc)
    if m:
        n = int(m.group(1))
        return now - (dt.timedelta(hours=n) if m.group(2) == "h" else dt.timedelta(days=n))
    t = dt.datetime.fromisoformat(s)
    if t.tzinfo is None:
        t = t.astimezone()
    return t.astimezone(dt.timezone.utc)


def template(line):
    t = VARY.sub("·", line.strip())
    return t[:110]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--since", default="24h")
    ap.add_argument("--db", default=os.path.join(os.environ.get("FYR_DATA", os.path.expanduser("~/.local/share/fyr")), "fyr.db"))
    ap.add_argument("--json", action="store_true")
    a = ap.parse_args()
    since = since_arg(a.since)
    db = sqlite3.connect(f"file:{a.db}?mode=ro", uri=True)

    threads = {}
    for tid, data in db.execute("select id, data from threads"):
        d = json.loads(data)
        threads[tid] = d
    changes = {}
    for cid, data in db.execute("select id, data from changes"):
        changes[cid] = json.loads(data)

    # Events are stored with UTC RFC 3339 strings on some hosts and local offsets on others: compare parsed times.
    rows = db.execute("select seq, at, subject, type, data from events where at >= ? order by seq", ((since - dt.timedelta(days=1)).strftime("%Y-%m-%d"),))
    per = collections.defaultdict(lambda: collections.Counter())
    notices = collections.Counter()
    notice_example = {}
    wake_notice = collections.Counter()  # notice template -> short runs it started
    prods, asks, merges, effects, holds, failures = [], [], [], [], [], []
    last_user = {}  # thread -> (template list, at) of the last user event
    run_start = {}
    active = set()
    opened = {}  # decision id -> (opened at, action)
    waits = collections.defaultdict(list)  # (action, who closed) -> minutes open
    runs = wasted = 0
    for seq, at, subj, typ, data in rows:
        try:
            t = dt.datetime.fromisoformat(at.replace("Z", "+00:00"))
        except ValueError:
            continue
        if t < since:
            continue
        try:
            d = json.loads(data)
        except ValueError:
            d = {}
        if subj.startswith("t_"):
            active.add(subj)
        per[subj][typ] += 1
        if typ == "user":
            text = d.get("text", "") or ""
            lines = NOTICE.findall(text)
            if lines:
                tps = [template(l) for l in lines]
                for tp, l in zip(tps, lines):
                    notices[tp] += 1
                    notice_example.setdefault(tp, (subj, l[:200]))
                last_user[subj] = (tps, t)
            else:
                last_user[subj] = (None, t)
                if PROD.search(text.strip()[:200]):
                    prods.append((at, subj, text.strip()[:160]))
        elif typ == "run.started":
            run_start[subj] = (t, d.get("by", ""), last_user.get(subj))
        elif typ == "run.ended":
            st = run_start.pop(subj, None)
            runs += 1
            f = d.get("failure")
            if f:
                failures.append((at, subj, f.get("class", ""), (f.get("message") or "")[:120]))
            if st and st[2] and st[2][0] and d.get("durationMs", 10**9) < SHORT_WAKE_MS and (t - st[2][1]).total_seconds() < 120:
                wasted += 1
                for tp in st[2][0]:
                    wake_notice[tp] += 1
        elif typ == "decision.opened":
            dd = d.get("decision") or {}
            opened[dd.get("id")] = (t, (dd.get("effect") or {}).get("action") or dd.get("kind", ""))
        elif typ == "decision.closed" and d.get("id") in opened:
            o, kind = opened.pop(d["id"])
            who = "auto" if d.get("auto") else ("not asked" if str(d.get("answer", "")).startswith("Not asked") else "owner")
            waits[(kind, who)].append((t - o).total_seconds() / 60)
        elif typ == "textAsk.nudged":
            asks.append((at, subj, (d.get("question") or "")[:140]))
        elif typ == "merge.refused":
            merges.append((at, subj, d.get("pr", ""), (d.get("reason") or "")[:140]))
        elif typ == "effect.result" and not d.get("ok", True):
            effects.append((at, subj, d.get("action", ""), (d.get("text") or "")[:140]))
        elif typ == "decision.closed" and str(d.get("answer", "")).startswith("Not asked"):
            merges.append((at, subj, (d.get("args") or {}).get("pr", ""), d.get("answer", "")[:140]))
        elif typ in ("agent.gateHold", "slot") and (d.get("why") or d.get("frozen")):
            holds.append((at, subj, d.get("why") or ("frozen " + str(d.get("frozen")))))

    # Sprawl: changes and repos per thread active in the window.
    sprawl = []
    for tid in active:
        th = threads.get(tid)
        if not th:
            continue
        # A thread holds its changes inline; only those with a PR count, since Fyr also observes plain branches.
        cs = [c if isinstance(c, dict) else changes.get(c, {}) for c in (th.get("changes") or [])]
        prs = [c for c in cs if c.get("pr")]
        repos = {c["pr"].get("repo", "") for c in prs} - {""}
        if len(prs) >= 3 or len(repos) >= 2:
            sprawl.append((tid, len(prs), len(repos), th.get("title", "")))
    sprawl.sort(key=lambda r: (-r[1], -r[2]))

    titles = []
    for tid in active:
        th = threads.get(tid)
        if not th:
            continue
        ti = th.get("title", "") or ""
        why = []
        if ti.endswith("…"):
            why.append("cut")
        if len(ti.split()) <= 1:
            why.append("one word")
        if len(ti) > 70:
            why.append("long")
        if why:
            titles.append((tid, ",".join(why), th.get("titleBy", ""), ti))

    # Throughput: PRs merged in the window and how long each took from its change's start.
    lead = []
    for th in threads.values():
        for c in th.get("changes") or []:
            if not isinstance(c, dict) or not (c.get("pr") or {}).get("mergedAt"):
                continue
            try:
                m = dt.datetime.fromisoformat(c["pr"]["mergedAt"].replace("Z", "+00:00"))
                s = dt.datetime.fromisoformat(c["createdAt"].replace("Z", "+00:00"))
            except (KeyError, ValueError):
                continue
            if m >= since:
                lead.append((m - s).total_seconds() / 3600)

    def med(xs):
        xs = sorted(xs)
        return round(xs[len(xs) // 2], 1) if xs else None

    score = {
        "threads_active": len(active),
        "owner_prods": len(prods),
        "runs": runs,
        "short_wakes_by_notice": wasted,
        "unasked_question_nudges": len(asks),
        "merge_refusals_or_not_asked": len(merges),
        "holds_and_freezes": len(holds),
        "prs_merged": len(lead),
        "pr_lead_hours_median": med(lead),
        "decision_wait_minutes_median": {f"{k[0]}/{k[1]}": (len(v), med(v)) for k, v in sorted(waits.items())},
    }
    out = {
        "since": since.isoformat(),
        "score": score,
        "threads_active": len(active),
        "notices_top": [(n, tp, notice_example[tp][0]) for tp, n in notices.most_common(25)],
        "notices_that_only_woke": [(n, tp) for tp, n in wake_notice.most_common(15)],
        "owner_prods": prods,
        "unasked_question_nudges": asks,
        "merge_refusals": merges,
        "failed_effects": effects,
        "holds": collections.Counter(h[1] for h in holds).most_common(15),
        "hold_reasons": collections.Counter(re.sub(r"\d+", "·", h[2])[:90] for h in holds).most_common(10),
        "run_failures": collections.Counter((f[2], f[3][:60]) for f in failures).most_common(10),
        "sprawl": sprawl[:15],
        "titles": titles[:30],
    }
    if a.json:
        json.dump(out, sys.stdout, indent=1, ensure_ascii=False, default=str)
        return

    def title(tid):
        return (threads.get(tid, {}).get("title", "") or "")[:60]

    p = print
    p(f"# Fyr friction since {out['since']} · {out['threads_active']} threads active\n")
    p("## Scorecard (compare with the last run's)")
    for k, v in score.items():
        p(f"- {k}: {v}")
    p("")
    p("## Notices by template (count · template · example thread)")
    for n, tp, tid in out["notices_top"]:
        p(f"- {n} · {tp} · {tid}")
    p("\n## Notices that only woke the agent for a short turn (likely noise)")
    for n, tp in out["notices_that_only_woke"]:
        p(f"- {n} · {tp}")
    p(f"\n## The owner asked what goes on ({len(prods)})")
    for at, tid, text in prods:
        p(f"- {at[:16]} {tid} [{title(tid)}]: {text}")
    p(f"\n## Unasked-question nudges ({len(asks)}): judge each, real or false")
    for at, tid, q in asks:
        p(f"- {at[:16]} {tid} [{title(tid)}]: {q}")
    p(f"\n## Merge refusals and asks not shown ({len(merges)})")
    for at, tid, pr, why in merges:
        p(f"- {at[:16]} {tid} {pr}: {why}")
    p(f"\n## Failed effects ({len(effects)})")
    for at, tid, act, why in effects:
        p(f"- {at[:16]} {tid} {act}: {why}")
    p("\n## Holds and freezes by thread")
    for tid, n in out["holds"]:
        p(f"- {n} · {tid} [{title(tid)}]")
    for r, n in out["hold_reasons"]:
        p(f"  - {n} · {r}")
    p("\n## Run failures (class · message)")
    for (c, m), n in out["run_failures"]:
        p(f"- {n} · {c} · {m}")
    p("\n## Sprawl (changes · repos · thread)")
    for tid, n, r, ti in out["sprawl"]:
        p(f"- {n} · {r} · {tid} [{ti[:60]}]")
    p("\n## Titles to judge (why · set by · title)")
    for tid, why, by, ti in out["titles"]:
        p(f"- {tid} · {why} · {by or '?'} · {ti}")


if __name__ == "__main__":
    main()
