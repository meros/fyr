package slots

import "github.com/meros/fyr/internal/guard/resource"

const gb = 1 << 30

// A host that gives its agents 6 cores and 13 GB, with 60 % of that memory really free.
var host6 = resource.Signals{Provider: "cgroups", Slowdown: f(1.1), Headroom: f(0.6), Stall: resource.StallOK, CPU: 6, Memory: 13 * gb}
