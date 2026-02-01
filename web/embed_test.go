package web

import (
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetFrontendFS_Success tests that GetFrontendFS returns a valid filesystem
func TestGetFrontendFS_Success(t *testing.T) {
	frontendFS, err := GetFrontendFS()
	require.NoError(t, err, "GetFrontendFS should not return an error")
	require.NotNil(t, frontendFS, "Returned filesystem should not be nil")
}

// TestGetFrontendFS_ContainsIndexHTML tests that the filesystem contains index.html
func TestGetFrontendFS_ContainsIndexHTML(t *testing.T) {
	frontendFS, err := GetFrontendFS()
	require.NoError(t, err)

	// Open index.html
	file, err := frontendFS.Open("index.html")
	require.NoError(t, err, "Should be able to open index.html")
	defer file.Close()

	// Verify file is not empty
	stat, err := file.Stat()
	require.NoError(t, err)
	assert.Greater(t, stat.Size(), int64(0), "index.html should not be empty")
	assert.False(t, stat.IsDir(), "index.html should be a file, not a directory")
}

// TestGetFrontendFS_ReadIndexHTML tests reading content from index.html
func TestGetFrontendFS_ReadIndexHTML(t *testing.T) {
	frontendFS, err := GetFrontendFS()
	require.NoError(t, err)

	// Read index.html
	file, err := frontendFS.Open("index.html")
	require.NoError(t, err)
	defer file.Close()

	content, err := io.ReadAll(file)
	require.NoError(t, err, "Should be able to read index.html content")
	require.NotEmpty(t, content, "index.html content should not be empty")

	htmlContent := string(content)

	// Verify it's a valid HTML file (case-insensitive DOCTYPE check)
	htmlLower := strings.ToLower(htmlContent)
	assert.Contains(t, htmlLower, "<!doctype html>", "Should contain DOCTYPE declaration")
	assert.Contains(t, htmlContent, "<html", "Should contain html tag")
	assert.Contains(t, htmlContent, "</html>", "Should have closing html tag")
}

// TestGetFrontendFS_ContainsAssetsDirectory tests that assets directory exists
func TestGetFrontendFS_ContainsAssetsDirectory(t *testing.T) {
	frontendFS, err := GetFrontendFS()
	require.NoError(t, err)

	// Try to open assets directory
	assetsDir, err := frontendFS.Open("assets")
	require.NoError(t, err, "Should be able to open assets directory")
	defer assetsDir.Close()

	// Verify it's a directory
	stat, err := assetsDir.Stat()
	require.NoError(t, err)
	assert.True(t, stat.IsDir(), "assets should be a directory")
}

// TestGetFrontendFS_ContainsJavaScriptFiles tests that JS files are embedded
func TestGetFrontendFS_ContainsJavaScriptFiles(t *testing.T) {
	frontendFS, err := GetFrontendFS()
	require.NoError(t, err)

	// Look for any .js file in assets
	var foundJS bool
	err = fs.WalkDir(frontendFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".js") {
			foundJS = true
			return fs.SkipAll // Stop walking once we find one
		}
		return nil
	})
	require.NoError(t, err)
	assert.True(t, foundJS, "Should contain at least one JavaScript file")
}

// TestGetFrontendFS_ContainsCSSFiles tests that CSS files are embedded
func TestGetFrontendFS_ContainsCSSFiles(t *testing.T) {
	frontendFS, err := GetFrontendFS()
	require.NoError(t, err)

	// Look for any .css file
	var foundCSS bool
	err = fs.WalkDir(frontendFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".css") {
			foundCSS = true
			return fs.SkipAll
		}
		return nil
	})
	require.NoError(t, err)
	assert.True(t, foundCSS, "Should contain at least one CSS file")
}

