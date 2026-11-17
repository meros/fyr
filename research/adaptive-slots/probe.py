import time,json,sys,resource
out=open(sys.argv[1],"a"); end=time.time()+int(sys.argv[2])
def burn(n):
    x=0
    for i in range(n): x+=i*i
# calibrate: CPU time of burn
N=200000
while time.time()<end:
    r0=resource.getrusage(resource.RUSAGE_SELF); w0=time.perf_counter()
    burn(N)
    w1=time.perf_counter(); r1=resource.getrusage(resource.RUSAGE_SELF)
    cpu=(r1.ru_utime-r0.ru_utime)+(r1.ru_stime-r0.ru_stime)
    s0=time.perf_counter(); time.sleep(0.005); sl=time.perf_counter()-s0-0.005
    out.write(json.dumps({"t":time.time(),"wall":w1-w0,"cpu":cpu,"stretch":(w1-w0)/max(cpu,1e-4),"sleep_over_ms":sl*1000})+"\n"); out.flush()
    time.sleep(5)
