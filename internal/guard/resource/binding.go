package resource

// Resources a wait can name.
const (
	ResCPU = "cpu"
	ResMem = "memory"
	// ResHeadroom: memory, held back by the host's real headroom (memory
	// outside the budget: the owner's browser, the running jobs' growth),
	// not by the memory budget. V0.admission-memory: the AIMD controller
	// grows the memory budget only for ResMem, since a larger budget does
	// not make the host's free memory any larger; the snapshot sends it as
	// ResMem, so the front reads the same two values.
	ResHeadroom = "headroom"
)

// Wire is binding as the snapshot sends it: ResHeadroom is memory.
func Wire(binding string) string {
	if binding == ResHeadroom {
		return ResMem
	}
	return binding
}
