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

// spaHandler serves the embedded SPA. Static assets are served directly;
// everything else falls back to index.html for client-side routing.
// The basePath prefix has already been stripped by the console server wrapper
// before this handler is invoked, so all paths arrive rooted at "/".
type spaHandler struct {
	staticFS   http.FileSystem
	indexBytes []byte
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
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write(h.indexBytes)
				return
			}
			http.ServeContent(w, r, filePath, stat.ModTime(), file.(io.ReadSeeker))
			return
		}
	}

	// Static asset not found → 404
	if strings.HasPrefix(filePath, "assets/") || strings.HasSuffix(filePath, ".js") ||
		strings.HasSuffix(filePath, ".css") || strings.HasSuffix(filePath, ".png") {
		http.NotFound(w, r)
		return
	}

	// SPA route → serve index.html
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(h.indexBytes)
}

// extractBasePath extracts the path component from a URL with trailing slash.
// "https://maxiofs.local/ui" → "/ui/"
// "http://localhost:8081"   → "/"
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

// setupEmbeddedFrontend loads the embedded frontend and returns an http.Handler.
// It injects <base href="/ui/"> and window.BASE_PATH into index.html so that
// React Router and the API client resolve paths correctly when the console is
// served under a subpath (e.g. /ui/).
func (s *Server) setupEmbeddedFrontend() (http.Handler, error) {
	frontendFS, err := getFrontendFS()
	if err != nil {
		logrus.WithError(err).Warn("Failed to load embedded frontend")
		return nil, err
	}

	basePath := extractBasePath(s.config.PublicConsoleURL)
	logrus.WithField("base_path", basePath).Info("Setting up embedded frontend")

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

	// Inject <base> tag and window.BASE_PATH right after <head>.
	// With Vite base='./', all asset paths in the bundle are relative and will
	// resolve correctly once the browser knows the subpath via <base href="/ui/">.
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

	logrus.Info("Embedded web console enabled")
	return &spaHandler{staticFS: httpFS, indexBytes: indexBytes}, nil
}
