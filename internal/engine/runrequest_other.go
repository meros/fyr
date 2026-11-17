//go:build !unix

package engine

import "os"

func ownedBy(os.FileInfo, int) bool { return false }
