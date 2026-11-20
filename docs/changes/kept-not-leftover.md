# V0.kept-not-leftover: a process the agent kept stays off its thread's leftover strip

→ V0.176. (#353)

(1) **What happened.** On 19 Nov at 11:59 an agent started a 3 h build detached (`setsid nohup timeout 3h …`), kept it with `keep_process`, held an admitted build slot, and waited for it with a background Bash. Its turn ended at 12:52. The build is outside the CLI's process tree, so the thread's strip showed it as "1 leftover process: uv (1.4 GB) · kept for 2 h: …" with End. At 13:05:48 the owner pressed End and the build died in its last step.

(2) **The change.** The strip under an open thread's header, on desktop and phone (one component), leaves out a process the agent kept, until the keep expires. It is the agent's work, not something left behind. A process the owner kept stays on the strip. Settings → Automation still lists every leftover, kept ones too, with End and Keep. The policy (what Fyr ends, and when) is unchanged.

Tests: `web/test/leftovers.test.ts` ("a process the agent kept stays off its thread's strip").
