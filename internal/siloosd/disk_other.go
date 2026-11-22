//go:build !linux

package siloosd

type diskStat struct {
	id          string
	total, free int64
}

func diskOf(string) (diskStat, bool) { return diskStat{}, false }
