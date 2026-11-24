# V0.toast-dark: the toast and its Undo are readable in dark mode, on both fronts

The toast (`.toast` on desktop, `.m-toast` on the phone) used the night tone, which in dark mode is almost the page, and a report showed a light box with a dark, faint Undo button. In dark mode (system dark with the app on Auto, or the app set to Dark) the toast now sits on the `--select` surface with `--ink` text and a `--line-strong` border, and its Undo button and key hint take the same ink on a transparent background. The error toast is unchanged. One rule serves both fronts (`web/src/thread.css`).
