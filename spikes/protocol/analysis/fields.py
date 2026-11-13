import sys, re, os, glob
root=sys.argv[1]; vers=sys.argv[2:]
T={"initialize":"SDKControlInitializeRequest","can_use_tool":"SDKControlPermissionRequest","hook_callback":"SDKHookCallbackRequest","interrupt":"SDKControlInterruptRequest","set_model":"SDKControlSetModelRequest","set_permission_mode":"SDKControlSetPermissionModeRequest","stop_task":"SDKControlStopTaskRequest","mcp_set_servers":"SDKControlMcpSetServersRequest","rewind_files":"SDKControlRewindFilesRequest","get_usage":"SDKControlGetUsageRequest"}
def fields(s,t):
    m=re.search(r"declare type "+t+r" = \{(.*?)\n\};", s, re.S)
    if not m: return None
    body=re.sub(r"/\*\*.*?\*/","",m.group(1),flags=re.S)
    return {f for f in re.findall(r"^    ([a-zA-Z_]+)\??:", body, re.M)}
prev={}; prevm=None
for v in vers:
    d=os.path.join(root,"v"+v,"package"); s=open(os.path.join(d,"sdk.d.ts")).read()
    mjs=set()
    for f in glob.glob(os.path.join(d,"*.mjs")): mjs|=set(re.findall(r'subtype:"([a-z_]+)"',open(f).read()))
    out=[]
    for k,t in T.items():
        f=fields(s,t)
        if f is None: out.append(f"{k}:MISSING"); continue
        if k in prev and prev[k]!=f: out.append(f"{k}: +{sorted(f-prev[k])} -{sorted(prev[k]-f)}")
        prev[k]=f
    print(v, "; ".join(out) or "(no field change in needed types)")
    if prevm is not None: print("   mjs +", sorted(mjs-prevm), " -", sorted(prevm-mjs))
    prevm=mjs
print("init fields latest:", sorted(prev["initialize"]))
