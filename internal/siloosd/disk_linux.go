//go:build linux

package siloosd

import (
	"fmt"
	"syscall"
)

type diskStat struct {
	id          string
	total, free int64
}

// diskOf is the size and free space of the filesystem holding path, and an
// id that is the same for two paths on one filesystem.
func diskOf(path string) (diskStat, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return diskStat{}, false
	}
	bs := int64(st.Bsize)
	return diskStat{id: fmt.Sprint(st.Fsid), total: int64(st.Blocks) * bs, free: int64(st.Bavail) * bs}, true
}
