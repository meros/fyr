import json,bisect,statistics as st
S=[json.loads(l) for l in open("samples.jsonl")]; P=[json.loads(l) for l in open("probe.jsonl")]
VM=[json.loads(l) for l in open("vmstat.jsonl")]
ts=[s["t"] for s in S]; vt=[v["t"] for v in VM]
pairs=[]
for p in P:
    i=bisect.bisect_left(ts,p["t"])
    if i>=len(S) or abs(S[i]["t"]-p["t"])>12: continue
    j=bisect.bisect_left(vt,p["t"])
    sw=None
    if 0<j<len(VM): sw=(VM[j]["pswpin"]-VM[j-1]["pswpin"])/(VM[j]["t"]-VM[j-1]["t"])
    pairs.append((p,S[i],sw))
print("joined",len(pairs),"span min",(pairs[-1][0]["t"]-pairs[0][0]["t"])/60)
C=4
sig={"cpu PSI some avg10":lambda s,w:s["psi"]["cpu"]["some"]["avg10"],
"load1/cores":lambda s,w:float(s["load"][0])/C,
"run queue/cores (instant)":lambda s,w:s["run"]/C,
"mem PSI some avg10":lambda s,w:s["psi"]["memory"]["some"]["avg10"],
"mem PSI full avg10":lambda s,w:s["psi"]["memory"]["full"]["avg10"],
"io PSI some avg10":lambda s,w:s["psi"]["io"]["some"]["avg10"],
"MemAvailable (low=bad)":lambda s,w:-s["mem_avail_kb"],
"swap used":lambda s,w:s["swap_used_kb"],
"swap-in rate":lambda s,w:w if w is not None else 0}
def auc(x,y):
    pos=[a for a,b in zip(x,y) if b]; neg=[a for a,b in zip(x,y) if not b]
    if not pos or not neg: return float("nan")
    n=0
    for a in pos:
        for b in neg: n+= 1 if a>b else (.5 if a==b else 0)
    return n/(len(pos)*len(neg))
for thr in (2,4):
    y=[p["stretch"]>thr for p,_,_ in pairs]
    print(f"\nAUC for 'stretch>{thr}' (share positive {sum(y)/len(y):.0%}):")
    for k,f in sorted(sig.items(),key=lambda kv:-auc([kv[1](s,w) for _,s,w in pairs],y)):
        print(f"  {k:28} {auc([f(s,w) for _,s,w in pairs],y):.2f}")
st_=[p["stretch"] for p,_,_ in pairs]
print("\nstretch p50/p90/max",st.median(st_),sorted(st_)[int(.9*len(st_))],max(st_))
print("share of time stretch>2: %.0f%%, >4: %.0f%%"%(100*sum(x>2 for x in st_)/len(st_),100*sum(x>4 for x in st_)/len(st_)))
print("cpu some avg10 by band -> median stretch")
for lo,hi in ((0,20),(20,50),(50,80),(80,90),(90,101)):
    v=[p["stretch"] for p,s,_ in pairs if lo<=s["psi"]["cpu"]["some"]["avg10"]<hi]
    if v: print(f"  {lo}-{hi}%: n={len(v)} median {st.median(v):.1f} p90 {sorted(v)[int(.9*len(v))]:.1f}")
print("load1/cores band -> median stretch")
for lo,hi in ((0,1),(1,2),(2,4),(4,8),(8,99)):
    v=[p["stretch"] for p,s,_ in pairs if lo<=float(s["load"][0])/C<hi]
    if v: print(f"  {lo}-{hi}: n={len(v)} median {st.median(v):.1f}")
