# V0.toast-pressed-colors

The desktop toast's Undo button went blank while pressed: the solid-button press rule (`layout.css`, `[data-pressed]:not(.p)`) paints `--sunken`, a light well, and outranks `.toast .btn`, whose text is the light `--on-night`. The toast now has its own pressed well, a translucent white as the phone's `.m-toast .tb` has, so the text stays readable in both themes.
