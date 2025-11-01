package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/api"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/lifecycle"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/metrics"
	"github.com/maxiofs/maxiofs/internal/middleware"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/share"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// Server represents the MaxIOFS server
type Server struct {
	config          *config.Config
	httpServer      *http.Server
	consoleServer   *http.Server
	storageBackend  storage.Backend
	metadataStore   metadata.Store
	bucketManager   bucket.Manager
	objectManager   object.Manager
	authManager     auth.Manager
	metricsManager  metrics.Manager
	shareManager    share.Manager
	systemMetrics   *metrics.SystemMetricsTracker
	lifecycleWorker *lifecycle.Worker
	startTime       time.Time // Server start time for uptime calculation
}

// New creates a new MaxIOFS server
func New(cfg *config.Config) (*Server, error) {
	// Initialize storage backend
	storageBackend, err := storage.NewBackend(cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage backend: %w", err)
	}

	// Initialize metadata store (BadgerDB)
	metadataStore, err := metadata.NewBadgerStore(metadata.BadgerOptions{
		DataDir:           cfg.DataDir,
		SyncWrites:        false, // Async for performance
		CompactionEnabled: true,  // Auto GC
		Logger:            logrus.StandardLogger(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create metadata store: %w", err)
	}

	// Initialize managers
	bucketManager := bucket.NewManager(storageBackend, metadataStore)
	objectManager := object.NewManager(storageBackend, metadataStore, cfg.Storage)

	// Connect object manager to bucket manager for metrics updates
	if om, ok := objectManager.(interface {
		SetBucketManager(interface {
			IncrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
			DecrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
		})
	}); ok {
		om.SetBucketManager(bucketManager)
	}

	authManager := auth.NewManager(cfg.Auth, cfg.DataDir)
	metricsManager := metrics.NewManagerWithStore(cfg.Metrics, cfg.DataDir, metadataStore)

	// Initialize system metrics
	systemMetrics := metrics.NewSystemMetrics(cfg.DataDir)

	// Connect system metrics to metrics manager
	if mm, ok := metricsManager.(interface{ SetSystemMetrics(*metrics.SystemMetricsTracker) }); ok {
		mm.SetSystemMetrics(systemMetrics)
	}

	// Connect storage metrics provider to metrics manager
	if mm, ok := metricsManager.(interface{ SetStorageMetricsProvider(metrics.StorageMetricsProvider) }); ok {
		mm.SetStorageMetricsProvider(func() (totalBuckets, totalObjects, totalSize int64) {
			// Get storage metrics by listing all buckets
			buckets, err := bucketManager.ListBuckets(context.Background(), "")
			if err != nil {
				return 0, 0, 0
			}

			totalBuckets = int64(len(buckets))
			for _, b := range buckets {
				totalObjects += b.ObjectCount
				totalSize += b.TotalSize
			}
			return
		})
	}

	// Initialize share manager with same database as auth
	shareManager, err := share.NewManagerWithDB(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create share manager: %w", err)
	}

	// Initialize lifecycle worker
	lifecycleWorker := lifecycle.NewWorker(bucketManager, objectManager, metadataStore)

	// Create HTTP servers
	httpServer := &http.Server{
		Addr:         cfg.Listen,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	consoleServer := &http.Server{
		Addr:         cfg.ConsoleListen,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	server := &Server{
		config:          cfg,
		httpServer:      httpServer,
		consoleServer:   consoleServer,
		storageBackend:  storageBackend,
		metadataStore:   metadataStore,
		bucketManager:   bucketManager,
		objectManager:   objectManager,
		authManager:     authManager,
		metricsManager:  metricsManager,
		shareManager:    shareManager,
		systemMetrics:   systemMetrics,
		lifecycleWorker: lifecycleWorker,
		startTime:       time.Now(), // Record server start time
	}

	// Setup routes
	if err := server.setupRoutes(); err != nil {
		return nil, fmt.Errorf("failed to setup routes: %w", err)
	}

	return server, nil
}

// Start starts the MaxIOFS server
func (s *Server) Start(ctx context.Context) error {
	logrus.WithFields(logrus.Fields{
		"api_address":     s.config.Listen,
		"console_address": s.config.ConsoleListen,
		"data_dir":        s.config.DataDir,
	}).Info("Starting MaxIOFS servers")

	// Start metrics collection
	if s.config.Metrics.Enable {
		s.metricsManager.Start(ctx)
	}

	// Start lifecycle worker (runs every 1 hour)
	s.lifecycleWorker.Start(ctx, 1*time.Hour)

	// Start API server
	go func() {
		if err := s.startAPIServer(); err != nil && err != http.ErrServerClosed {
			logrus.WithError(err).Error("API server error")
		}
	}()

	// Start console server
	go func() {
		if err := s.startConsoleServer(); err != nil && err != http.ErrServerClosed {
			logrus.WithError(err).Error("Console server error")
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	return s.shutdown()
}

func (s *Server) startAPIServer() error {
	logrus.WithField("address", s.config.Listen).Info("Starting API server")

	if s.config.EnableTLS {
		return s.httpServer.ListenAndServeTLS(s.config.CertFile, s.config.KeyFile)
	}
	return s.httpServer.ListenAndServe()
}

func (s *Server) startConsoleServer() error {
	logrus.WithField("address", s.config.ConsoleListen).Info("Starting console server")

	if s.config.EnableTLS {
		logrus.Info("Console server using TLS")
		return s.consoleServer.ListenAndServeTLS(s.config.CertFile, s.config.KeyFile)
	}
	return s.consoleServer.ListenAndServe()
}

func (s *Server) shutdown() error {
	logrus.Info("Shutting down servers")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown API server
	if err := s.httpServer.Shutdown(ctx); err != nil {
		logrus.WithError(err).Error("Failed to shutdown API server")
	}

	// Shutdown console server
	if err := s.consoleServer.Shutdown(ctx); err != nil {
		logrus.WithError(err).Error("Failed to shutdown console server")
	}

	// Stop metrics
	if s.metricsManager != nil {
		s.metricsManager.Stop()
	}

	// Stop lifecycle worker
	if s.lifecycleWorker != nil {
		s.lifecycleWorker.Stop()
	}

	// Close storage backend
	if err := s.storageBackend.Close(); err != nil {
		logrus.WithError(err).Error("Failed to close storage backend")
	}

	return nil
}

// shareManagerAdapter adapts share.Manager to the interface expected by api.NewHandler
type shareManagerAdapter struct {
	mgr share.Manager
}

func (sma *shareManagerAdapter) GetShareByObject(ctx context.Context, bucketName, objectKey, tenantID string) (interface{}, error) {
	return sma.mgr.GetShareByObject(ctx, bucketName, objectKey, tenantID)
}

func (s *Server) setupRoutes() error {
	// Setup API routes (S3 compatible)
	apiRouter := mux.NewRouter()

	// Create a wrapper for shareManager to match the interface expected by api.NewHandler
	shareManagerWrapper := &shareManagerAdapter{mgr: s.shareManager}

	apiHandler := api.NewHandler(
		s.bucketManager,
		s.objectManager,
		s.authManager,
		s.metricsManager,
		shareManagerWrapper,
		s.config.PublicAPIURL,
		s.config.PublicConsoleURL,
		s.config.DataDir,
	)

	// Apply middleware
	// VERBOSE LOGGING - logs EVERY request with full details
	apiRouter.Use(middleware.VerboseLogging())
	apiRouter.Use(middleware.CORS())
	apiRouter.Use(middleware.Logging())
	if s.config.Auth.EnableAuth {
		apiRouter.Use(s.authManager.Middleware())
	}
	if s.config.Metrics.Enable {
		apiRouter.Use(s.metricsManager.Middleware())
	}

	// Register API routes
	apiHandler.RegisterRoutes(apiRouter)

	// Setup CORS and other middleware
	s.httpServer.Handler = handlers.RecoveryHandler()(apiRouter)

	// Setup console routes (Web UI)
	consoleRouter := mux.NewRouter()
	s.setupConsoleRoutes(consoleRouter)
	s.consoleServer.Handler = handlers.RecoveryHandler()(consoleRouter)

	return nil
}

func (s *Server) setupConsoleRoutes(router *mux.Router) {
	// Extract base path from public_console_url
	basePath := extractBasePathFromURL(s.config.PublicConsoleURL)

	logrus.WithFields(logrus.Fields{
		"public_console_url": s.config.PublicConsoleURL,
		"base_path":          basePath,
	}).Info("Setting up console routes")

	// Create base router
	var baseRouter *mux.Router
	if basePath != "/" && basePath != "" {
		// All routes (including API) must be under the base path
		baseRouter = router.PathPrefix(basePath).Subrouter()
	} else {
		baseRouter = router
	}

	// API endpoints for the web console (under base path)
	apiRouter := baseRouter.PathPrefix("/api/v1").Subrouter()
	s.setupConsoleAPIRoutes(apiRouter)

	// Try to serve embedded frontend, fallback to placeholder if not available
	frontendHandler, err := s.setupEmbeddedFrontend(router)
	if err != nil {
		logrus.WithError(err).Warn("Failed to setup embedded frontend, using placeholder")
		s.setupPlaceholderHandler(baseRouter)
		return
	}

	// Serve embedded frontend for all other routes (under base path)
	baseRouter.PathPrefix("/").Handler(frontendHandler)
}

func (s *Server) setupPlaceholderHandler(router *mux.Router) {
	webHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
	<title>MaxIOFS Console</title>
	<style>
		body { font-family: system-ui, -apple-system, sans-serif; max-width: 800px; margin: 50px auto; padding: 20px; }
		.warning { background: #fff3cd; border: 1px solid #ffc107; border-radius: 4px; padding: 15px; margin: 20px 0; }
		code { background: #f5f5f5; padding: 2px 6px; border-radius: 3px; font-family: monospace; }
	</style>
</head>
<body>
	<h1>⚠️ MaxIOFS Console - Not Built</h1>
	<div class="warning">
		<p><strong>The web console frontend has not been compiled.</strong></p>
		<p>To build and enable the web console:</p>
		<ol>
			<li>Build: <code>cd web/frontend && npm install && npm run build</code></li>
			<li>Run: <code>go run ./cmd/maxiofs --data-dir ./data</code></li>
		</ol>
	</div>
	<p><strong>API Endpoints:</strong></p>
	<ul>
		<li>S3 API: <a href="` + s.config.PublicAPIURL + `">` + s.config.PublicAPIURL + `</a></li>
		<li>Console API: <a href="` + s.config.PublicConsoleURL + `/api/v1">` + s.config.PublicConsoleURL + `/api/v1</a></li>
	</ul>
	<p><strong>For development with hot reload:</strong></p>
	<p><code>cd web/frontend && npm run dev</code> (opens port 3000)</p>
</body>
</html>`))
	})

	router.PathPrefix("/").Handler(webHandler)
}

// extractBasePathFromURL extracts the path component from a URL
// Example: "https://s3.accst.local/ui" -> "/ui"
// Example: "http://localhost:8081" -> "/"
func extractBasePathFromURL(urlStr string) string {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		logrus.WithError(err).Warn("Failed to parse public console URL, using / as base path")
		return "/"
	}

	basePath := parsedURL.Path
	if basePath == "" || basePath == "/" {
		return "/"
	}

	// Ensure base path starts with / but does NOT end with /
	// This is important for PathPrefix matching in mux
	basePath = strings.TrimSuffix(basePath, "/")
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}

	return basePath
}
