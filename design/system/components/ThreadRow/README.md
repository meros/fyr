# ThreadRow

One thread in the inbox: state icon, title, the reason it is where it is, PR, host and time.

- The reason is the first thing in the meta line and explains the row's position ("2 questions", "checks failed on head").
- Rows in Working, Seen, Snoozed and Settled are dimmed (title in `ink-muted`); rows that need the user keep full `ink`.
- Rows never move under the pointer or keyboard focus; the list reorders when the pointer leaves.
- Subagents never get rows; they live inside their parent's thread view.
