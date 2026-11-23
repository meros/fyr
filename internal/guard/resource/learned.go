package resource

import "slices"

// Learned is one class's raw measures.
type Learned struct {
	Demand float64   `json:"demand"`
	Peak   uint64    `json:"peak"`
	Walls  []float64 `json:"walls"`
	Runs   int       `json:"runs"`
}

// WallP90 is the 90th percentile of the last wall times, 0 without any.
func (l Learned) WallP90() float64 {
	if len(l.Walls) == 0 {
		return 0
	}
	w := slices.Clone(l.Walls)
	slices.Sort(w)
	return w[min(len(w)-1, int(0.9*float64(len(w))))]
}
