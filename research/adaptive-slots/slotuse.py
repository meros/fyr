"""#174: how many heavy Bash runs held a slot, from Fyr's thread logs (read only).

python3 slotuse.py ~/.local/share/fyr/logs [--since MM-DD]
The patterns follow internal/heavy (Defaults); a quoted message (-m, --title, ...) is not a run.
"""
import collections, glob, json, os, re, sys, time

PAT = [
    ("make $1", r"make\s+(?:-C\s+\S+\s+|-\S+\s+)*(test[\w:-]*|check|e2e[\w-]*)\b"),
    ("go test ./...", r"go\s+test\b[^|;&]*\s\./(?:[\w.-]+/)*\.\.\."),
    ("playwright test", r"playwright\s+test\b"),
    ("yarn $1", r"yarn\s+(?:run\s+)?(test[\w:-]*|screenshot:diff|e2e[\w:-]*)\b"),
    ("npm run $1", r"npm\s+run\s+(test:[\w:-]+|e2e[\w:-]*)\b"),
    ("nix build", r"nix\s+build\b"),
    ("cargo test", r"cargo\s+test\b"),
    ("pytest", r"pytest\b"),
]
MSG = re.compile(r"""(?:\s-[mt]|\s--(?:message|title|body|subject))(?:=|\s+)(?:'[^'\n]*'|"[^"\n]*")""")


HEREDOC = re.compile(r"<<-?\s*['\"]?(\w+)['\"]?")
SPLIT = re.compile(r"&&|\|\||[;|\n()]|\$\(|`")
WRAP = re.compile(r"""^(?:\w+=\S*\s+|timeout\s+(?:-\S+\s+)*\S+\s+|time\s+|nice\s+(?:-n\s*-?\d+\s+)?|env\s+|exec\s+|command\s+|npx\s+|nix\s+develop\s+(?:\S+\s+)*?-c\s+|(?:ba)?sh\s+-l?c\s+|['"])""")


def segments(cmd):
    """The commands a shell line runs: heredoc bodies dropped, split at && || ; | ( ) $( ` and newlines, with
    wrappers (VAR=x, timeout, time, nice, env, exec, npx, nix develop -c, sh -c and a quote) taken off."""
    lines, out, end = cmd.split("\n"), [], None
    for ln in lines:
        if end is not None:
            if ln.strip() == end:
                end = None
            continue
        m = HEREDOC.search(ln)
        if m:
            end = m.group(1)
        out.append(ln)
    segs = []
    for seg in SPLIT.split("\n".join(out)):
        seg = seg.strip()
        while True:
            t = WRAP.sub("", seg, count=1).lstrip()
            if t == seg:
                break
            seg = t
        if seg:
            segs.append(seg)
    return segs


def klass(cmd):
    cmd = MSG.sub(" ''", cmd)
    best, n = "", 0
    for seg in segments(cmd):
        for name, p in PAT:
            m = re.match(p, seg)
            if m and m.end() - m.start() > n:
                best, n = name.replace("$1", m.group(1) if m.groups() else ""), m.end() - m.start()
    return best


def main():
    d = sys.argv[1]
    since = sys.argv[sys.argv.index("--since") + 1] if "--since" in sys.argv else None
    runs = []  # (thread, class, held, sub, bg)
    rel = collections.Counter()
    for f in sorted(glob.glob(os.path.join(d, "t_*.log"))):
        if since and time.strftime("%m-%d", time.localtime(os.path.getmtime(f))) < since:
            continue
        th = os.path.basename(f)[:-4]
        held, pend = False, {}
        for line in open(f, errors="ignore"):
            if " >> " in line:
                if "slot is yours now" in line:
                    held = True
                    rel["granted later"] += 1
                continue
            if " << " not in line:
                continue
            js = line.split(" << ", 1)[1]
            if '"type":"result"' in js and '"subtype":"success"' in js or '"type":"result"' in js and '"is_error"' in js:
                if held:
                    rel["released by the run's end"] += 1
                held = False
                continue
            if '"tool_use"' not in js and '"tool_result"' not in js:
                continue
            try:
                m = json.loads(js)
            except ValueError:
                continue
            sub = bool(m.get("parent_tool_use_id"))
            for c in (m.get("message") or {}).get("content") or []:
                if not isinstance(c, dict):
                    continue
                if c.get("type") == "tool_use":
                    n = c.get("name", "")
                    if n.endswith("slot_acquire") or n.endswith("slot_release"):
                        pend[c["id"]] = n
                    elif n == "Bash":
                        k = klass(c["input"].get("command", ""))
                        if k:
                            runs.append((th, k, held, sub, bool(c["input"].get("run_in_background"))))
                elif c.get("type") == "tool_result" and c.get("tool_use_id") in pend:
                    n = pend.pop(c["tool_use_id"])
                    txt = json.dumps(c.get("content"))
                    if n.endswith("slot_acquire"):
                        if "You hold" in txt:
                            held = True
                            rel["held at once"] += 1
                        elif "Queued" in txt:
                            rel["queued"] += 1
                        elif "no limit" in txt or "no slots" in txt:
                            held = True
                    elif "Released" in txt:
                        if held:
                            rel["released by slot_release"] += 1
                        held = False
    tot = len(runs)
    no = [r for r in runs if not r[2]]
    print(f"heavy Bash runs: {tot}; without a held slot: {len(no)} ({100*len(no)/max(tot,1):.0f} %)")
    print(f"  of those in a subagent: {sum(r[3] for r in no)}; in the background: {sum(r[4] for r in no)}")
    by = collections.defaultdict(lambda: [0, 0])
    for r in runs:
        by[r[1]][0] += 1
        by[r[1]][1] += not r[2]
    print("by command (runs, without a slot):")
    for k, (a, b) in sorted(by.items(), key=lambda x: -x[1][0]):
        print(f"  {k:24} {a:4} {b:4} ({100*b/a:.0f} %)")
    bt = collections.defaultdict(lambda: [0, 0])
    for r in runs:
        bt[r[0]][0] += 1
        bt[r[0]][1] += not r[2]
    print(f"threads with heavy runs: {len(bt)}; threads that never held a slot for one: {sum(1 for a, b in bt.values() if a == b)}")
    print("top threads by runs without a slot:")
    for k, (a, b) in sorted(bt.items(), key=lambda x: -x[1][1])[:8]:
        print(f"  {k} {a:4} {b:4}")
    print("slots:", dict(rel))


main()
