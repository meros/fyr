# V0.reorder-one: every list the owner orders moves the same way, and a drag feels like one

→ V0.119 (4), V0.120 (4), V0.121, V0.132, V0.137. *Amends V0.119 (4): the phone's Reorder mode also has a handle to drag, and the desktop's drag moves the row itself.* (owner, 19 Nov: "Align all reorder things to work the same way. Make sure desktop and mobile both are really nice to use".)

(1) **One control, four lists.** A provider's Models, Settings → Projects, Settings → Hosts and Waiting to run use one hook (`web/src/reorder-ui.tsx`) and one set of style rules. Before, each list kept its own pending order, its own error and its own lift style, and the Hosts page showed nothing while a row was dragged.

(2) **Desktop.** Each row has a grip. A drag lifts the row: it follows the pointer, the other rows slide out of its way, and the list scrolls by itself near its top and bottom edges. On release the row glides into its place and the new order saves. Esc during a drag puts the row back and saves nothing. The press that ends a drag does not open the row. Alt+↑/↓ on a focused grip moves the row one place, and the focus stays on that grip. With reduced motion the rows move without sliding.

(3) **Phone.** Each list has the same Reorder button where it has its other actions, and the same Done. The Models header's Reorder link is now a button like the others. Waiting to run uses the inbox header's own button, which shows Done as the primary colour. In the mode every row ends in ↑, ↓ and a handle, each 44 px. Dragging the handle works as on the desktop, with a light tick at each new place. While the mode is on, a row does not open. The handle drags only in the mode, because a drag on a normal row would fight the scroll and the swipe.

(4) **Saving.** A move shows on this device at once and is sent as the whole new order. A Models move now sends that order (`modelsAfter` `{order}`) rather than a from and to, so a second move made before the first one lands still goes to the right place. A refusal puts the list's own order back and gives the reason in that list's error line. A list that changes during a drag (a poll, another device) ends the drag.

Tests: `web/test/reorder.test.ts` (where the held row lands, how far the others slide, the row's way to its place, the edge scroll speed), `web/test/providers.test.ts` (`modelsAfter` `{order}`), `web/test/layout.e2e.mjs` section `V0.reorder-one` (a mouse drag and Esc on Projects, Hosts and Models; a touch drag of the phone's handle).
