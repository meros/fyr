# V0.phone-chips-scroll: the phone's header chips keep their width and the row scrolls sideways

→ amends V0.agent-pill. On the phone the chips and the model chooser were squeezed.

1. The thread header's chip row (`.m-chipsd`) scrolls sideways; no chip shrinks to an ellipsis (branch, change, agent pill are `flex:none`).
2. In Details the agent field gives the provider and effort what they need and the model the rest, so the model never reads "S…".
