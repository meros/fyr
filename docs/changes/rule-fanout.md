# V0.rule-fanout: the firing limit counts one Rule, so an Input that moves many changes does not read as a loop and leave a built-in rule unfired

→ SPEC sections 3, 5, 6 and 7 (7.3, I9); the `thread-rule-overdue` and `thread-rule-loop` invariants (V0.thread-invariants).

Evidence: `thread-rule-overdue` fired for a `builtins:<change>` rule right after a `deploy.self` input whose transition carried `rule.loop`.

(1) **Cause.** The scheduler stopped after 8 firings per Input in total. `deploy.self` (and the first clock pass of a loaded thread) moves every landed change, and each change with a PR has its own `changeMoved` rule, so a thread with more than 8 such changes fired 8 rules, reported `rule.loop`, and left the rest true with `LastValue=false` until some later input.

(2) **Fix.** The limit is 8 firings of one Rule per Input. A fan-out over many changes fires each rule once; a loop between Rules still ends, as each of them reaches the limit.

Tests: `TestRuleFanoutIsNotALoop`, `TestRuleLoopIsBounded` (`internal/core/rules_test.go`).
