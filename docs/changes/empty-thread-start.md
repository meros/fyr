# V0.empty-thread-start: a thread can start with no message, idle until its first one

→ 5.1 (`thread.create`), V0.auto-title, V0.dialogs-review (5, 23). *Amends 5.1: `text` of `thread.create` is optional.*

Why: the owner sometimes wants a thread in place (a project, a folder, an agent and model picked) before they know what to write, or to write the first message in the thread's own composer. Until now New thread's Start stayed off until there was a prompt or a file, and the host refused an empty create.

(1) **Host.** `thread.create` with no text, attachment or image makes the thread and sends nothing: no run starts, no agent session is made, the thread does not move to Working. A scratch folder is made as for any create without a folder. `thread.send` with nothing in it is still refused.

(2) **Title.** A thread started empty with no title is titled "New thread" (`titleAuto: "empty"`). Its first message names it, as the first message of a create does (`titleAuto: "first"`), and the automatic titles of V0.auto-title follow from there. That rename is Fyr's, so the agent is not told that the owner renamed the thread. A title given at the start stays.

(3) **New thread, both fronts.** One rule in `new-thread.ts` `startArgs`, used by the desktop dialog and the phone screen: with nothing in the form Start is on and reads **Start empty** ("Create project and start empty" with the New project step open). Uploads that wait or failed still hold Start. Only a press of the button starts an empty thread: Enter in an empty form does nothing, so a stray key makes no thread. After Start the new thread opens, as before, with its composer ready for the first message.

(4) **Not in this change.** An agent's `thread_new` still carries its brief.
