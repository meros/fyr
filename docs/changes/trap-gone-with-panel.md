# V0.trap-gone-with-panel: a panel that has left the page no longer holds the app's keys

`openTraps()` (V0.dialogs-review (1)) counted the panels whose focus trap had not been dropped. The drop runs in the cleanup of the panel's effect, a moment after its DOM is gone. A key pressed in that moment (found with the layout test: `c` right after New thread closed, under load; the trace showed the key at 1759 ms and the drop at 1760 ms) was swallowed as "a panel is open", and no new thread opened.

1. A trap knows its panel; a panel that is no longer in the page does not count in `openTraps()` and is never the top trap.
2. The cleanup still drops the trap. This closes the gap; it does not move the drop.
3. This is one cause of the layout check "a reopened New thread shows no line about its draft" failing now and then, not the whole of it: 4 failures were traced to a trap left after the dialog closed, and one more failure came with this change built in. That one is not explained and stays open.
