# V0.draft-close-sync: a closed New thread writes its draft as it leaves the page

→ V0.dialogs-review (3), V0.layout-e2e-load; `web/src/new-thread-form.ts`, `web/src/attach-ui.tsx`, `web/test/layout.e2e.mjs`.

**What failed.** "round 2 draft desktop: a reopened New thread shows no line about its draft" kept failing now and then on the self-hosted runner (test.yml on main, and pr.yml on a PR that touched neither New thread nor drafts) after V0.layout-e2e-load (2) had made the check press `c` again until the dialog opened. So the lost key press was not the cause.

**The cause, in the app.** The form keeps its draft in a `useEffect` cleanup that runs as the form unmounts (V0.dialogs-review (3)). Preact 11 runs a `useEffect` cleanup on unmount only after the next paint (`preact/hooks`: `unmountCleanups`, flushed with the after-paint effects, a frame or a 35 ms timer later), not at the commit that takes the dialog out of the page. A New thread opened again in that gap reads the storage in its first render (`loadDraft`), before the closed form has written. It opens empty, with no line about the draft, and the text typed is gone from the form. The closed form's write lands a moment later, and the new form's own timer then writes its empty form over it. On a busy machine the frame comes late and the gap grows, which is why only the loaded runner showed it. The owner could hit it too: Esc, then `c` at once.

The files of the form had the same gap: `useAttachments` handed the chips it keeps to the next form (`held`) in a `useEffect` cleanup too.

**The fix.** Both cleanups are layout effects now (`useLayoutEffect`), which Preact still runs in the commit, so the draft and the kept chips are written before any later render can read them. The draft's debounce timer is a layout effect as well, so the closed form's pending write is cancelled in the same commit and cannot land after the new form has opened.

**The check.** The layout test now closes New thread and presses `c` in one task, before a frame can be painted, and needs the line. Before this fix that order fails every time; the old check only caught it when the runner was slow.

**How it was found.** Run alone the section passed. Beside busy loops on every core it failed once in 25 runs, and then passed 48 times with CPU throttling and 80 times side by side in eight tabs: locally the frame came too soon to hit the gap. The cause was found by reading why the draft could be missing in the reopened form: the write was in a cleanup whose timing Preact 11 changed. The status screen keeps its look time in a `useEffect` cleanup the same way (`web/src/home-ui.tsx`); it is left as it is here.
