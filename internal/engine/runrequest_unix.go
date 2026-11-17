//go:build unix

package engine

import (
	"os"
	"syscall"
)

func ownedBy(st os.FileInfo, uid int) bool {
	s, ok := st.Sys().(*syscall.Stat_t)
	return ok && int(s.Uid) == uid
}
