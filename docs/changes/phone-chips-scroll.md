# V0.phone-chips-scroll: the phone's header chips keep their width and the row scrolls sideways

→ amends V0.agent-pill. On the phone the chips and the model chooser were squeezed.

1. The whole thread header chip row (`.m-chips`) scrolls sideways as one rail; no chip shrinks to an ellipsis (branch, change, agent pill are `flex:none`), and the header's agent pill shows the provider's dot, the full model name and the effort meter.
2. In Details the Agent row spans the full width of the sheet, with the read-only provider on a top band and the 48 px model and effort pickers sharing the full width underneath, so long provider and model names never squeeze.
