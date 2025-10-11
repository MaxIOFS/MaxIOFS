package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/maxiofs/maxiofs/web"
	"github.com/sirupsen/logrus"
)

// getFrontendFS returns the embedded filesystem with the correct root
func getFrontendFS() (fs.FS, error) {
	return web.GetFrontendFS()
}

// spaHandler implements the http.Handler interface for serving a SPA
type spaHandler struct {
	staticFS   http.FileSystem
	indexBytes []byte
}

// ServeHTTP serves the SPA with fallback to index.html for client-side routing
func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Clean the path
	urlPath := r.URL.Path
	if urlPath == "" {
		urlPath = "/"
	}

	// Try to serve the exact file first
	filePath := strings.TrimPrefix(path.Clean(urlPath), "/")

	// Special handling for root
	if filePath == "" || filePath == "." {
		filePath = "index.html"
	}

	// Check if file exists
	file, err := h.staticFS.Open(filePath)
	if err == nil {
		defer file.Close()

		// Get file info
		stat, err := file.Stat()
		if err == nil && !stat.IsDir() {
			// File exists and is not a directory, serve it
			http.FileServer(h.staticFS).ServeHTTP(w, r)
			return
		}
	}

	// Check if it's an API request (should not reach here, but safety check)
	if strings.HasPrefix(urlPath, "/api/") {
		http.NotFound(w, r)
		return
	}

	// Check if it's a static asset request
	if strings.HasPrefix(urlPath, "/_next/") ||
		strings.HasPrefix(urlPath, "/static/") ||
		strings.HasSuffix(urlPath, ".js") ||
		strings.HasSuffix(urlPath, ".css") ||
		strings.HasSuffix(urlPath, ".png") ||
		strings.HasSuffix(urlPath, ".jpg") ||
		strings.HasSuffix(urlPath, ".jpeg") ||
		strings.HasSuffix(urlPath, ".gif") ||
		strings.HasSuffix(urlPath, ".svg") ||
		strings.HasSuffix(urlPath, ".ico") ||
		strings.HasSuffix(urlPath, ".woff") ||
		strings.HasSuffix(urlPath, ".woff2") ||
		strings.HasSuffix(urlPath, ".ttf") {
		// Static asset not found
		http.NotFound(w, r)
		return
	}

	// For all other routes (SPA routes), serve index.html
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(h.indexBytes)
}

// newSPAHandler creates a new SPA handler
func newSPAHandler(staticFS http.FileSystem) (*spaHandler, error) {
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
	_, err = indexFile.Read(indexBytes)
	if err != nil {
		return nil, err
	}

	return &spaHandler{
		staticFS:   staticFS,
		indexBytes: indexBytes,
	}, nil
}

// setupEmbeddedFrontend sets up the embedded frontend handler
func (s *Server) setupEmbeddedFrontend(router http.Handler) (http.Handler, error) {
	frontendFS, err := getFrontendFS()
	if err != nil {
		logrus.WithError(err).Warn("Failed to load embedded frontend")
		return nil, err // Return error instead of router
	}

	// Create SPA handler
	spaHandler, err := newSPAHandler(http.FS(frontendFS))
	if err != nil {
		logrus.WithError(err).Warn("Failed to create SPA handler")
		return nil, err // Return error instead of router
	}

	logrus.Info("Embedded web console enabled")
	return spaHandler, nil
}
