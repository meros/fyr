import sys, time, os
step=int(sys.argv[1]); n=int(sys.argv[2]); bufs=[]
cg=open('/proc/self/cgroup').read().strip().split('::')[-1]
base='/sys/fs/cgroup'+cg
print('cgroup', cg, flush=True)
for i in range(n):
    t=time.time(); bufs.append(os.urandom(step*1024*1024)); dt=time.time()-t
    cur=int(open(base+'/memory.current').read())//2**20
    sw=int(open(base+'/memory.swap.current').read())//2**20
    ev=open(base+'/memory.events').read().replace('\n',' ')
    pr=open(base+'/memory.pressure').read().split('\n')[0]
    print(f'{(i+1)*step}MB alloc dt={dt:.2f}s cur={cur}M swap={sw}M | {ev}| {pr}', flush=True)
print('done', flush=True)
