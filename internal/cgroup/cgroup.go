// Package cgroup: SPEC 0.5 V0.80 (stub for the red test).
package cgroup

import "time"

type Budget struct {
	Enable                                                 bool
	ServerMemoryLow, AgentsCPUMax                          string
	AgentsMemoryHigh, AgentsMemoryMax, AgentsMemorySwapMax string
}

type Options struct {
	Budget
	FS, Self string
	PID      int
	Logf     func(string, ...any)
	Probe    func(dir string) error
	move     func(file, pid string) error
}

type Tree struct{ root string }

func Setup(o Options) *Tree             { return nil }
func (t *Tree) Root() string            { return "" }
func (t *Tree) Thread(id string) string { return "" }
func (t *Tree) Release(id string)       {}
func (t *Tree) Deploy() string          { return "" }

type Use struct {
	CPU               *float64
	CPUMax            float64
	Memory, MemoryMax uint64
}
type ThreadLoad struct {
	ID     string
	CPU    *float64
	Memory uint64
}
type Load struct {
	Cores       int
	Load        [3]float64
	CPUPressure *float64
	Cgroups     bool
	Agents      *Use
	Threads     []ThreadLoad
}
type Sampler struct {
	Tree  *Tree
	Proc  string
	Cores int
	Now   func() time.Time
}

func (s *Sampler) Get() Load { return Load{} }
