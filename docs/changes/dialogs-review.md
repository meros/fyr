# V0.dialogs-review: New thread starts with the prompt and loses nothing typed, Back and Esc go one level up, and no key acts under an open dialog

→ V0.30 (4), V0.42, V0.45, V0.61, V0.63, V0.67, V0.68, V0.139, V0.185, V0.composer-focus-reach (2), V0.trans-host-handover. Amends V0.61 (what Enter does in the project search) and V0.63 (what closes the New project step).

Why: a review of every dialog, sheet and menu outside Settings, run against the built client on both fronts, found work that could be lost and keys that acted in the wrong place. The numbers in brackets are the review's findings.

- New thread asked for the prompt last, under five fields that keep their defaults, and on a 720 px window one line of it showed. Esc, a click beside the dialog or × threw the whole form away, from the project list and the New project step too.
- With the Merge dialog open, `#` archived the thread behind it and `s` opened Snooze on top of it: the app's key handler knew only the dialogs it opens itself.
- On the phone, Back with the folder picker, a confirmation or the New project step open left the screen under it; the folder picker then stood over the inbox.
- Enter in the project search made a project named like the typed part of an existing project's name.
- Errors of New thread were printed below the visible part of the dialog.

(1) **New thread, one form on both fronts.** The order is: the prompt (no label; placeholder "What should the agent do?", "Add a note (optional)" with a file), the attach row, Host (only with several hosts), Project, Folder, Agent, and a fold "More" with Title and Live view. The desktop dialog and the phone screen have the same fields, order and words; the state is one hook (`web/src/new-thread-form.ts`) over one pure model (`web/src/new-thread.ts`).
- The prompt has the focus when the desktop dialog opens, and again after a project or a folder is picked and after a project is made (9). It grows with its text up to 40 % of the window's height, then scrolls (18).
- The Host field has no standing help line here; its help is the label's tooltip, and an offline host's note still shows under it.
- The Folder line has a hint only when the hint says something the Project field does not.
- More is open when Title or Live view has a value. Live view's help is text beside the box, not a tooltip. Enter in Title starts the thread, as in the prompt (17).
- The form's foot (desktop) and bottom bar (phone) hold what must be seen without scrolling: the error of the last try (`role="alert"`), else why Start is off, and "Draft restored · Clear". The focus goes to the field the error is about (5).
- Start is off while there is nothing to send or the host cannot take the thread, on both fronts, with the reason beside it (23): "Write a prompt or attach a file.", "lab is offline since 19:40.".
- A dialog stands 6vh from the top and is at most 88vh tall (it was 12vh and 76vh). Settings keeps its size and place.
- The phone differs where touch or width forces it, each with its reason at the code: no focus on open and no return of the focus (a focused field raises the keyboard); Enter is a new line (no modifier key); four attach buttons, Paste, Camera, Photos and Files, in one row where the desktop has a paperclip and "Paste or drop files" (no drop on touch, separate system pickers); every provider's quota under More where the desktop shows the picked one's (no rail); "Add … on …" is 44 px high.

(2) **One primary button (11).** While the New project step is open, Start reads "Create project and start", and the step's own "Create project" is a plain button.

(3) **The New project step in New thread.**
- Its folder field is labelled "Project folder" (13), also in Settings and in the Project picker, which share the step. While the step is open the thread's own Folder row is hidden, and one line says where the thread starts, about the project being made (6): "Zebra is new and has no folder: the thread starts in a fresh scratch folder."
- What is typed in it stays while New thread is open (12): Back shows the project list again with the name in its search, the Project button closes the step, and both keep the name and the folder for the next time the step opens. Another host keeps the step open with its name; its folder, and a folder picked for the thread, are paths of the other host and go, and a line under Host says what went. Only making the project ends it. This amends V0.63, where Cancel and the Project button dropped it. The Project picker (file a thread) keeps the step's name and folder in the same way.
- A button that adds a project on another host names that host, not its install id (7).

