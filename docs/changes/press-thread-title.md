# V0.press-thread-title: a thread a press started is titled from the press's label and prompt

→ V0.auto-title. *Amends V0.auto-title (5): the press notice that starts a thread is read as its brief. Amends V0.auto-title (6): such a thread's placeholder is the press's label. Amends V0.auto-title (4): a goal title that fails for want of conversation stays due.*

Why: a thread started from a press ("Start …" chip with `in: "new"`) has one first message, Fyr's notice `[Fyr] The owner pressed "<label>" on <on>.` followed by the press's prompt. The title call skipped every `[Fyr]` message (V0.auto-title (5)), so it dropped the one message that held the thread's brief. When the agent opened its first change before writing any text, the goal title found no conversation, failed quietly, and the mark was already `done`, so no title was ever asked again. The thread kept the notice line, `[Fyr] The owner pressed "Start #… also (…`, as its title for its whole life.

(1) **The brief.** When a thread's first message is a press notice (`core.PressNotice`, read back by `core.PressBrief`), the title call reads it as the owner's words: the label and the prompt, without the frame. Every other `[Fyr]` message is still left out, a press on a running thread among them: it says what to do next, not what the thread is for.

(2) **Placeholder.** Such a thread is first named by the press's label (`core.PlaceholderTitle`), cut like any first line (V0.auto-title (6)).

(3) **Asked again.** A goal title that finds no conversation lands as `title.made {by: "fyr", auto: "goal", again: true}`. It sets the mark to `"again"`, and the end of the next run asks the goal title once more, then the mark is `done`. A failure for any other reason stays quiet and done, as before. The model is not called on an empty conversation.

Threads titled before this change keep their titles. The fronts show the title as before and need no change.

Tests: `TestPressThreadPlaceholderIsTheLabel`, `TestGoalTitleWithoutConversationIsAskedAgain` (core); `TestTitleTextReadsTheStartingPress` (engine).
