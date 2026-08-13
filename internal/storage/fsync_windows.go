//go:build windows

package storage

// syncDir is a no-op on Windows.
func syncDir(string) error {
	return nil
}
