# V0.machine-wait-restart: a wait for the machine ends when the host that waited is gone

→ V0.153, V0.90, V0.6. 

(1) **The cause.** The wait is saved on the thread, but the start that ends it lives in the host's memory. When the host stopped during the wait, nothing was left to end it, and a thread that was not running was skipped by the repair at the next start. The line stayed through later runs, until the thread's process was lost during a run. On 18 Nov this happened at 07:22 (a host restart that kept the process) and at 15:30 (systemd-oomd killed the host).

(2) **The repair.** When a host starts, every thread's wait ends, whether or not the runner kept its process. The next start of the agent waits again and says so if the host is still under pressure. Waits left by an earlier host end at the first start of this version.
