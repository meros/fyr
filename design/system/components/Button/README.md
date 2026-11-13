# Button

One action, one label, one shortcut. Use `primary` (night fill) once per view for the action that moves work forward; `default` for the rest; `ghost` in dense rows; `danger` only for destructive actions.

- Show the shortcut with `kbd` on every action that has one; the label and key are the same on every surface.
- A disabled button always carries the `reason` the host gave (shown as its tooltip). Never disable silently.
- Never colour a button with `beam`: beam marks what needs the user, not what the user can do.
