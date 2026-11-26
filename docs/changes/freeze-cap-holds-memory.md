# V0.freeze-cap-holds-memory: only a freeze that holds memory fills a slot of the freeze limit

→ V0.guard-wake (3), V0.sim-fixes (3).

On a 30 GiB host the agents sat at 97 % memory stall for minutes while the guard logged "not freezing more: 2 frozen, at most 2". One of the two freezes held 0.0 GB (a small part) and the other was work taking turns (a part runs), so neither freed memory, yet both filled the limit while one thread held 7.5 GB.

(1) `HoldingFreezes` counts the freezes that fill a slot: the whole work frozen, holding `TurnMinRSS` or more. The limit (threads with heavy work less one) and the wait after the last freeze are unchanged.

(2) The log line says "frozen that hold memory".

Not tested against the engine suite yet: `internal/engine` failed in one run, not compared with main.
