# 0003 Tests drive a scripted fake claude over the real protocol

Date: 2026-11-14 · Status: accepted · Spec: 16.2

**Context.** Session handling (questions, interrupt, crashes, resume) is where most bugs will be. Tests against the real CLI are slow, cost money and are not deterministic.

**Decision.** `internal/claude/fake` implements the control protocol with scripted behaviours chosen by the first word of the prompt (`ask`, `approve`, `fail`, `hang`, `pr`, `crash`, `sub`). A test binary re-executes itself as the fake when `FYR_FAKE_CLAUDE=1`. Tests then cover the real client, the process boundary and the session manager.

**Consequences.** The fake must follow the real CLI. The conformance recording planned in FYR-V1 7 (record real traffic once per pinned CLI version, replay in CI) closes that gap; until then, `live`-tagged tests run against the real CLI by hand.
