package object

import (
	"errors"
	"syscall"

	"github.com/shirou/gopsutil/v3/disk"
)

// MaxDiskUsedPercent is how full a disk gets: object writes stop there. What
// stays free keeps Pebble and SQLite working, deletes included, which is how
// space comes back.
const MaxDiskUsedPercent = 99.5

// WithDataDir names the directory of the metadata stores, whose disk may not be
// the one of the objects.
func WithDataDir(dir string) Option {
	return func(om *objectManager) { om.dataDir = dir }
}

// IsDiskFull reports a write refused for its disk, or that ran out of it.
func IsDiskFull(err error) bool {
	return errors.Is(err, ErrDiskFull) || errors.Is(err, syscall.ENOSPC)
}

// WritableBytes is what objects can still take of a disk with used and free
// bytes: its free space less what stays free.
func WritableBytes(used, free uint64) uint64 {
	reserve := uint64(float64(used+free) * (100 - MaxDiskUsedPercent) / 100)
	if free <= reserve {
		return 0
	}
	return free - reserve
}

func diskUsedPercent(path string) (float64, error) {
	usage, err := disk.Usage(path)
	if err != nil {
		return 0, err
	}
	return usage.UsedPercent, nil
}

// checkDiskSpace refuses a write when the disk of the objects or of the metadata
// stores reached MaxDiskUsedPercent. A disk that cannot be read refuses nothing.
func (om *objectManager) checkDiskSpace() error {
	usedPercent := om.diskUsed
	if usedPercent == nil {
		usedPercent = diskUsedPercent
	}
	for _, path := range []string{om.config.Root, om.dataDir} {
		if path == "" {
			continue
		}
		if used, err := usedPercent(path); err == nil && used >= MaxDiskUsedPercent {
			return ErrDiskFull
		}
	}
	return nil
}
