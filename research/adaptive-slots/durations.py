"""#174: wall time per heavy class from the thread logs (read only): python3 durations.py ~/.local/share/fyr/logs"""
import collections, glob, json, os, sys
sys_argv = sys.argv
sys.argv = ["x", "/dev/null"]
ns = {}
exec(open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "slotuse.py")).read().replace("\nmain()\n", "\n"), ns)
sys.argv = sys_argv
klass = ns["klass"]


def t(s):
    h, m, x = s.split(":")
    return int(h) * 3600 + int(m) * 60 + float(x)


res = collections.defaultdict(list)
for f in glob.glob(os.path.join(sys.argv[1], "t_*.log")):
    pend = {}
    for line in open(f, errors="ignore"):
        if " << " not in line:
            continue
        ts, _, js = line.partition(" << ")
        if '"tool_use"' not in js and '"tool_result"' not in js:
            continue
        try:
            d = json.loads(js)
        except ValueError:
            continue
        for c in (d.get("message") or {}).get("content") or []:
            if not isinstance(c, dict):
                continue
            if c.get("type") == "tool_use" and c.get("name") == "Bash" and not c["input"].get("run_in_background"):
                k = klass(c["input"].get("command", ""))
                if k:
                    pend[c["id"]] = (k, t(ts))
            elif c.get("type") == "tool_result" and c.get("tool_use_id") in pend:
                k, t0 = pend.pop(c["tool_use_id"])
                dt = t(ts) - t0
                res[k].append(dt + 86400 if dt < 0 else dt)
for k, v in sorted(res.items(), key=lambda x: -len(x[1])):
    v.sort()
    n = len(v)
    print(f"{k:22} n={n:4} p10={v[n//10]:5.0f} p50={v[n//2]:5.0f} p90={v[int(n*.9)]:5.0f}")
