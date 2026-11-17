import json,glob,re,sys,collections
PAT=[("make test",r"\bmake (test|check)\b"),("go test ./...",r"go test [^|;&]*\./\.\.\."),("make test-go",r"make test-go"),
 ("layout e2e",r"test:layout|make e2e|playwright test"),("yarn test",r"yarn (run )?test\b"),("screenshot:diff",r"screenshot:diff"),
 ("nix build",r"nix build"),("yarn build/tsc",r"yarn (build|tsc|typecheck)|tsc -b"),("go build",r"go build|make build")]
def t(s):
    h,m,x=s.split(":"); return int(h)*3600+int(m)*60+float(x)
res=collections.defaultdict(list)
for f in glob.glob(sys.argv[1]+"/t_*.log"):
    pend={}
    for line in open(f,errors="ignore"):
        if " << " not in line: continue
        ts,_,js=line.partition(" << ")
        if '"tool_use"' not in js and '"tool_result"' not in js: continue
        try: d=json.loads(js)
        except Exception: continue
        msg=d.get("message") or {}
        for c in msg.get("content") or []:
            if not isinstance(c,dict): continue
            if c.get("type")=="tool_use" and c.get("name")=="Bash":
                cmd=c["input"].get("command",""); bg=c["input"].get("run_in_background")
                for name,p in PAT:
                    if re.search(p,cmd): pend[c["id"]]=(name,t(ts),bg,f); break
            elif c.get("type")=="tool_result" and c.get("tool_use_id") in pend:
                name,t0,bg,ff=pend.pop(c["tool_use_id"])
                if not bg:
                    dt=t(ts)-t0
                    if dt<0: dt+=86400
                    res[name].append(dt)
for k,v in sorted(res.items(),key=lambda x:-len(x[1])):
    v.sort(); n=len(v)
    print(f"{k:16} n={n:4} p50={v[n//2]:6.0f}s p90={v[int(n*.9)]:6.0f}s max={v[-1]:6.0f}s")
