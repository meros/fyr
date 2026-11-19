# V0.model-choice: a provider and its model are one readable choice; effort in Hand over; a project's default effort; the nearest match on Hand over

→ V0.23, V0.45, V0.50, V0.120, decisions 0015 (addendum 4) and 0017. *Amends V0.45: Hand over picks the model and effort, starting on the nearest match; amends V0.50: a provider that cannot run is shown in Hand over, not pickable.* (#18: "better model selectors per provider"; #23: the project default effort, a fallback that says so, the nearest model on Hand over.)

(1) **One choice.** New thread and Hand over show provider and model as one select, each choice "Claude · Opus 5.5": the provider's name, then the model's name (the name the provider reports, else the id made readable). The choices are grouped by provider, the group named with what is left on its account (V0.44). A provider with no model list is one choice, "Bare · its default model". A provider that cannot run is one choice, "Work · needs setup", shown and not pickable, with why as its title. A picked model the owner turned off stays in the list, marked "(off)" (V0.120 (5)). On the phone the same choices stand behind the New thread chip, which reads the same words (one component, `model-choice-ui.tsx`).

(2) **Effort.** Beside the choice, an Effort select, only when the picked model takes effort (V0.50). Hand over has it now too.

(3) **Hand over starts on the nearest match.** On the target provider: the thread's model when the target offers it, else the first model of the same family (Opus, Sonnet, Haiku, Fable) in the owner's order, else the target's default. The effort: the thread's when the new model takes it, else the nearest level (low, medium, high, xhigh, max; the lower on a tie), else the target's default. A line under the choice names the mapping: "Opus 5.5 · max → Sonnet 5.5 · high: the nearest Claude (acme) has.", "Keeps Opus 5.5 · high." when nothing changes, and without "the nearest" once the owner picks something else.

(4) **The host.** `thread.handover` takes an optional `effort`. Every rung starts the target with it (before, only resume passed the thread's effort), and `handover.done` sets the thread's effort to it; without one the thread keeps its effort, mapped as before (`cli.EffortFor`). The handover event's data names the effort.

(5) **A project's default effort.** The project screen's "Default provider, model and effort" gains an Effort select when the default model takes effort (`project.update {effort}`, stored as the project's `effort`; a new default provider clears it). A new thread in the project starts with it; the owner can change it per thread.

(6) **A default that cannot run says so.** When the project's default provider cannot run (or is not on the thread's host), New thread falls back as before (the current pick, the device's last, the first that can run) and says so under the choice: "Web's default, Work, cannot run (Needs a login); it starts on Claude (acme)." Picking a model clears the line.
