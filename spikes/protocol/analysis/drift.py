import sys, re, os, glob, json
root = sys.argv[1]; vers = sys.argv[2:]
NEED = ["initialize","can_use_tool","hook_callback","interrupt","set_model","set_permission_mode","stop_task","mcp_set_servers","rewind_files","get_usage"]
def union(s, name):
    m = re.search(r"declare type "+name+r" = ([^;]+);", s)
    return [x.strip() for x in m.group(1).split("|")] if m else []
def subtype_of(s, t):
    m = re.search(r"declare type "+t+r" = \{(.*?)\n\};", s, re.S)
    if not m: return t+"?"
    st = re.search(r"subtype: ['\"]([a-z_]+)['\"]", m.group(1))
    return st.group(1) if st else t+"?"
res = {}
for v in vers:
    d = os.path.join(root, "v"+v, "package")
    s = open(os.path.join(d, "sdk.d.ts")).read()
    inner = union(s, "SDKControlRequestInner")
    dts = {subtype_of(s, t) for t in inner}
    mjs = set()
    for f in glob.glob(os.path.join(d, "*.mjs")):
        mjs |= set(re.findall(r'subtype:"([a-z_]+)"', open(f).read()))
    msgs = set(union(s, "SDKMessage"))
    res[v] = dict(dts=dts, mjs=mjs, msgs=msgs)
    print(f"{v}: d.ts control subtypes={len(dts)} mjs subtype literals={len(mjs)} SDKMessage members={len(msgs)}")
    print("   needed missing from d.ts union:", [n for n in NEED if n not in dts], " missing from mjs:", [n for n in NEED if n not in mjs and n not in ("can_use_tool","hook_callback")])
for a, b in zip(vers, vers[1:]):
    A, B = res[a], res[b]
    print(f"\n== {a} -> {b}")
    for k in ("dts","msgs"):
        add = sorted(B[k]-A[k]); rem = sorted(A[k]-B[k])
        print(f"  {k}: +{add}\n  {k}: -{rem}")
print("\nfirst version present (d.ts union) per needed subtype:")
for n in NEED:
    print("  ", n, next((v for v in vers if n in res[v]["dts"]), "never"))
