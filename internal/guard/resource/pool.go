package resource

import "time"

// The kinds of heavy job; they all take from the same pool.
const (
	Test  = "test"
	Build = "build"
	E2E   = "e2e"
)

// Kinds are the kinds an agent may name, in the order the tool lists them.
func Kinds() []string { return []string{Test, Build, E2E} }

// Holder is one owner (a thread id, or the self-deploy) holding or waiting.
type Holder struct {
	Owner    string    `json:"owner"`
	Kind     string    `json:"kind"`
	Since    time.Time `json:"since"`
	Priority bool      `json:"priority,omitempty"`
	// Instances (V0.freeze-controller): how many heavy commands the hold asks
	// to run at once, set by a grow when a thread starts another beside one
	// that runs; 0 is one. The need of the holders that run is counted from
	// the open calls (Admission.OpenCalls), not from this.
	Instances int `json:"instances,omitempty"`
	// V0.163, in a snapshot: the job's need (learned or live: cores and
	// bytes) as percentages of the host, CPU and memory, and Pct the larger
	// of the two; for a waiter, what it will need. Need is nil without
	// Pool.Cost. Cost is the old one-number form (Pct of N), kept one
	// release for older fronts.
	Need   *Need   `json:"need,omitempty"`
	CPUPct float64 `json:"cpuPct,omitempty"`
	MemPct float64 `json:"memPct,omitempty"`
	Pct    float64 `json:"pct,omitempty"`
	Cost   float64 `json:"cost,omitempty"`
}

// Undeclared is a thread that runs heavy work Fyr did not admit (V0.164): its
// use above its idle level, counted in the budget beside the admitted jobs.
// The host's detector makes the list (Pool.Undeclared); the pool adds the
// shares of the host.
type Undeclared struct {
	Thread  string    `json:"thread"`
	Name    string    `json:"name,omitempty"` // the thread's project, for the wait reason
	CPU     float64   `json:"cpu"`            // cores
	Mem     uint64    `json:"mem"`            // bytes
	CPUPct  float64   `json:"cpuPct"`
	MemPct  float64   `json:"memPct"`
	Since   time.Time `json:"since"`
	Command string    `json:"command,omitempty"`
	Paused  bool      `json:"paused"`
	Queued  bool      `json:"queued,omitempty"`
	// OverCPU and OverMem: the measure is at least a tenth of its
	// threshold, so the row names it.
	OverCPU bool `json:"overCpu"`
	OverMem bool `json:"overMem"`
	// MemOver: the memory itself is over its threshold (the freeze takes these).
	MemOver bool `json:"-"`
	Matched bool `json:"matched,omitempty"`
	// Kind and Window are for the agent's message, not the front.
	Kind   string        `json:"-"`
	Window time.Duration `json:"-"`
}

// Snapshot is the pool as GET /api/load shows it.
type Snapshot struct {
	N       int      `json:"n"`
	Held    []Holder `json:"held"`
	Waiting []Holder `json:"waiting"`
	// Decision 0029 S2: Used is the holders' costs summed (nil without
	// Pool.Cost); Why is why the head waits ("" when nothing holds it).
	Used *float64 `json:"used,omitempty"`
	Why  string   `json:"why,omitempty"`
	// Decision 0029 S4: the budget now (nil while it is N) and why.
	Budget    *float64 `json:"budget,omitempty"`
	BudgetWhy string   `json:"budgetWhy,omitempty"`
	// V0.163, in percent of what the host gives its agents, per resource:
	// the jobs in use, the budget (AIMD moves it), and what is free in it.
	// UsedPct is the tighter resource's use of its budget (the rail's bar)
	// and Tight names it; Binding names what holds the head back; NeedPct
	// is the head's larger need. Without measures (no Pool.Host) only
	// UsedPct, a share of the fixed count, is set.
	CPUPct       *float64 `json:"cpuPct,omitempty"`
	MemPct       *float64 `json:"memPct,omitempty"`
	CPUBudgetPct *float64 `json:"cpuBudgetPct,omitempty"`
	MemBudgetPct *float64 `json:"memBudgetPct,omitempty"`
	CPUFreePct   *float64 `json:"cpuFreePct,omitempty"`
	MemFreePct   *float64 `json:"memFreePct,omitempty"`
	UsedPct      *float64 `json:"usedPct,omitempty"`
	Tight        string   `json:"tight,omitempty"`
	Binding      string   `json:"binding,omitempty"`
	NeedPct      *float64 `json:"needPct,omitempty"`
	// V0.164: the threads that run heavy work Fyr did not admit, and their
	// share of the host; the used percentages above include them.
	Undeclared       []Undeclared `json:"undeclared,omitempty"`
	UndeclaredCPUPct *float64     `json:"undeclaredCpuPct,omitempty"`
	UndeclaredMemPct *float64     `json:"undeclaredMemPct,omitempty"`
	// Decision 0032: what the memory guard did lately.
	Guard *Guard `json:"guard,omitempty"`
	// Bound is the resource that holds the head back as the AIMD controller
	// reads it: Binding, with the headroom told apart where the host's real
	// headroom binds and not the memory budget (V0.admission-memory).
	Bound string `json:"-"`
}
