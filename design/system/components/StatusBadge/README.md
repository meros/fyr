# StatusBadge

The one status vocabulary: an icon plus a word, for each of the nine states Attention computes.

- Each state has exactly one icon and one label everywhere: row, thread header, notification, palette.
- Shape carries the state, colour only reinforces it: asking is a filled disc in `beam-mark`, failed a diamond in `signal-ink`, blocked a ring with a centre in `harbour`, done a small dot in `ink`.
- Working, waiting, seen and snoozed are deliberately grey (`ink-muted`): work in progress must not compete with what needs the user.
- Never invent a state in a module. Modules add Signals that map onto these nine.
