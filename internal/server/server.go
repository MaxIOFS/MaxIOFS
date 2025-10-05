package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/api"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metrics"
	"github.com/maxiofs/maxiofs/internal/middleware"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/share"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// TODO: Re-enable embed in production build
// //go:embed all:web/dist
// var webAssets embed.FS

// Server represents the MaxIOFS server
type Server struct {
	config         *config.Config
	httpServer     *http.Server
	consoleServer  *http.Server
	storageBackend storage.Backend
	bucketManager  bucket.Manager
	objectManager  object.Manager
	authManager    auth.Manager
	metricsManager metrics.Manager
	shareManager   share.Manager
	systemMetrics  *metrics.SystemMetricsTracker
}

// New creates a new MaxIOFS server
func New(cfg *config.Config) (*Server, error) {
	// Initialize storage backend
	storageBackend, err := storage.NewBackend(cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage backend: %w", err)
	}

	// Initialize managers
	bucketManager := bucket.NewManager(storageBackend)
	objectManager := object.NewManager(storageBackend, cfg.Storage)
	authManager := auth.NewManager(cfg.Auth, cfg.DataDir)
	metricsManager := metrics.NewManager(cfg.Metrics)

	// Initialize system metrics
	systemMetrics := metrics.NewSystemMetrics(cfg.DataDir)

	// Initialize share manager with same database as auth
	shareManager, err := share.NewManagerWithDB(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create share manager: %w", err)
	}

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
		config:         cfg,
		httpServer:     httpServer,
		consoleServer:  consoleServer,
		storageBackend: storageBackend,
		bucketManager:  bucketManager,
		objectManager:  objectManager,
		authManager:    authManager,
		metricsManager: metricsManager,
		shareManager:   shareManager,
		systemMetrics:  systemMetrics,
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

func (sma *shareManagerAdapter) GetShareByObject(ctx context.Context, bucketName, objectKey string) (interface{}, error) {
	return sma.mgr.GetShareByObject(ctx, bucketName, objectKey)
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
	)

	// Apply middleware
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
	// TODO: Serve embedded web assets when embed is re-enabled
	// webHandler := http.FileServer(http.FS(webAssets))

	// Temporary: serve static placeholder
	webHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>MaxIOFS Console</title></head>
<body>
<h1>MaxIOFS Console</h1>
<p>Web console will be available when frontend is built.</p>
<p>API is available at :8080</p>
</body>
</html>`))
	})

	// API endpoints for the web console
	apiRouter := router.PathPrefix("/api/v1").Subrouter()

	// Setup complete Console API routes
	s.setupConsoleAPIRoutes(apiRouter)

	// Serve static files and SPA
	router.PathPrefix("/").Handler(webHandler)
}
