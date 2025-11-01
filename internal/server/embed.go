package server

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/maxiofs/maxiofs/web"
	"github.com/sirupsen/logrus"
)

// getFrontendFS returns the embedded filesystem with the correct root
func getFrontendFS() (fs.FS, error) {
	return web.GetFrontendFS()
}

// rewriteAbsoluteURLs rewrites all absolute URLs in HTML to include the base path
func rewriteAbsoluteURLs(htmlBytes []byte, basePath string) []byte {
	// Patterns to match: href="/" src="/" srcset="/" content="/"
	patterns := []string{
		`href="/`,
		`src="/`,
		`srcset="/`,
		`content="/`,
	}

	result := htmlBytes
	for _, pattern := range patterns {
		// Skip /api/ URLs - those should not be rewritten
		// Replace pattern=/ with pattern=/basePath/ for all other cases
		parts := bytes.Split(result, []byte(pattern))
		if len(parts) <= 1 {
			continue
		}

		var newResult []byte
		for i, part := range parts {
			if i > 0 {
				// Check if this is an /api/ URL - don't rewrite those
				if bytes.HasPrefix(part, []byte("api/")) {
					newResult = append(newResult, []byte(pattern)...)
				} else {
					// Rewrite with base path
					newResult = append(newResult, []byte(pattern[:len(pattern)-1]+basePath+"/")...)
				}
			}
			newResult = append(newResult, part...)
		}
		result = newResult
	}

	return result
}

// spaHandler implements the http.Handler interface for serving a SPA
type spaHandler struct {
	staticFS   http.FileSystem
	indexBytes []byte
	basePath   string // Base path for the frontend (e.g., "/ui" or "/")
}

// ServeHTTP serves the SPA with fallback to index.html for client-side routing
func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path

	// If we have a base path (like /ui), check if the request starts with it
	if h.basePath != "/" && h.basePath != "" {
		basePathWithoutSlash := strings.TrimSuffix(h.basePath, "/")

		// Request must start with base path
		if !strings.HasPrefix(urlPath, basePathWithoutSlash) {
			// Not under our base path, 404
			http.NotFound(w, r)
			return
		}

		// Strip the base path to get the file path
		// /ui/assets/file.js -> /assets/file.js
		urlPath = strings.TrimPrefix(urlPath, basePathWithoutSlash)
	}

	// Clean and remove leading slash
	urlPath = path.Clean(urlPath)
	filePath := strings.TrimPrefix(urlPath, "/")
	if filePath == "" {
		filePath = "index.html"
	}

	// Try to open file from embedded filesystem
	file, err := h.staticFS.Open(filePath)
	if err == nil {
		defer file.Close()
		stat, _ := file.Stat()

		if filePath == "index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(h.indexBytes)
			return
		}

		if !stat.IsDir() {
			http.ServeContent(w, r, filePath, stat.ModTime(), file.(io.ReadSeeker))
			return
		}
	}

	// If it's a static asset and not found, return 404
	if strings.HasPrefix(filePath, "assets/") || strings.HasSuffix(filePath, ".js") ||
	   strings.HasSuffix(filePath, ".css") || strings.HasSuffix(filePath, ".png") {
		http.NotFound(w, r)
		return
	}

	// For SPA routes, serve index.html
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(h.indexBytes)
}

