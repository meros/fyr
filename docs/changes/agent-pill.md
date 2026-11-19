# V0.agent-pill: provider, model and effort read as one compact pill, on every screen that shows or picks them

→ V0.model-choice, V0.50, V0.120 (5), V0.206. *Changes only the look of V0.model-choice's choice and Effort select on New thread; its choices, its effort in Hand over and a project's default effort stay as they are.* (owner, 19 Nov: "Redesign the agent pull to combine provider, model, effort in a compact but readable way".)

(1) **The pill.** One rounded control, parts parted by hairlines: the provider (its colour dot and name), the model (its name, mono), the effort (a meter of one bar per level the model takes, filled up to the current level, and the level's name; "default" with no bar filled while the CLI picks). A model that takes no effort has no effort part. One component draws it on both fronts (`web/src/agent-ui.tsx`).

(2) **Picking.** A part that can change lights on hover and holds its native picker laid over it: one click (one tap) away, the platform picker on the phone.
- **New thread** (both fronts): provider and model are one part over V0.model-choice's one choice (grouped by provider, with each one's quota; a provider that cannot run is shown, not pickable), and the effort part is its Effort select. The project's default effort and the note when the project's default cannot run are V0.model-choice's.
- **A thread**: the provider only shows (another provider, model and effort is Hand over, V0.model-choice). The model, on this provider, and the effort change in place with `thread.setModel` unless the thread cannot take it; then the reason is in the part's title. A model the owner turned off still reads "(off)" (V0.120 (5)).

(3) **Where.** The desktop thread header (in place of the provider, model and effort chips); the phone thread header (in place of the provider chip; it only shows there, because that chip row is one button that opens Details); the phone Details sheet (one Agent row in place of Model, Effort and Provider); both New thread screens (one Agent field in place of Model and Effort).

(4) **Sizes.** Desktop: a 24 px chip in the header, a 34 px field in the dialog. Phone: a 28 px chip in the header, a 48 px field in forms (a finger's size). The phone header is narrow: the model keeps its width, the provider shrinks to its dot and the effort to its meter; the names are in Details.
