# V0.no-interrupt-rest: a live view, a republished page, a late answer and a sign-out wait for the owner

→ U26, V0.no-interrupt (7), V0.37, V0.157. *Amends V0.37 (7): a thread opens on View when it has a live view at the time it is opened; a view that appears later does not switch the tab. Does the four cases V0.no-interrupt (7) left.*

**Why.** V0.no-interrupt left four places where something the agent or the host does still took over what the owner was doing. Each one breaks U26.

(1) **A view that appears does not switch the tab.** A thread still opens on View when it has a live view at the time the owner opens it. A view an agent publishes while the thread is open adds the View tab and leaves the owner on the tab they read. This applies to the desktop's pane and the phone's thread screen alike (`view-ui.tsx` `useOpenedWithView`, `no-interrupt.ts` `unpickedTab`). An explicit tab choice wins as before.

(2) **A republished page waits while the owner is in the frame.** A new `pageId` used to reload the view's frame at once. Now, while the frame has the focus, the old page stays and the view's header shows **New version · Show** (44 px on the phone, for the finger). The new page loads on that press, or within a second of the focus leaving the frame. State updates (`view_state`) still reach the page shown at once, as before. In full screen the button is not shown, and the page comes when the owner leaves the frame.

(3) **No move-on after a late answer.** On the desktop, the next thread that needs the owner opened when the host took the answer. That could happen seconds later, after the owner had moved on. Now it opens only if the answered thread is still open and no key was pressed since the answer (`moveOnAfterAnswer`). The phone only ever offers the next thread, so it is unchanged.

(4) **A sign-out reload waits too.** A revoked device's reload to the sign-in page waits while the owner is busy, by the same rule as a new build's (V0.no-interrupt (4); `activity.ts` `whenIdle`, now shared by both). Nothing the owner types can be sent meanwhile, but their draft and open card are not lost.
