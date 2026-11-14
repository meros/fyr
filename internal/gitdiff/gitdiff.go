// Package gitdiff computes a thread's branch diff against its base with
// git (SPEC 0.5 V0.25). Read-only; git runs without a shell and with a
// timeout.
package gitdiff

import (
	"context"
	"errors"
)

var (
	maxFiles = 1000
	maxPatch = 400 << 10
)

// File is one changed file.
type File struct {
	Path      string `json:"path"`
	OldPath   string `json:"oldPath,omitempty"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary"`
}

// Changes is the diff summary of a folder.
type Changes struct {
	Git       bool   `json:"git"`
	Root      string `json:"root,omitempty"`
	Branch    string `json:"branch"`
	Base      string `json:"base"`
	MergeBase string `json:"mergeBase"`
	Commits   int    `json:"commits"`
	Files     []File `json:"files"`
	Truncated bool   `json:"truncated"`
}

// Patch is one file's unified diff.
type Patch struct {
	Path      string `json:"path"`
	Patch     string `json:"patch"`
	Binary    bool   `json:"binary"`
	Truncated bool   `json:"truncated"`
}

func Summary(ctx context.Context, dir string) (Changes, error) {
	return Changes{}, errors.New("not implemented")
}

func FilePatch(ctx context.Context, dir, path string) (Patch, error) {
	return Patch{}, errors.New("not implemented")
}
