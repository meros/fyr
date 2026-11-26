# V0.paste-inline: a file pasted into a text field goes in the text where it was pasted

→ V0.18. *Amends V0.18: a paste also writes a marker in the text.*

Why: where in a sentence a screenshot was pasted often carries meaning ("this button [image-1.png] should sit left of this one [image-2.png]"), and a pasted file only became a chip under the field, so the agent read every file after the text with no tie to a place in it.

(1) **The marker.** A file pasted into a composer (a thread's, New thread's, on desktop and phone) uploads as a chip as before and puts `[name]` in the text at the caret, replacing a selection, with a space where it would touch a word. It goes in the way typed text does, so undo takes it back. The agent gets the text with the marker where it was, and the file's `Attached: name (…) at path` line after it, as before, so the marker names its file.

(2) **Names.** A clipboard image comes as `image.png` or without a name; each is named `image-N.<ext>` with the first N no chip in that composer has. A pasted file with a name of its own keeps it unless a chip already has it (`name-N.ext`).

(3) **Unchanged.** The paperclip, Camera, Photos and Files pickers and a drop add a chip alone, with no marker. Removing a chip leaves its marker in the text for the owner to edit. The sent message shows the text with its markers and the files below it.

Never interrupt the owner: the marker comes only from the owner's own paste into the field they are typing in, at their caret.
