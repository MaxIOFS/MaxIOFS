//go:build windows

package storage

// syncDir is a no-op on Windows.
//
// A directory handle cannot be flushed there — FlushFileBuffers on one fails —
// and it is not the mechanism NTFS uses: metadata operations such as a rename
// are journalled, so the directory entry does not depend on a separate flush
// the way it does on a POSIX filesystem. Syncing the FILES still matters and is
// done on every platform.
func syncDir(string) error {
	return nil
}
