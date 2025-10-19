package storage

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

// ensureFolderMarkersInPath creates .maxiofs-folder markers in all subdirectories
// of the given path that are within the dataDir. This is called when files are
// uploaded to nested paths, ensuring that intermediate directories are properly
// marked as folders (even if they weren't explicitly created with a trailing /)
func (fs *FilesystemBackend) ensureFolderMarkersInPath(dir string) {
	// Only process directories within our rootPath
	if !strings.HasPrefix(dir, fs.rootPath) {
		return
	}

	// Walk up the directory tree from dir to rootPath
	current := dir
	for {
		// Stop when we reach rootPath or root
		if current == fs.rootPath || current == filepath.Dir(current) {
			break
		}

		// Check if this directory already has a .maxiofs-folder marker
		markerPath := filepath.Join(current, ".maxiofs-folder")
		if _, err := os.Stat(markerPath); os.IsNotExist(err) {
			// Create the marker file
			if markerFile, err := os.Create(markerPath); err == nil {
				markerFile.Close()
				logrus.WithField("path", current).Debug("Created .maxiofs-folder marker for implicit directory")
			}
		}

		// Move up one directory
		current = filepath.Dir(current)
	}
}
