//go:build !linux

package cgroup

// releaseMemory: process_mrelease is Linux only.
func releaseMemory([]int) func(bool) { return func(bool) {} }
