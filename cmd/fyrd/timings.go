package main

import "time"

// fyrd's own clocks, named once. They are not config: no owner has needed to
// move one, and a config key is a promise to keep it.
const (
	loginShellWait     = 5 * time.Second  // how long the login shell may take to print PATH
	shutdownWait       = 5 * time.Second  // listeners get this long to finish in-flight requests
	runnerListWait     = 10 * time.Second // V0.90 (3): how long to wait for the runner's list at start
	quotaHTTPTimeout   = 10 * time.Second // one provider quota read
	releaseHTTPTimeout = 15 * time.Minute // a release download
	setupExistingAfter = time.Minute      // V0.116: projects from before get a setup run this long after start
	setupExistingEvery = 30 * time.Second // ... and this far apart
	setupHostAfter     = time.Minute      // V0.208: this host's mark gets one setup run this long after start
	engineTick         = 10 * time.Second // the engine's tick, and the guard's fallback to its PSI trigger
	focusExpireEvery   = 5 * time.Second  // V0.85: a focus whose beats stopped drops back
)