// newSPAHandler creates a new SPA handler with dynamic base path
func newSPAHandler(staticFS http.FileSystem, basePath string) (*spaHandler, error) {
	// Read index.html into memory for fast serving
	indexFile, err := staticFS.Open("index.html")
	if err != nil {
		return nil, err
	}
	defer indexFile.Close()

	indexStat, err := indexFile.Stat()
	if err != nil {
		return nil, err
	}

	indexBytes := make([]byte, indexStat.Size())
	_, err = io.ReadFull(indexFile, indexBytes)
	if err != nil {
		return nil, err
	}

	// Rewrite ALL absolute asset paths to include the base path
	// Vite generates paths like src="/assets/file.js"
	// We need to convert them to src="/ui/assets/file.js" when basePath is "/ui/"
	if basePath != "/" {
		basePathWithoutSlash := strings.TrimSuffix(basePath, "/")

		// Replace all absolute URLs that start with / (except /api which is handled separately)
		// This covers /assets/, /static/, /img/, /fonts/, etc.
		indexBytes = rewriteAbsoluteURLs(indexBytes, basePathWithoutSlash)
	}

	// Inject base tag and window.BASE_PATH variable
	// BASE_PATH is used by React Router to handle routes correctly under a subpath
	baseTag := []byte(`<base href="` + basePath + `">`)
	scriptTag := []byte(`<script>
		// Set base path for React Router and other frontend code
		window.__BASE_PATH__ = "` + basePath + `";
		window.BASE_PATH = "` + basePath + `";
		// Also set as a constant for imports
		if (typeof globalThis !== 'undefined') {
			globalThis.BASE_PATH = "` + basePath + `";
		}
	</script>`)

	// Find the <head> tag and inject after it
	headEnd := bytes.Index(indexBytes, []byte("<head>"))
	if headEnd != -1 {
		headEnd += len("<head>")
		// Insert base tag and script right after <head>
		modifiedIndex := make([]byte, 0, len(indexBytes)+len(baseTag)+len(scriptTag)+2)
		modifiedIndex = append(modifiedIndex, indexBytes[:headEnd]...)
		modifiedIndex = append(modifiedIndex, '\n')
		modifiedIndex = append(modifiedIndex, baseTag...)
		modifiedIndex = append(modifiedIndex, '\n')
		modifiedIndex = append(modifiedIndex, scriptTag...)
		modifiedIndex = append(modifiedIndex, indexBytes[headEnd:]...)
		indexBytes = modifiedIndex
	}

	return &spaHandler{
		staticFS:   staticFS,
		indexBytes: indexBytes,
		basePath:   basePath,
	}, nil
}

// extractBasePath extracts the path component from a URL with trailing slash
// Example: "https://s3.accst.local/ui" -> "/ui/"
// Example: "http://localhost:8081" -> "/"
func extractBasePath(urlStr string) string {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		logrus.WithError(err).Warn("Failed to parse public console URL, using / as base path")
		return "/"
	}

	basePath := parsedURL.Path
	if basePath == "" || basePath == "/" {
		return "/"
	}

	// Ensure base path starts with / and ends with /
	// The trailing slash is needed for the <base> tag in HTML
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	if !strings.HasSuffix(basePath, "/") {
		basePath = basePath + "/"
	}

	return basePath
}

// setupEmbeddedFrontend sets up the embedded frontend handler
func (s *Server) setupEmbeddedFrontend(router http.Handler) (http.Handler, error) {
	frontendFS, err := getFrontendFS()
	if err != nil {
		logrus.WithError(err).Warn("Failed to load embedded frontend")
		return nil, err // Return error instead of router
	}

	// Extract base path from public console URL (DYNAMIC - from config.yaml)
	basePath := extractBasePath(s.config.PublicConsoleURL)
	logrus.WithFields(logrus.Fields{
		"public_console_url": s.config.PublicConsoleURL,
		"base_path":          basePath,
	}).Info("Setting up embedded frontend with dynamic base path from public_console_url")

	// List files in embedded filesystem to verify they exist
	httpFS := http.FS(frontendFS)
	assetsDir, err := httpFS.Open("assets")
	if err == nil {
		defer assetsDir.Close()
		files, _ := assetsDir.(fs.ReadDirFile).ReadDir(10)
		var fileNames []string
		for _, f := range files {
			fileNames = append(fileNames, f.Name())
		}
		logrus.WithField("sample_assets", fileNames).Info("Embedded filesystem contains assets")
	} else {
		logrus.WithError(err).Error("Cannot open assets directory in embedded filesystem")
	}

	// Create SPA handler with dynamic base path
	spaHandler, err := newSPAHandler(httpFS, basePath)
	if err != nil {
		logrus.WithError(err).Warn("Failed to create SPA handler")
		return nil, err // Return error instead of router
	}

	logrus.Info("Embedded web console enabled")
	return spaHandler, nil
}