(4) **Nothing typed is lost (3).** What the owner typed in New thread is a draft: the prompt, the title, Live view, the New project step's name and folder, and the picks made by hand (host, project, folder, provider, model, effort). It is kept in this browser's storage under `fyr.newThreadDraft` as the form changes, so every way of closing the form keeps it (Esc, the dimmed area, ×, the phone's Back and ×, a reload), and the next New thread opens with it and says "Draft restored · Clear". Clear empties the form. A started thread removes the draft. Picks alone are not a draft: without a word typed the form opens on its defaults. A pick that no longer exists (a project, a provider, the host) leaves the form's default; a draft whose host is gone keeps its words. Attached files are not part of the draft.

(5) **Esc and Back go one level up.** In New thread Esc closes, in this order: the `/` list, the New project step (to the project list), the project list, the dialog. On the phone Back does the same for the step and the list; the header's × leaves the screen in one step.

(6) **The phone's Back closes the top-most layer (4).** The folder picker, a confirmation, a dialog shown as a sheet (the Merge dialog), the diff comment sheet, and the project list and New project step inside New thread and inside the Project sheet are layers: each has a history entry of its own while it is up, with the screen's own state and URL, so Back closes it and the screen under it stays (`web/src/back-layer.ts`). A layer closed on the page takes its entry out again. A navigation asked for while such an entry is on its way out waits for it, and a navigation never lands on top of a layer's entry. A Back control of the page (the header's arrow or ×, a sheet's close) steps over the layers of what it leaves.

(7) **No app key acts under an open dialog (1).** The desktop's key handler returns while a modal panel is open that its `dialog` state does not know, and while a second panel is open over a dialog. It reads the focus trap's stack, which every dialog joins, so a later dialog cannot forget it. The Merge dialog's Esc and Enter are its own. Esc that closes the folder or quota popover goes no further (21): it no longer also interrupts the run or closes the thread.

(8) **The first focus of a dialog is not its × (10).** The focus starts on the first control that is not the header's ×: the first choice in Snooze (the arrows walk the choices), the search in Changes, and "To" in Hand over.

(9) **Enter in the project search takes the first row (2).** *Amends V0.61.* Under typed text the matches come first and "New project “text”" after them. Enter picks the project named exactly like the text, else the first match; it starts New project only when nothing matches, where that row is the only one. V0.61 had Enter make a project for every text that was not an exact name, so a part of a name made a project called like that part. With an empty search the New project row stays first (V0.30). The picked project is scrolled into view when the list opens. On the phone the Project sheet does not focus its search (14): the focus raised the keyboard over a list whose usual use is one tap.

(10) **Snooze: a date and time of the owner's own on both fronts (15).** The last choice is "Pick a date and time…" (desktop key `t`). It opens one field that starts on tomorrow 09:00 and takes no time before now. The desktop had no such choice; the phone's started empty.

(11) **Hand over (16).** The dialog is wide. Across hosts the button reads "Move to <host> and settle here", and the sentence above the form marks "and settles this thread". How the context goes over stands between the form and the buttons, outside the part that scrolls. Enter confirms and the foot names Esc and Enter, as in the confirmation; not on the phone, which has no keyboard, where the two buttons centre their words like every other sheet's (30).

(12) **The Merge dialog (27).** It starts on the method the repository was merged with last on this device (`fyr.mergeMethod`), stays open with the host's error when the merge does not start, and names Esc and Enter in its foot on the desktop.

(13) **One word for one thing (26).** The folder picker's button is "Change…" everywhere. New thread's button is "Start" and its key hint "start". The prompt says "the agent", never a provider's name; the phone's peek names the thread's provider. A thread without a folder of the owner's starts in "A fresh scratch folder" in New thread and Hand over. Hand over's texts use ":" where they had a dash.

(14) **Smaller.** "End change" is red like the other confirmations that cannot be taken back (28); why a tap beside a confirmation cancels on the phone and does nothing on the desktop is written at the code. The phone's Details shows the folder short ("~/…") and the host with several hosts (29). A row of the phone's thread sheet that is off names no swipe. `&dialog=handover` and `&dialog=picker` open the open thread's Hand over and Project dialogs, and `web/NOTES.md` lists the links that exist (32).

Not in this change, each a decision about the product and not a repair: a change card that shows two Merge buttons (20), one layout for the two desktop menus and their keys (24), groups and icons in the phone's thread sheets (25), filing several picked threads on the phone (31), and a key or a button for Hand over (32).

Tests: `web/test/dialogs-review.test.ts` (the draft, the New project step, the folder line, why Start is off, Enter in the project search, the snooze date and time, the first focus, the open panels, the Merge dialog's method, the Back layers), `web/test/projects.test.ts` (V0.61 as amended), `web/test/offers.test.ts` (End change is red); `web/test/layout.e2e.mjs` follows the new words ("Change…", the Host field's tooltip, Hand over's key hint).
