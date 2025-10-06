package server

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	nextJSPort         = 3000
	nextJSReadyTimeout = 30 * time.Second
)

// NextJSServer manages the embedded Next.js server process
type NextJSServer struct {
	cmd   *exec.Cmd
	proxy *httputil.ReverseProxy
}

// NewNextJSServer creates and starts the Next.js server
func NewNextJSServer() (*NextJSServer, error) {
	// Check if standalone build exists
	standalonePath := filepath.Join("web", "frontend", ".next", "standalone")
	serverPath := filepath.Join(standalonePath, "server.js")

	if _, err := os.Stat(serverPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("Next.js standalone build not found at %s. Run 'npm run build' first", serverPath)
	}

	// Prepare Node.js command
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("node.exe", "server.js")
	} else {
		cmd = exec.Command("node", "server.js")
	}

	// Set working directory to standalone build
	cmd.Dir = standalonePath

	// Set environment
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("PORT=%d", nextJSPort),
		"NODE_ENV=production",
	)

	// Capture output
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Start the server
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start Next.js server: %w", err)
	}

	logrus.WithField("port", nextJSPort).Info("Started Next.js server")

	// Wait for Next.js to be ready
	if err := waitForNextJS(nextJSPort, nextJSReadyTimeout); err != nil {
		cmd.Process.Kill()
		return nil, err
	}

	// Create reverse proxy
	target, _ := url.Parse(fmt.Sprintf("http://localhost:%d", nextJSPort))
	proxy := httputil.NewSingleHostReverseProxy(target)

	return &NextJSServer{
		cmd:   cmd,
		proxy: proxy,
	}, nil
}

// Handler returns the HTTP handler for the Next.js proxy
func (s *NextJSServer) Handler() http.Handler {
	return s.proxy
}

// Stop stops the Next.js server
func (s *NextJSServer) Stop() error {
	if s.cmd != nil && s.cmd.Process != nil {
		logrus.Info("Stopping Next.js server")
		if err := s.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to stop Next.js server: %w", err)
		}
		s.cmd.Wait()
	}
	return nil
}

// waitForNextJS waits for Next.js server to be ready
func waitForNextJS(port int, timeout time.Duration) error {
	url := fmt.Sprintf("http://localhost:%d", port)
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				logrus.Info("Next.js server is ready")
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("Next.js server did not become ready within %v", timeout)
}

// NextJSAvailable checks if Next.js standalone build is available
func NextJSAvailable() bool {
	serverPath := filepath.Join("web", "frontend", ".next", "standalone", "server.js")
	_, err := os.Stat(serverPath)
	return err == nil
}
