import json,statistics as st,re,collections,bisect
S=[json.loads(l) for l in open("samples.jsonl")]
try: P=[json.loads(l) for l in open("probe.jsonl")]
except FileNotFoundError: P=[]
CORES=4
def q(v,p): v=sorted(v); return v[min(len(v)-1,int(p*len(v)))]
def row(name,v): print(f"{name:28} p10={q(v,.1):7.2f} p50={q(v,.5):7.2f} p90={q(v,.9):7.2f} max={max(v):7.2f}")
print(f"samples={len(S)} span={(S[-1]['t']-S[0]['t'])/60:.1f} min, probe={len(P)}")
sig={
 "cpu some avg10 %":lambda s:s["psi"]["cpu"]["some"]["avg10"],
 "mem some avg10 %":lambda s:s["psi"]["memory"]["some"]["avg10"],
 "mem full avg10 %":lambda s:s["psi"]["memory"]["full"]["avg10"],
 "io some avg10 %":lambda s:s["psi"]["io"]["some"]["avg10"],
 "io full avg10 %":lambda s:s["psi"]["io"]["full"]["avg10"],
 "load1 / cores":lambda s:float(s["load"][0])/CORES,
 "run queue (procs_running)":lambda s:s["run"],
 "mem available GB":lambda s:s["mem_avail_kb"]/1048576,
 "swap used GB":lambda s:s["swap_used_kb"]/1048576,
}
for k,f in sig.items(): row(k,[f(s) for s in S])
# interval PSI from totals (10 s windows)
def rate(res,kind):
    out=[]
    for a,b in zip(S,S[1:]):
        dt=(b["t"]-a["t"])*1e6
        out.append(100*(b["psi"][res][kind]["total"]-a["psi"][res][kind]["total"])/dt)
    return out
row("cpu some (10s from total) %",rate("cpu","some"))
row("mem full (10s from total) %",rate("memory","full"))
# agents total cores from cgroup deltas
def cores(name):
    out=[]
    for a,b in zip(S,S[1:]):
        if name in a["cg"] and name in b["cg"] and a["cg"][name]["cpu_usec"] and b["cg"][name]["cpu_usec"]:
            out.append((b["cg"][name]["cpu_usec"]-a["cg"][name]["cpu_usec"])/((b["t"]-a["t"])*1e6))
    return out
tot=[]
for a,b in zip(S,S[1:]):
    c=0
    for n in b["cg"]:
        if n in a["cg"] and a["cg"][n]["cpu_usec"] and b["cg"][n]["cpu_usec"]: c+=(b["cg"][n]["cpu_usec"]-a["cg"][n]["cpu_usec"])/((b["t"]-a["t"])*1e6)
    tot.append(c)
row("fyr cgroups cores used",tot)
# per-thread
print("\nper cgroup: mean cores, p90 cores, max mem.current GB, peak (lifetime) GB, swap GB, top command")
names=set(n for s in S for n in s["cg"])
cmds=collections.defaultdict(collections.Counter)
for s in S:
    for n,c in s["cg"].items():
        if c.get("top_cmd"): cmds[n][re.sub(r"\s+"," ",c["top_cmd"])[:70]]+=1
rows=[]
for n in names:
    c=cores(n)
    if not c: continue
    mem=[s["cg"][n]["mem"]/2**30 for s in S if n in s["cg"] and s["cg"][n]["mem"]]
    pk=max((s["cg"][n]["peak"] or 0) for s in S if n in s["cg"])/2**30
    sw=max((s["cg"][n]["swap"] or 0) for s in S if n in s["cg"])/2**30
    rows.append((st.mean(c),q(c,.9),max(mem),pk,sw,n,cmds[n].most_common(1)[0][0] if cmds[n] else ""))
