package server

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/maxiofs/maxiofs/web"
	"github.com/sirupsen/logrus"
)

// extractBasePathFromURL returns the path component of rawURL, with trailing
// slash stripped. E.g. "https://maxiofs.local/ui" → "/ui", "" → "".
func extractBasePathFromURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimRight(parsed.Path, "/")
}

// getFrontendFS returns the embedded filesystem with the correct root
func getFrontendFS() (fs.FS, error) {
	return web.GetFrontendFS()
}

// spaHandler serves the embedded SPA. Static assets are served directly;
// everything else falls back to index.html for client-side routing.
//
// The reverse proxy (nginx) is responsible for stripping the subpath prefix
// (e.g. /ui/) before forwarding requests to this port. This handler always
// receives paths rooted at "/".
//
// window.BASE_PATH is injected statically from the configured public_console_url
// so the React app knows its basename (e.g. "/ui" or "" for direct access).
type spaHandler struct {
	staticFS   http.FileSystem
	indexBytes []byte
	basePath   string // e.g. "/ui" extracted from public_console_url, may be empty
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	filePath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if filePath == "" {
		filePath = "index.html"
	}

	file, err := h.staticFS.Open(filePath)
	if err == nil {
		defer file.Close()
		stat, _ := file.Stat()
		if !stat.IsDir() {
			if filePath == "index.html" {
				h.serveIndex(w, r)
				return
			}
			http.ServeContent(w, r, filePath, stat.ModTime(), file.(io.ReadSeeker))
			return
		}
	}

	// Static assets not found → 404
	if strings.HasPrefix(filePath, "assets/") || strings.HasSuffix(filePath, ".js") ||
		strings.HasSuffix(filePath, ".css") || strings.HasSuffix(filePath, ".png") {
		http.NotFound(w, r)
		return
	}

	// SPA route → serve index.html
	h.serveIndex(w, r)
}

// serveIndex serves index.html with window.BASE_PATH injected from config.
// The value comes from the path component of public_console_url (e.g. "/ui"),
// or empty string for direct IP:port access.
func (h *spaHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	// Use the configured basePath when behind a reverse proxy (nginx sets X-Forwarded-For
	// or X-Real-IP). For direct IP:port access neither header is present, so inject an
	// empty BASE_PATH so React Router matches routes at root.
	basePath := h.basePath
	if r.Header.Get("X-Forwarded-For") == "" && r.Header.Get("X-Real-IP") == "" {
		basePath = ""
	}
	script := fmt.Sprintf(`<script>window.BASE_PATH=%q;</script>`, basePath)
	injected := bytes.Replace(h.indexBytes, []byte("</head>"), []byte(script+"</head>"), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(injected)
}

// setupEmbeddedFrontend loads the embedded frontend and returns an http.Handler.
func (s *Server) setupEmbeddedFrontend() (http.Handler, error) {
	frontendFS, err := getFrontendFS()
	if err != nil {
		logrus.WithError(err).Warn("Failed to load embedded frontend")
		return nil, err
	}

	httpFS := http.FS(frontendFS)

	indexFile, err := httpFS.Open("index.html")
	if err != nil {
		return nil, err
	}
	defer indexFile.Close()

	stat, err := indexFile.Stat()
	if err != nil {
		return nil, err
	}

	indexBytes := make([]byte, stat.Size())
	if _, err = io.ReadFull(indexFile, indexBytes); err != nil {
		return nil, err
	}

	basePath := extractBasePathFromURL(s.config.PublicConsoleURL)
	logrus.WithFields(logrus.Fields{
		"base_path": basePath,
	}).Info("Embedded web console enabled")
	return &spaHandler{staticFS: httpFS, indexBytes: indexBytes, basePath: basePath}, nil
}
