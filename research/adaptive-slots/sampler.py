import json, os, time, sys
ROOT="/sys/fs/cgroup/system.slice/fyr.service"
OUT=sys.argv[1]; DUR=int(sys.argv[2]); STEP=10
def rd(p):
    try: return open(p).read()
    except Exception: return None
def psi(p):
    t=rd(p); r={}
    if not t: return r
    for line in t.splitlines():
        k,*kv=line.split(); d=dict(x.split("=") for x in kv)
        r[k]={"avg10":float(d["avg10"]),"avg60":float(d["avg60"]),"total":int(d["total"])}
    return r
def cpu_usec(p):
    t=rd(p+"/cpu.stat")
    if not t: return None
    for l in t.splitlines():
        if l.startswith("usage_usec"): return int(l.split()[1])
def num(p):
    t=rd(p); 
    try: return int(t.strip())
    except Exception: return None
def meminfo():
    d={}
    for l in open("/proc/meminfo"):
        k,v=l.split(":"); d[k]=int(v.split()[0])
    return d
def procs_running():
    for l in open("/proc/stat"):
        if l.startswith("procs_running"): return int(l.split()[1])
def topcmd(cg):
    best=None
    t=rd(cg+"/cgroup.procs") or ""
    # recurse
    pids=[]
    for dp,dn,fn in os.walk(cg):
        x=rd(dp+"/cgroup.procs")
        if x: pids+=x.split()
    for pid in pids:
        try:
            st=open(f"/proc/{pid}/stat").read().rsplit(")",1)[1].split()
            rss=int(st[21])*4096
            cmd=open(f"/proc/{pid}/cmdline").read().replace("\0"," ")[:160]
        except Exception: continue
        if best is None or rss>best[0]: best=(rss,cmd)
    return {"n":len(pids),"top_rss":best[0] if best else 0,"top_cmd":best[1] if best else ""}
end=time.time()+DUR; i=0
with open(OUT,"a") as f:
    while time.time()<end:
        m=meminfo()
        rec={"t":time.time(),"load":open("/proc/loadavg").read().split()[:3],"run":procs_running(),
             "psi":{k:psi(f"/proc/pressure/{k}") for k in ("cpu","memory","io")},
             "mem_avail_kb":m["MemAvailable"],"mem_total_kb":m["MemTotal"],
             "swap_used_kb":m["SwapTotal"]-m["SwapFree"],"cg":{}}
        for base in ("server","agents"):
            for name in ([base] if base=="server" else os.listdir(ROOT+"/agents")):
                p=ROOT+"/"+(name if base=="server" else "agents/"+name)
                if not os.path.isdir(p): continue
                c={"cpu_usec":cpu_usec(p),"mem":num(p+"/memory.current"),"peak":num(p+"/memory.peak"),
                   "swap":num(p+"/memory.swap.current"),
                   "cpu_psi":psi(p+"/cpu.pressure").get("some",{}).get("avg10"),
                   "mem_psi":psi(p+"/memory.pressure").get("some",{}).get("avg10"),
                   "io_psi":psi(p+"/io.pressure").get("some",{}).get("avg10")}
                if i%6==0: c.update(topcmd(p))
                rec["cg"][name]=c
        f.write(json.dumps(rec)+"\n"); f.flush(); i+=1
        time.sleep(STEP)
