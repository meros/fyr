# V0.filmstrip-fold: the filmstrip is one folded line on both fronts, and a thread can hide it until a header chip brings it back

→ 13.5 (the filmstrip), V0.19, V0.75 (2). *Amends V0.75 (2): the desktop's strip was always open, only the phone's folded.*

Why: a thread with many previews spent a tall strip of thumbnails at the top of the conversation on cards the owner had already seen, and each card is in the conversation anyway. The phone already folded the strip into one line; the desktop did not, and neither front could put it away.

1. **Folded by default, on both fronts.** The strip is one line, "N previews ▸", with a × at its end; a press on the line opens the thumbnails under it and a second press folds them. The line and the × are 28 px on the desktop and 44 px on the phone (a finger's size). The fold is not stored; it lasts the page, so a tab switch does not close an open strip. One component draws both fronts (`Filmstrip` in `web/src/preview-ui.tsx`).
2. **Hide, per device and thread.** The × hides the strip for this thread on this device. It is kept in `localStorage` under `fyr.filmstrip.<threadRef>`, where the ref is the thread's client ref `<hostId>/<threadId>` (decision 0031), so a host's id rotation moves it with the other keys. Where storage is blocked the hide lasts the page.
3. **The previews chip brings it back.** While the strip is hidden and the thread has previews, the header's chip row shows "N previews" beside the folder chip (on the phone in its own 44 px button outside the Details button, as the folder chip). A press forgets the hide, moves to the Conversation tab, and shows the strip open, scrolled into view.
4. **Not done here:** folding by itself once the owner has scrolled past every card (a strip now starts folded, so it costs one line already), the same fold for the live view (it is only on the View tab now), and a Settings default for new threads.
