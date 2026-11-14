# 0001 Spec in the repo, plan and status in GitHub issues

Date: 2026-11-14 · Status: accepted

**Context.** The design phase produced a spec, a v1 plan and review logs as files. Building needs tracked work, recorded decisions and a way to prove each behaviour.

**Options.** (a) Everything in markdown, status by hand. (b) Everything in GitHub (issues, wiki). (c) Spec and decisions in the repo, plan and status in GitHub issues and milestones.

**Decision.** (c). The spec and decisions change through PRs, so they are versioned with the code that implements them. Status comes from issue state, which no one has to update by hand. Every behaviour change goes spec, then a red test that names the spec ID, then code (`docs/PROCESS.md`).

**Consequences.** `docs/PLAN.md` holds only milestone definitions. Tests carry spec IDs. Issue forms ask for the spec section.
