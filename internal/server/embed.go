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

// extractBasePathFromURL extracts the path component from a URL.
// Example: "https://s3.accst.local/ui" -> "/ui"
// Example: "http://localhost:8081" -> "/"
// Example: "" -> "/"
func extractBasePathFromURL(urlStr string) string {
	if urlStr == "" {
		return "/"
	}
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return "/"
	}
	basePath := parsed.Path
	if basePath == "" || basePath == "/" {
		return "/"
	}
	basePath = strings.TrimSuffix(basePath, "/")
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	return basePath
}

// getFrontendFS returns the embedded filesystem with the correct root
func getFrontendFS() (fs.FS, error) {
	return web.GetFrontendFS()
}

// extractBasePath extracts the path component from a URL with trailing slash.
// The trailing slash is needed for the <base> tag in HTML.
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

	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	if !strings.HasSuffix(basePath, "/") {
		basePath = basePath + "/"
	}

	return basePath
}

// rewriteAbsoluteURLs rewrites all absolute URLs in HTML to include the base path.
// Vite generates src="/assets/file.js" — when served under /ui/ these must become
// src="/ui/assets/file.js". API URLs (/api/) are left untouched.
func rewriteAbsoluteURLs(htmlBytes []byte, basePath string) []byte {
	patterns := []string{
		`href="/`,
		`src="/`,
		`srcset="/`,
		`content="/`,
	}

	result := htmlBytes
	for _, pattern := range patterns {
		parts := bytes.Split(result, []byte(pattern))
		if len(parts) <= 1 {
			continue
		}

		var newResult []byte
		for i, part := range parts {
			if i > 0 {
				if bytes.HasPrefix(part, []byte("api/")) {
					newResult = append(newResult, []byte(pattern)...)
				} else {
					newResult = append(newResult, []byte(pattern[:len(pattern)-1]+basePath+"/")...)
				}
			}
			newResult = append(newResult, part...)
		}
		result = newResult
	}

	return result
}

// spaHandler serves the embedded SPA with subpath support.
type spaHandler struct {
	staticFS   http.FileSystem
	indexBytes []byte // pre-processed index.html with base path injected
	basePath   string // e.g. "/ui/" or "/"
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path

	// If we have a base path (like /ui), strip it to get the file path
	if h.basePath != "/" && h.basePath != "" {
		basePathWithoutSlash := strings.TrimSuffix(h.basePath, "/")
		if !strings.HasPrefix(urlPath, basePathWithoutSlash) {
			http.NotFound(w, r)
			return
		}
		urlPath = strings.TrimPrefix(urlPath, basePathWithoutSlash)
	}

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

	// If it's a static asset path and not found, return 404
	if strings.HasPrefix(filePath, "assets/") || strings.HasSuffix(filePath, ".js") ||
		strings.HasSuffix(filePath, ".css") || strings.HasSuffix(filePath, ".png") {
		http.NotFound(w, r)
		return
	}

	// For SPA routes, serve index.html
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(h.indexBytes)
}

// newSPAHandler creates a new SPA handler with the given base path.
// It reads index.html once, rewrites absolute asset URLs if needed,
// and injects the BASE_PATH script tag.
func newSPAHandler(staticFS http.FileSystem, basePath string) (*spaHandler, error) {
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
	if _, err = io.ReadFull(indexFile, indexBytes); err != nil {
		return nil, err
	}

	// Rewrite absolute asset paths in HTML to include the base path
	if basePath != "/" {
		basePathWithoutSlash := strings.TrimSuffix(basePath, "/")
		indexBytes = rewriteAbsoluteURLs(indexBytes, basePathWithoutSlash)
	}

	// Inject <base> tag and window.BASE_PATH
	baseTag := []byte(`<base href="` + basePath + `">`)
	scriptTag := []byte(`<script>
		window.__BASE_PATH__ = "` + basePath + `";
		window.BASE_PATH = "` + basePath + `";
		if (typeof globalThis !== 'undefined') {
			globalThis.BASE_PATH = "` + basePath + `";
		}
	</script>`)

	headEnd := bytes.Index(indexBytes, []byte("<head>"))
	if headEnd != -1 {
		headEnd += len("<head>")
		modified := make([]byte, 0, len(indexBytes)+len(baseTag)+len(scriptTag)+2)
		modified = append(modified, indexBytes[:headEnd]...)
		modified = append(modified, '\n')
		modified = append(modified, baseTag...)
		modified = append(modified, '\n')
		modified = append(modified, scriptTag...)
		modified = append(modified, indexBytes[headEnd:]...)
		indexBytes = modified
	}

	return &spaHandler{
		staticFS:   staticFS,
		indexBytes: indexBytes,
		basePath:   basePath,
	}, nil
}

// setupEmbeddedFrontend sets up the embedded frontend handler.
func (s *Server) setupEmbeddedFrontend() (http.Handler, error) {
	frontendFS, err := getFrontendFS()
	if err != nil {
		logrus.WithError(err).Warn("Failed to load embedded frontend")
		return nil, err
	}

	// extractBasePath returns with trailing slash (e.g. "/ui/") for <base href>
	basePath := extractBasePath(s.config.PublicConsoleURL)
	logrus.WithField("base_path", basePath).Info("Setting up embedded frontend")

	httpFS := http.FS(frontendFS)
	handler, err := newSPAHandler(httpFS, basePath)
	if err != nil {
		logrus.WithError(err).Warn("Failed to create SPA handler")
		return nil, err
	}

	logrus.Info("Embedded web console enabled")
	return handler, nil
}
