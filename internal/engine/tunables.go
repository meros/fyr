package engine

// The engine's own tunables. How long an admission may sit idle and the
// memory guard's clocks are internal/guard's (tunables.go there; V0.guard-finish).

// guardKeep is how many decisions the front's history holds.
const guardKeep = 60
