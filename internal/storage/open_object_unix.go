//go:build !windows

package storage

import "os"

func openObjectFile(path string) (*os.File, error) {
	return os.Open(path)
}

func replaceObjectFile(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}