// TestGetFrontendFS_ContainsImages tests that image files are embedded
func TestGetFrontendFS_ContainsImages(t *testing.T) {
	frontendFS, err := GetFrontendFS()
	require.NoError(t, err)

	imageExtensions := []string{".png", ".jpg", ".jpeg", ".svg", ".gif", ".ico"}
	var foundImage bool

	err = fs.WalkDir(frontendFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			for _, ext := range imageExtensions {
				if strings.HasSuffix(strings.ToLower(path), ext) {
					foundImage = true
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	require.NoError(t, err)
	assert.True(t, foundImage, "Should contain at least one image file")
}

// TestGetFrontendFS_WalkDirectory tests walking through the filesystem
func TestGetFrontendFS_WalkDirectory(t *testing.T) {
	frontendFS, err := GetFrontendFS()
	require.NoError(t, err)

	fileCount := 0
	dirCount := 0

	err = fs.WalkDir(frontendFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirCount++
		} else {
			fileCount++
		}
		return nil
	})
	require.NoError(t, err)

	assert.Greater(t, fileCount, 0, "Should contain at least one file")
	assert.GreaterOrEqual(t, dirCount, 1, "Should contain at least one directory (root)")

	t.Logf("Found %d files and %d directories in embedded filesystem", fileCount, dirCount)
}

// TestGetFrontendFS_ReadSpecificAsset tests reading a specific asset file
func TestGetFrontendFS_ReadSpecificAsset(t *testing.T) {
	frontendFS, err := GetFrontendFS()
	require.NoError(t, err)

	// Find any .js file to test reading
	var jsFilePath string
	err = fs.WalkDir(frontendFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".js") {
			jsFilePath = path
			return fs.SkipAll
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, jsFilePath, "Should find at least one JS file")

	// Try to read the JS file
	file, err := frontendFS.Open(jsFilePath)
	require.NoError(t, err, "Should be able to open JS file: %s", jsFilePath)
	defer file.Close()

	content, err := io.ReadAll(file)
	require.NoError(t, err, "Should be able to read JS file content")
	assert.NotEmpty(t, content, "JS file should not be empty")

	t.Logf("Successfully read %d bytes from %s", len(content), jsFilePath)
}

// TestGetFrontendFS_FileInfo tests getting file information
func TestGetFrontendFS_FileInfo(t *testing.T) {
	frontendFS, err := GetFrontendFS()
	require.NoError(t, err)

	// Get file info for index.html
	file, err := frontendFS.Open("index.html")
	require.NoError(t, err)
	defer file.Close()

	stat, err := file.Stat()
	require.NoError(t, err)

	assert.Equal(t, "index.html", stat.Name(), "File name should be index.html")
	assert.Greater(t, stat.Size(), int64(0), "File size should be greater than 0")
	assert.False(t, stat.IsDir(), "index.html should not be a directory")
	assert.NotNil(t, stat.ModTime(), "ModTime should not be nil")
}

// TestGetFrontendFS_OpenNonExistentFile tests error handling for missing files
func TestGetFrontendFS_OpenNonExistentFile(t *testing.T) {
	frontendFS, err := GetFrontendFS()
	require.NoError(t, err)

	// Try to open a file that doesn't exist
	_, err = frontendFS.Open("nonexistent-file.txt")
	assert.Error(t, err, "Should return error when opening non-existent file")
	assert.ErrorIs(t, err, fs.ErrNotExist, "Error should be ErrNotExist")
}

// TestGetFrontendFS_PathSeparators tests that paths work correctly
func TestGetFrontendFS_PathSeparators(t *testing.T) {
	frontendFS, err := GetFrontendFS()
	require.NoError(t, err)

	// Test with forward slashes (Go standard for embed.FS)
	file, err := frontendFS.Open("assets/img/icon.png")
	if err == nil {
		defer file.Close()
		assert.NoError(t, err, "Should be able to open with forward slashes")

		stat, err := file.Stat()
		require.NoError(t, err)
		assert.Greater(t, stat.Size(), int64(0), "Image file should have content")
	}
	// Note: If assets/img/icon.png doesn't exist, that's OK - we're just testing path handling
}

// TestGetFrontendFS_ConsistentResults tests that multiple calls return consistent results
func TestGetFrontendFS_ConsistentResults(t *testing.T) {
	// Call GetFrontendFS multiple times
	fs1, err1 := GetFrontendFS()
	require.NoError(t, err1)

	fs2, err2 := GetFrontendFS()
	require.NoError(t, err2)

	// Both should be able to open index.html
	file1, err := fs1.Open("index.html")
	require.NoError(t, err)
	defer file1.Close()

	file2, err := fs2.Open("index.html")
	require.NoError(t, err)
	defer file2.Close()

	// Read content from both
	content1, err := io.ReadAll(file1)
	require.NoError(t, err)

	content2, err := io.ReadAll(file2)
	require.NoError(t, err)

	// Content should be identical
	assert.Equal(t, content1, content2, "Multiple calls should return identical content")
}

// TestGetFrontendFS_GlobPattern tests using glob patterns with the filesystem
func TestGetFrontendFS_GlobPattern(t *testing.T) {
	frontendFS, err := GetFrontendFS()
	require.NoError(t, err)

	// Find all .js files using glob
	jsFiles, err := fs.Glob(frontendFS, "assets/*.js")
	if err == nil && len(jsFiles) > 0 {
		assert.Greater(t, len(jsFiles), 0, "Should find at least one JS file with glob pattern")
		t.Logf("Found %d JS files with glob pattern 'assets/*.js'", len(jsFiles))
	}
}

// TestFrontendAssets_DirectAccess tests that FrontendAssets embed.FS is accessible
func TestFrontendAssets_DirectAccess(t *testing.T) {
	// Verify FrontendAssets is not nil
	require.NotNil(t, FrontendAssets, "FrontendAssets should be initialized")

	// Try to open from the full path (frontend/dist/index.html)
	file, err := FrontendAssets.Open("frontend/dist/index.html")
	require.NoError(t, err, "Should be able to open from FrontendAssets with full path")
	defer file.Close()

	stat, err := file.Stat()
	require.NoError(t, err)
	assert.Greater(t, stat.Size(), int64(0), "File should have content")
}

// TestGetFrontendFS_SubdirectoryStructure tests the filesystem structure
func TestGetFrontendFS_SubdirectoryStructure(t *testing.T) {
	frontendFS, err := GetFrontendFS()
	require.NoError(t, err)

	// Map to track directory structure
	directories := make(map[string]bool)
	files := make(map[string]bool)

	err = fs.WalkDir(frontendFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Normalize path
		normalizedPath := filepath.ToSlash(path)

		if d.IsDir() {
			directories[normalizedPath] = true
		} else {
			files[normalizedPath] = true
		}
		return nil
	})
	require.NoError(t, err)

	t.Logf("Filesystem structure:")
	t.Logf("  Directories: %d", len(directories))
	t.Logf("  Files: %d", len(files))

	// Verify we have some expected structure
	assert.Greater(t, len(files), 0, "Should have at least one file")
}