for r in sorted(rows,reverse=True): print(f"{r[5]:20} {r[0]:5.2f} {r[1]:5.2f} {r[2]:5.2f} {r[3]:5.2f} {r[4]:5.2f}  {r[6]}")
# probe join
if P:
    ts=[s["t"] for s in S]
    pairs=[]
    for p in P:
        i=bisect.bisect_left(ts,p["t"]); i=min(max(i,0),len(S)-1)
        pairs.append((p,S[i]))
    row("\nprobe stretch (wall/cpu)",[p["stretch"] for p,_ in pairs])
    row("probe sleep overshoot ms",[p["sleep_over_ms"] for p,_ in pairs])
    def rank(v):
        o=sorted(range(len(v)),key=lambda i:v[i]); r=[0]*len(v)
        for k,i in enumerate(o): r[i]=k
        return r
    def spear(x,y):
        rx,ry=rank(x),rank(y); return st.correlation(rx,ry)
    y=[p["stretch"] for p,_ in pairs]
    print("\nSpearman rho with probe stretch:")
    for k,f in sig.items():
        x=[f(s) for _,s in pairs]
        try: print(f"  {k:28} {spear(x,y):+.2f}")
        except Exception as e: print(k,e)

# slots attribution
import os
SL=[json.loads(l) for l in open("slots.jsonl")] if os.path.exists("slots.jsonl") else []
if SL:
    st_=[x["t"] for x in SL]
    held_c=0; unheld_c=0; heavy_unheld=collections.Counter()
    waits=[len(x["slots"]["waiting"]) for x in SL]
    holders=collections.Counter(h["owner"]+":"+h["kind"] for x in SL for h in x["slots"]["held"])
    for a,b in zip(S,S[1:]):
        if b["t"]<SL[0]["t"]: continue
        i=min(bisect.bisect_left(st_,b["t"]),len(SL)-1)
        held={h["owner"] for h in SL[i]["slots"]["held"]}
        dt=(b["t"]-a["t"])*1e6
        for n in b["cg"]:
            if n in a["cg"] and a["cg"][n]["cpu_usec"] and b["cg"][n]["cpu_usec"] and n!="server":
                c=(b["cg"][n]["cpu_usec"]-a["cg"][n]["cpu_usec"])/dt
                if n in held: held_c+=c
                else:
                    unheld_c+=c
                    if c>0.5: heavy_unheld[n]+=1
    print(f"\nslot samples={len(SL)}; queue length max={max(waits)}; holders seen={dict(holders)}")
    print(f"CPU share by threads holding a slot: {held_c/(held_c+unheld_c):.0%}; without: {unheld_c/(held_c+unheld_c):.0%}")
    print("threads without a slot using >0.5 core (count of 10 s intervals):",dict(heavy_unheld.most_common(8)))
VM=[json.loads(l) for l in open("vmstat.jsonl")] if os.path.exists("vmstat.jsonl") else []
if len(VM)>2:
    r=lambda k:[(b[k]-a[k])/(b["t"]-a["t"]) for a,b in zip(VM,VM[1:])]
    row("\nswap-in pages/s",r("pswpin")); row("swap-out pages/s",r("pswpout")); row("major faults/s",r("pgmajfault"))
if P:
    print("\nprobe stretch by cpu some avg10 band:")
    for lo,hi in ((0,80),(80,90),(90,95),(95,101)):
        v=[p["stretch"] for p,s in pairs if lo<=s["psi"]["cpu"]["some"]["avg10"]<hi]
        if v: print(f"  {lo}-{hi}%: n={len(v)} median stretch={st.median(v):.1f}")
    print("probe stretch by run-queue/cores band:")
    for lo,hi in ((0,4),(4,8),(8,12),(12,16),(16,99)):
        v=[p["stretch"] for p,s in pairs if lo<=s["run"]/CORES<hi]
        if v: print(f"  {lo}-{hi}: n={len(v)} median stretch={st.median(v):.1f}")
    print("probe stretch by load1/cores band:")
    for lo,hi in ((0,4),(4,8),(8,12),(12,99)):
        v=[p["stretch"] for p,s in pairs if lo<=float(s["load"][0])/CORES<hi]
        if v: print(f"  {lo}-{hi}: n={len(v)} median stretch={st.median(v):.1f}")
