# V0.load-tuning: heavy jobs are predicted from their highs, only the agents' own memory stall cuts their budget, and the guard pauses the job that grew

→ V0.148, V0.151, V0.163, decision 0029 S3, S4, S5. *Amends V0.148 (the demand's average), V0.151 (the newest job) and V0.163 (what cuts the memory budget).* (owner, 18 Nov, after reading a day of load logs: "review, research and do a tweak PR".)

(1) **CPU demand.** A class's learned demand follows its runs' highs and decays by 10 % a run, as its memory peak does. A starved run still moves it by α/2, because its demand says little. Replayed over the 170 recorded runs with a prediction (17–18 Nov): runs more than 1.5 cores above their prediction fall from 25 % to 11 %; the prediction is on average 1.5 cores above the run instead of 0.7, which the CPU budget's growth on a calm host can give back.

(2) **Memory budget.** The budget controller cuts the memory budget when the agents' headroom is under the reserve or the agents' own stall (`agents/memory.pressure` full avg10) is critical. A critical stall of the rest of the host still holds new jobs and still pauses one, and it keeps the budget from growing, but it no longer cuts it. Without that reading (no cgroups) the host's stall counts as before. On 18 Nov 21 of the 24 minutes of "memory short" came from the host-wide stall reading while the agents' headroom was above the reserve.

(3) **The pause.** Under the memory guard the job whose memory grew most since its admission is paused, also when it is the oldest; of equals the newest. One job always runs, and a hold is paused at most once. Without a reading of growth the rule stays V0.151's.

(4) **Log.** `slots: budget` says each move of the budget and why, `slots: admitted … waited=` how long a job queued, `slots: left the queue` a wait given up, and the guard's lines add `agents_stall=`. `GET /api/load` has `agentsMemoryStall`; nothing on the fronts changes.
