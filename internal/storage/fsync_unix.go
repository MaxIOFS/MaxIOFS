//go:build !windows

package storage

import "os"

// syncDir makes a rename in dir durable.
//
// A rename is a directory update, and on a crash the kernel may have the file's
// contents on the platter while the directory entry that names it is still only
// in the page cache — or the reverse. Syncing the file makes its bytes durable;
// syncing the directory is what makes the name that reaches them durable too,
// and the two-phase sidecar commit depends on both, since it reasons about
// which of two renames survived.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
