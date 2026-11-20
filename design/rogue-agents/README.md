# Undeclared heavy work (V0.164)

The design for agents that run heavy work without asking for a heavy job: detect it from the thread's cgroup, tell the agent once, count the use against the host, and pause it only under real memory pressure.

| Path | What |
|---|---|
| `SPEC.md` | **Build from this.** The rules, the edge cases, the owner's answers, the spec row for `docs/SPEC.md`, the test plan and where each part goes in the code. |
| `mock/i2.html` | **Iteration 2: the final screens.** Flagged and paused, at desktop and phone width, and Settings → Automation → Heavy jobs. |
| `mock/i1.html` | Iteration 1: variant A (quiet) and B (louder). The owner chose A's row and rail with B's strip. Kept for the history. |
| `mock/rogue.css` | The proposed new CSS; copy from here. |
| `mock/build.py` | Writes `frames/*.html` and the boards. Run `python3 design/rogue-agents/mock/build.py`. |
| `mock/base/`, `mock/app.css`, `mock/fonts/`, `mock/logo.png` | The real front the frames are made of: the DOM of `web/dist` in mock mode (`?mock=rail`) at 1280 and 390 px with its scripts removed, and the built stylesheet (origin/main dffbf23, 17 Nov). The frames therefore use the real tokens and components; only the parts V0.164 changes are swapped in. |
| `mock/shots/` | Screenshots of the boards and of the main frames (light, one dark). |

Open an `iN.html` file in a browser (Light and Dark at the top right). The frames still say "slot" where today's front does; V0.163 changes those words, not this design.
