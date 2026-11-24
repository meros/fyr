# V0.desktop-system-theme: the Linux app's page follows the desktop's colour scheme, not a GTK_THEME of the session

→ amends V0.linux-desktop (the window).

Why: a session that exports `GTK_THEME=<theme>-dark` made WebKitGTK answer `prefers-color-scheme: dark` while the desktop's colour scheme (gsettings and the portal) was light, so the page, which follows the system by default, showed its dark theme in light mode.

1. `fyr-desktop` unsets `GTK_THEME` before the window starts. The page then follows the desktop's colour scheme; an owner's own choice in Settings (Light or Dark) still wins.
