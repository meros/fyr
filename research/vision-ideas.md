# Visionary ideas (Fable 5.2, 2026-11-13)

Ideas asked for as inspiration, 3–5 years out, buildable on the Fyr foundation.

1. **The Decision, not the thread, is the inbox unit.** Seven agents asking the same thing is one row answered once. → Adopted (D1).
2. **Priced asks.** Each ask carries its cost to you, what it unblocks, a deadline and a default; the inbox sorts by unblocked work per minute of your time. → Adopted in the Decision type; UI in v1.1.
3. **Policy proposed from your own approval log**, shadow-run for a week before you accept it. → Data in place; v1.5.
4. **Counterfactual replay**: run a new Rule or policy against last month's events before trusting it. → Enabled by keeping events and per-run checkpoints.
5. **Speculative execution on the likely answer** in a fork while a question is pending. → v2.
6. **Evidence-carrying claims** in Briefs: each "done" points to the event that proves it, or says "unverified". → Adopted (Claim type).
7. **The goal document is the product; threads are exhaust.** Agents reconcile toward an editable goal with acceptance criteria.
8. **Chief of staff with zero authority**: a coordinator that proposes batches of actions and answers from precedent. → Coordinator design.
9. **Editions**: the inbox is published at times you choose ("nothing needs you until 14:00"). → v2; work/silent hours are the first step.
10. **The fleet as one computer**: a parked thread is a commit plus a Brief and wakes on whichever host has capacity and a warm Nix store. → Host-independent thread ids (D2); v2.
11. **Factories that file PRs against themselves**, mining review rounds and CI failures to improve skills and `CLAUDE.md`.

**Shape the foundation now:** Request as a logged Decision (D1); host-independent identity with replayable events (D2); one place that sees all hosts (became the mesh, D3).

**The trap:** a hierarchy of manager agents with delegated authority. Constraints vanish through layers of summaries, approvals stop proving human consent, and no decision is inspectable. Keep one flat layer: agents ask, a coordinator proposes, authority grows only through policy you accepted.
